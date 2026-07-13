import test from 'node:test';
import assert from 'node:assert/strict';
import { PassThrough } from 'node:stream';
import {
  JsonRpcConnection,
  JsonRpcConnectionClosedError,
  JsonRpcProtocolError,
  JsonRpcRequestCancelledError,
  JsonRpcResponseError,
  type JsonRpcNotification,
  type JsonRpcRequest,
} from './json-rpc.ts';

interface ConnectionHarness {
  connection: JsonRpcConnection;
  input: PassThrough;
  output: PassThrough;
  frames: Array<Record<string, unknown>>;
  rawOutput: string[];
  errors: Error[];
}

interface HarnessOptions {
  closeHandler?: (reason: Error) => void;
  maxFrameBytes?: number;
}

function createHarness(options: HarnessOptions = {}): ConnectionHarness {
  const input = new PassThrough();
  const output = new PassThrough();
  const frames: Array<Record<string, unknown>> = [];
  const rawOutput: string[] = [];
  const errors: Error[] = [];
  let buffered = '';
  output.setEncoding('utf8');
  output.on('data', (chunk: string) => {
    rawOutput.push(chunk);
    buffered += chunk;
    let newline = buffered.indexOf('\n');
    while (newline >= 0) {
      const line = buffered.slice(0, newline);
      buffered = buffered.slice(newline + 1);
      if (line !== '') {
        const parsed: unknown = JSON.parse(line);
        if (isRecord(parsed)) frames.push(parsed);
      }
      newline = buffered.indexOf('\n');
    }
  });
  const connection = new JsonRpcConnection({
    closeHandler: options.closeHandler,
    input,
    maxFrameBytes: options.maxFrameBytes,
    output,
    errorHandler: (error) => errors.push(error),
  });
  connection.start();
  return { connection, input, output, frames, rawOutput, errors };
}

test('parses split and coalesced LF and CRLF request and notification frames', async (t) => {
  const harness = createHarness();
  t.after(() => harness.connection.close());
  const requests: JsonRpcRequest[] = [];
  const notifications: JsonRpcNotification[] = [];
  harness.connection.setRequestHandler((request) => {
    requests.push(request);
  });
  harness.connection.setNotificationHandler((notification) => {
    notifications.push(notification);
  });

  harness.input.write('{"jsonrpc":"2.0","id":4,"method":"tools/li');
  harness.input.write('st","params":{"cursor":"á"}}\r\n');
  harness.input.write(
    '{"jsonrpc":"2.0","method":"notifications/initialized"}\n'
      + '{"jsonrpc":"2.0","id":"next","method":"ping"}\r\n',
  );
  await flushEvents();

  assert.deepEqual(requests, [
    { jsonrpc: '2.0', id: 4, method: 'tools/list', params: { cursor: 'á' } },
    { jsonrpc: '2.0', id: 'next', method: 'ping' },
  ]);
  assert.deepEqual(notifications, [
    { jsonrpc: '2.0', method: 'notifications/initialized' },
  ]);
  assert.deepEqual(harness.frames, []);
  assert.deepEqual(harness.errors, []);
});

test('correlates outgoing requests with result responses', async (t) => {
  const harness = createHarness();
  t.after(() => harness.connection.close());

  const resultPromise = harness.connection.sendRequest('elicitation/create', {
    message: 'Pick a player',
  });
  await waitForFrames(harness.frames, 1);
  assert.deepEqual(harness.frames[0], {
    jsonrpc: '2.0',
    id: 1,
    method: 'elicitation/create',
    params: { message: 'Pick a player' },
  });

  harness.input.write('{"jsonrpc":"2.0","id":1,"result":{"action":"accept"}}\n');
  assert.deepEqual(await resultPromise, { action: 'accept' });
  assert.deepEqual(harness.errors, []);
});

