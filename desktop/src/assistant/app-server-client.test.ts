import assert from 'node:assert/strict';
import test from 'node:test';
import {
  AppServerClosedError,
  AppServerRequestTimeoutError,
  CodexAppServerClient,
} from './app-server-client.ts';
import type { AppServerTransport } from './app-server-transport.ts';

class FakeTransport implements AppServerTransport {
  readonly frames: string[] = [];
  readonly #closeListeners = new Set<(reason: Error) => void>();
  readonly #dataListeners = new Set<(chunk: Buffer | string) => void>();
  readonly #diagnosticListeners = new Set<(chunk: Buffer | string) => void>();
  readonly #errorListeners = new Set<(error: Error) => void>();
  closed = false;

  onData(listener: (chunk: Buffer | string) => void): void {
    this.#dataListeners.add(listener);
  }

  onDiagnostic(listener: (chunk: Buffer | string) => void): void {
    this.#diagnosticListeners.add(listener);
  }

  onError(listener: (error: Error) => void): void {
    this.#errorListeners.add(listener);
  }

  onClose(listener: (reason: Error) => void): void {
    this.#closeListeners.add(listener);
  }

  async write(frame: string): Promise<void> {
    if (this.closed) throw new Error('transport closed');
    this.frames.push(frame);
  }