test('cancels an outgoing request and notifies the remote peer', async (t) => {
  const harness = createHarness();
  t.after(() => harness.connection.close());
  const controller = new AbortController();

  const result = harness.connection.sendRequest('elicitation/create', { message: 'Pick' }, controller.signal);
  await waitForFrames(harness.frames, 1);
  controller.abort();

  await assert.rejects(result, JsonRpcRequestCancelledError);
  await waitForFrames(harness.frames, 2);
  assert.deepEqual(harness.frames[1], {
    jsonrpc: '2.0',
    method: 'notifications/cancelled',
    params: { reason: 'caller cancelled request', requestId: 1 },
  });
});

test('rejects an outgoing request with the remote JSON-RPC error', async (t) => {
  const harness = createHarness();
  t.after(() => harness.connection.close());

  const response = harness.connection.sendRequest('sampling/createMessage');
  await waitForFrames(harness.frames, 1);
  harness.input.write(
    '{"jsonrpc":"2.0","id":1,"error":{"code":-32001,"message":"denied","data":{"reason":"policy"}}}\n',
  );

  await assert.rejects(response, (error: unknown) => {
    assert.ok(error instanceof JsonRpcResponseError);
    assert.equal(error.code, -32_001);
    assert.deepEqual(error.data, { reason: 'policy' });
    return true;
  });
});

test('writes only compact newline-delimited JSON for results, errors, and notifications', async (t) => {
  const harness = createHarness();
  t.after(() => harness.connection.close());

  await harness.connection.sendResult('abc', undefined);
  await harness.connection.sendError(7, -32_602, 'Invalid params', { field: 'jobId' });
  await harness.connection.sendNotification('notifications/tools/list_changed', { revision: 2 });
  await waitForFrames(harness.frames, 3);

  assert.deepEqual(harness.frames, [
    { jsonrpc: '2.0', id: 'abc', result: null },
    {
      jsonrpc: '2.0',
      id: 7,
      error: { code: -32_602, message: 'Invalid params', data: { field: 'jobId' } },
    },
    {
      jsonrpc: '2.0',
      method: 'notifications/tools/list_changed',
      params: { revision: 2 },
    },
  ]);
  const raw = harness.rawOutput.join('');
  assert.equal(raw.endsWith('\n'), true);
  assert.equal(raw.split('\n').filter(Boolean).length, 3);
  assert.equal(raw.includes('[json-rpc]'), false);
});

test('emits JSON-RPC parse and invalid-request errors and reports both locally', async (t) => {
  const harness = createHarness();
  t.after(() => harness.connection.close());

  harness.input.write('{not-json}\n');
  harness.input.write('{"jsonrpc":"2.0","id":9,"method":12}\n');
  await waitForFrames(harness.frames, 2);

  assert.deepEqual(harness.frames, [
    { jsonrpc: '2.0', id: null, error: { code: -32_700, message: 'Parse error' } },
    { jsonrpc: '2.0', id: 9, error: { code: -32_600, message: 'Invalid Request' } },
  ]);
  assert.equal(harness.errors.length, 2);
  assert.ok(harness.errors[0] instanceof JsonRpcProtocolError);
  assert.equal(harness.errors[0].kind, 'parse');
  assert.ok(harness.errors[1] instanceof JsonRpcProtocolError);
  assert.equal(harness.errors[1].kind, 'invalid_request');
});

test('rejects and closes an oversized input frame without retaining it', async () => {
  const harness = createHarness({ maxFrameBytes: 64 });

  harness.input.write(`${JSON.stringify({
    jsonrpc: '2.0',
    id: 1,
    method: 'tools/call',
    params: { padding: 'x'.repeat(128) },
  })}\n`);
  await waitForFrames(harness.frames, 1);

  assert.deepEqual(harness.frames, [{
    jsonrpc: '2.0',
    id: null,
    error: {
      code: -32_600,
      data: { reason: 'message exceeds the configured size limit' },
      message: 'Invalid Request',
    },
  }]);
  assert.equal(harness.connection.closed, true);
  assert.equal(harness.errors.length, 1);
  assert.ok(harness.errors[0] instanceof JsonRpcProtocolError);
  assert.equal(harness.errors[0].kind, 'frame_too_large');
});

test('responds method-not-found when no request handler is installed but ignores notifications', async (t) => {
  const harness = createHarness();
  t.after(() => harness.connection.close());

  harness.input.write('{"jsonrpc":"2.0","id":"missing","method":"unknown"}\n');
  harness.input.write('{"jsonrpc":"2.0","method":"unknown-notification"}\n');
  await waitForFrames(harness.frames, 1);
  await flushEvents();

  assert.deepEqual(harness.frames, [
    {
      jsonrpc: '2.0',
      id: 'missing',
      error: { code: -32_601, message: 'Method not found' },
    },
  ]);
});

test('reports malformed and unknown responses without replying to them', async (t) => {
  const harness = createHarness();
  t.after(() => harness.connection.close());

  const pending = harness.connection.sendRequest('roots/list');
  await waitForFrames(harness.frames, 1);
  const outputCount = harness.frames.length;
  harness.input.write(
    '{"jsonrpc":"2.0","id":1,"error":{"code":"bad","message":"broken"}}\n',
  );
  await assert.rejects(pending, /invalid json-rpc response/);
  harness.input.write('{"jsonrpc":"2.0","id":999,"result":true}\n');
  await flushEvents();

  assert.equal(harness.frames.length, outputCount);
  assert.equal(harness.errors.length, 2);
  assert.ok(harness.errors.every((error) => (
    error instanceof JsonRpcProtocolError && error.kind === 'invalid_response'
  )));
});

test('rejects all pending requests when input closes and refuses later writes', async () => {
  const closeReasons: Error[] = [];
  const harness = createHarness({ closeHandler: (reason) => closeReasons.push(reason) });
  const first = harness.connection.sendRequest('one');
  const second = harness.connection.sendRequest('two');
  await waitForFrames(harness.frames, 2);

  harness.input.end();
  await assert.rejects(first, JsonRpcConnectionClosedError);
  await assert.rejects(second, /input ended/);
  assert.equal(harness.connection.closed, true);
  assert.equal(closeReasons.length, 1);
  assert.match(closeReasons[0]?.message ?? '', /input ended/);
  await assert.rejects(harness.connection.sendNotification('late'), JsonRpcConnectionClosedError);
  await assert.rejects(harness.connection.sendRequest('late'), JsonRpcConnectionClosedError);
  harness.connection.close();
  assert.equal(closeReasons.length, 1, 'close handler must run exactly once');
});

test('processes a final non-newline-terminated frame before input end', async () => {
  const harness = createHarness();
  const notifications: JsonRpcNotification[] = [];
  harness.connection.setNotificationHandler((notification) => {
    notifications.push(notification);
  });

  harness.input.end('{"jsonrpc":"2.0","method":"exit"}\r');
  await flushEvents();

  assert.deepEqual(notifications, [{ jsonrpc: '2.0', method: 'exit' }]);
  assert.equal(harness.connection.closed, true);
});

test('reports synchronous and asynchronous handler failures without writing diagnostics to output', async (t) => {
  const harness = createHarness();
  t.after(() => harness.connection.close());
  harness.connection.setRequestHandler(() => {
    throw new Error('sync handler failure');
  });
  harness.connection.setNotificationHandler(async () => {
    throw new Error('async handler failure');
  });

  harness.input.write('{"jsonrpc":"2.0","id":1,"method":"request"}\n');
  harness.input.write('{"jsonrpc":"2.0","method":"notification"}\n');
  await flushEvents();

  assert.deepEqual(harness.errors.map((error) => error.message), [
    'sync handler failure',
    'async handler failure',
  ]);
  assert.deepEqual(harness.frames, []);
});

async function waitForFrames(frames: Array<Record<string, unknown>>, count: number): Promise<void> {
  for (let attempt = 0; attempt < 50; attempt += 1) {
    if (frames.length >= count) return;
    await new Promise<void>((resolve) => setImmediate(resolve));
  }
  throw new Error(`timed out waiting for ${count} frames; got ${frames.length}`);
}

async function flushEvents(): Promise<void> {
  await new Promise<void>((resolve) => setImmediate(resolve));
  await Promise.resolve();
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}