  close(): void {
    if (this.closed) return;
    this.closed = true;
    for (const listener of this.#closeListeners) listener(new Error('transport closed'));
  }

  emit(value: unknown): void {
    const frame = `${JSON.stringify(value)}\n`;
    for (const listener of this.#dataListeners) listener(Buffer.from(frame));
  }

  emitRaw(value: string): void {
    for (const listener of this.#dataListeners) listener(value);
  }

  emitClose(reason = new Error('peer exited')): void {
    this.closed = true;
    for (const listener of this.#closeListeners) listener(reason);
  }
}

function written(transport: FakeTransport, index: number): Record<string, unknown> {
  const frame = transport.frames[index];
  assert.notEqual(frame, undefined, `missing transport frame ${index}`);
  return JSON.parse(frame) as Record<string, unknown>;
}

test('initializes with experimental API and sends the initialized notification without a jsonrpc envelope', async () => {
  const transport = new FakeTransport();
  const statuses: string[] = [];
  const client = new CodexAppServerClient({
    clientInfo: { name: 'fragforge_test', title: 'FragForge test', version: '1.2.3' },
    onStatus: (status) => statuses.push(status),
    transport,
  });

  const initializing = client.initialize();
  await waitForFrames(transport, 1);
  assert.deepEqual(written(transport, 0), {
    id: 1,
    method: 'initialize',
    params: {
      capabilities: { experimentalApi: true, requestAttestation: false },
      clientInfo: { name: 'fragforge_test', title: 'FragForge test', version: '1.2.3' },
    },
  });
  transport.emit({ id: 1, result: { platformOs: 'windows' } });
  await initializing;
  assert.deepEqual(written(transport, 1), { method: 'initialized', params: {} });
  assert.deepEqual(statuses, ['starting', 'ready']);
  assert.equal(client.status, 'ready');
});

test('correlates thread and turn requests and applies safe app-server defaults', async () => {
  const transport = new FakeTransport();
  const client = new CodexAppServerClient({
    dynamicTools: [{
      description: 'FragForge tools',
      name: 'fragforge',
      tools: [{
        description: 'Search current operations',
        inputSchema: { type: 'object' },
        name: 'search',
        type: 'function',
      }],
      type: 'namespace',
    }],
    transport,
  });
  const ready = client.initialize();
  await waitForFrames(transport, 1);
  transport.emit({ id: 1, result: {} });
  await ready;

  const startThread = client.startThread({ cwd: 'C:\\FragForge', model: 'gpt-5.4' });
  await waitForFrames(transport, 3);
  assert.deepEqual(written(transport, 2), {
    id: 2,
    method: 'thread/start',
    params: {
      approvalPolicy: 'never',
      cwd: 'C:\\FragForge',
      dynamicTools: [{
        description: 'FragForge tools',
        name: 'fragforge',
        tools: [{
          description: 'Search current operations',
          inputSchema: { type: 'object' },
          name: 'search',
          type: 'function',
        }],
        type: 'namespace',
      }],
      model: 'gpt-5.4',
      sandbox: 'read-only',
      serviceName: 'fragforge_studio',
    },
  });
  transport.emit({ id: 2, result: { thread: { id: 'thr_1', sessionId: 'session_1' } } });
  assert.deepEqual(await startThread, { id: 'thr_1', sessionId: 'session_1' });

  const startTurn = client.startTurn('thr_1', 'Resume the selected demo.');
  await waitForFrames(transport, 4);
  assert.deepEqual(written(transport, 3), {
    id: 3,
    method: 'turn/start',
    params: {
      input: [{ text: 'Resume the selected demo.', text_elements: [], type: 'text' }],
      threadId: 'thr_1',
    },
  });
  transport.emit({ id: 3, result: { turn: { id: 'turn_1', status: 'inProgress' } } });
  assert.deepEqual(await startTurn, { id: 'turn_1', status: 'inProgress' });

  const interrupted = client.interruptTurn('thr_1', 'turn_1');
  await waitForFrames(transport, 5);
  assert.deepEqual(written(transport, 4), {
    id: 4,
    method: 'turn/interrupt',
    params: { threadId: 'thr_1', turnId: 'turn_1' },
  });
  transport.emit({ id: 4, result: {} });
  await interrupted;
});

test('resumes a persisted thread without resending unsupported dynamic tools or full history', async () => {
  const transport = new FakeTransport();
  const client = new CodexAppServerClient({
    dynamicTools: [{
      description: 'FragForge tools',
      inputSchema: { type: 'object' },
      name: 'fragforge',
      type: 'function',
    }],
    transport,
  });
  const ready = client.initialize();
  await waitForFrames(transport, 1);
  transport.emit({ id: 1, result: {} });
  await ready;

  const resume = client.resumeThread('thr-1', { cwd: 'C:\\FragForge', excludeTurns: true });
  await waitForFrames(transport, 3);
  assert.deepEqual(written(transport, 2), {
    id: 2,
    method: 'thread/resume',
    params: {
      approvalPolicy: 'never',
      cwd: 'C:\\FragForge',
      excludeTurns: true,
      sandbox: 'read-only',
      threadId: 'thr-1',
    },
  });
  transport.emit({ id: 2, result: { thread: { id: 'thr-1', sessionId: 'session-1' } } });
  await resume;
});

test('forwards agent deltas and completed turn notifications while preserving generic notifications', async () => {
  const transport = new FakeTransport();
  const deltas: string[] = [];
  const completed: string[] = [];
  const notifications: string[] = [];
  const _client = new CodexAppServerClient({
    onAgentMessageDelta: ({ delta }) => deltas.push(delta),
    onNotification: ({ method }) => notifications.push(method),
    onTurnCompleted: ({ turn }) => completed.push(turn.id),
    transport,
  });

  transport.emit({
    method: 'item/agentMessage/delta',
    params: { delta: 'Hola', itemId: 'item_1', threadId: 'thr_1', turnId: 'turn_1' },
  });
  transport.emit({
    method: 'turn/completed',
    params: { threadId: 'thr_1', turn: { id: 'turn_1', status: 'completed' } },
  });

  assert.deepEqual(deltas, ['Hola']);
  assert.deepEqual(completed, ['turn_1']);
  assert.deepEqual(notifications, ['item/agentMessage/delta', 'turn/completed']);
});

test('answers dynamic tool server requests through the host callback and fails closed when unavailable', async () => {
  const transport = new FakeTransport();
  const seen: unknown[] = [];
  const _client = new CodexAppServerClient({
    onDynamicToolCall: async (call) => {
      seen.push(call);
      return { contentItems: [{ text: 'Current demo is ready.', type: 'inputText' }], success: true };
    },
    transport,
  });

  transport.emit({
    id: 'request_1',
    method: 'item/tool/call',
    params: {
      arguments: { jobId: 'job_1' },
      callId: 'call_1',
      namespace: 'fragforge',
      threadId: 'thr_1',
      tool: 'search',
      turnId: 'turn_1',
    },
  });
  await waitForFrames(transport, 1);
  assert.deepEqual(seen, [{
    arguments: { jobId: 'job_1' },
    callId: 'call_1',
    namespace: 'fragforge',
    requestId: 'request_1',
    threadId: 'thr_1',
    tool: 'search',
    turnId: 'turn_1',
  }]);
  assert.deepEqual(written(transport, 0), {
    id: 'request_1',
    result: { contentItems: [{ text: 'Current demo is ready.', type: 'inputText' }], success: true },
  });

  const noHandlerTransport = new FakeTransport();
  new CodexAppServerClient({ transport: noHandlerTransport });
  noHandlerTransport.emit({
    id: 9,
    method: 'item/tool/call',
    params: {
      arguments: {},
      callId: 'call_2',
      namespace: null,
      threadId: 'thr_2',
      tool: 'preview',
      turnId: 'turn_2',
    },
  });
  await waitForFrames(noHandlerTransport, 1);
  assert.deepEqual(written(noHandlerTransport, 0), {
    id: 9,
    result: {
      contentItems: [{ text: 'FragForge Studio has no handler for dynamic tool calls.', type: 'inputText' }],
      success: false,
    },
  });
});

test('rejects outstanding requests after the transport closes', async () => {
  const transport = new FakeTransport();
  const client = new CodexAppServerClient({ transport });
  const initializing = client.initialize();
  await waitForFrames(transport, 1);
  transport.emitClose();
  await assert.rejects(initializing, AppServerClosedError);
  assert.equal(client.status, 'closed');
  await assert.rejects(client.startThread(), AppServerClosedError);
});

test('fails the transport when an app-server request exceeds its deadline', async () => {
  const transport = new FakeTransport();
  const client = new CodexAppServerClient({ requestTimeoutMs: 10, transport });

  const initializing = client.initialize();
  await waitForFrames(transport, 1);
  await assert.rejects(initializing, AppServerRequestTimeoutError);
  assert.equal(client.status, 'failed');
  assert.equal(transport.closed, true);
});

test('answers a dynamic tool request with failure when its handler exceeds its deadline', async () => {
  const transport = new FakeTransport();
  let handlerSignal: AbortSignal | undefined;
  const client = new CodexAppServerClient({
    dynamicToolTimeoutMs: 10,
    onDynamicToolCall: async (_call, signal) => {
      handlerSignal = signal;
      return new Promise(() => {});
    },
    transport,
  });

  transport.emit({
    id: 'request-timeout',
    method: 'item/tool/call',
    params: {
      arguments: {},
      callId: 'call-timeout',
      namespace: 'fragforge',
      threadId: 'thr-1',
      tool: 'read',
      turnId: 'turn-1',
    },
  });
  await new Promise<void>((resolve) => setTimeout(resolve, 25));

  assert.deepEqual(written(transport, 0), {
    id: 'request-timeout',
    result: {
      contentItems: [{ text: 'Dynamic tool call timed out.', type: 'inputText' }],
      success: false,
    },
  });
  assert.equal(client.status, 'starting');
  assert.equal(handlerSignal?.aborted, true);
  client.close();
});

test('forwards the turn started notification before turn start resolves', () => {
  const transport = new FakeTransport();
  const started: string[] = [];
  new CodexAppServerClient({
    onTurnStarted: ({ turn }) => started.push(turn.id),
    transport,
  });

  transport.emit({
    method: 'turn/started',
    params: { threadId: 'thr-1', turn: { id: 'turn-1', items: [], status: 'inProgress' } },
  });

  assert.deepEqual(started, ['turn-1']);
});

test('handles fragmented CRLF frames without accepting an invalid response id', async () => {
  const transport = new FakeTransport();
  const client = new CodexAppServerClient({ transport });
  const initializing = client.initialize();
  await waitForFrames(transport, 1);
  transport.emitRaw('{"id":1,"result":{}');
  transport.emitRaw('}\r\n');
  await initializing;
  assert.equal(client.status, 'ready');
});

test('processes the final non-newline app-server notification before transport close', () => {
  const transport = new FakeTransport();
  const completed: string[] = [];
  new CodexAppServerClient({
    onTurnCompleted: ({ turn }) => completed.push(turn.id),
    transport,
  });
  transport.emitRaw('{"method":"turn/completed","params":{"threadId":"thr_1","turn":{"id":"turn_1","status":"completed"}}}');
  transport.emitClose();
  assert.deepEqual(completed, ['turn_1']);
});

test('fails closed and rejects the active request when the peer sends an oversized frame', async () => {
  const transport = new FakeTransport();
  const client = new CodexAppServerClient({ maxFrameBytes: 16, transport });
  const initializing = client.initialize();
  await waitForFrames(transport, 1);
  transport.emitRaw('{"id":1,"result":{"tooLong":true}}\n');
  await assert.rejects(initializing, /frame exceeds/);
  assert.equal(client.status, 'failed');
  assert.equal(transport.closed, true);
});

async function waitForFrames(transport: FakeTransport, count: number): Promise<void> {
  for (let attempt = 0; attempt < 50; attempt += 1) {
    if (transport.frames.length >= count) return;
    await new Promise<void>((resolve) => setImmediate(resolve));
  }
  throw new Error(`timed out waiting for ${count} frames; got ${transport.frames.length}`);
}
