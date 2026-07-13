import assert from 'node:assert/strict';
import { mkdtemp, rm, writeFile } from 'node:fs/promises';
import { createServer, type IncomingMessage, type ServerResponse } from 'node:http';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { PassThrough } from 'node:stream';
import test from 'node:test';
import {
  JsonRpcConnection,
  JsonRpcConnectionClosedError,
  JsonRpcResponseError,
  type JsonRpcRequest,
} from './json-rpc.ts';
import { isJsonObject } from './json.ts';
import { OrchestratorClient } from './orchestrator-client.ts';
import { FragForgeMcpServer } from './server.ts';

const JOB_ID = '11111111-1111-4111-8111-111111111111';
const TEST_EVENT_TIMEOUT_MS = 2_000;
const PENDING_OBSERVATION_MS = 50;

test('serves progressive discovery, safe previews, live execution, resources, and elicitation over MCP', async (t) => {
  const requests: Array<{ body: string; method: string; path: string }> = [];
  const httpServer = createServer(async (request, response) => {
    const body = await requestBody(request);
    requests.push({ body, method: request.method ?? 'GET', path: request.url ?? '/' });
    routeFakeOrchestrator(request, response);
  });
  await new Promise<void>((resolve) => httpServer.listen(0, '127.0.0.1', resolve));
  t.after(() => httpServer.close());
  const address = httpServer.address();
  if (address === null || typeof address === 'string') throw new Error('fake orchestrator did not bind TCP');

  const clientToServer = new PassThrough();
  const serverToClient = new PassThrough();
  const diagnostics = new PassThrough();
  const diagnosticsChunks: Buffer[] = [];
  const elicitedFields: string[] = [];
  diagnostics.on('data', (chunk: Buffer) => diagnosticsChunks.push(chunk));
  const server = new FragForgeMcpServer({
    client: new OrchestratorClient({ baseUrl: `http://127.0.0.1:${address.port}` }),
    diagnostics,
    input: clientToServer,
    output: serverToClient,
    serverVersion: 'test',
  });
  let client: JsonRpcConnection;
  client = new JsonRpcConnection({
    input: serverToClient,
    output: clientToServer,
    requestHandler: (request): Promise<void> => answerElicitation(client, request, elicitedFields),
  });
  server.start();
  client.start();
  t.after(() => {
    client.close();
    server.close();
  });

  const initialized = await client.sendRequest('initialize', {
    capabilities: { elicitation: { form: {} } },
    clientInfo: { name: 'test-client', version: '1' },
    protocolVersion: '2025-11-25',
  });
  assert.ok(isJsonObject(initialized));
  assert.equal(initialized.protocolVersion, '2025-11-25');
  assert.ok(typeof initialized.instructions === 'string' && initialized.instructions.startsWith('Search before execute.'));
  await assert.rejects(client.sendRequest('tools/list'), (error: unknown) => {
    assert.ok(error instanceof JsonRpcResponseError);
    assert.equal(error.code, -32_602);
    return true;
  });
  await client.sendNotification('notifications/initialized');

  await assert.rejects(client.sendRequest('initialize', {
    capabilities: {},
    clientInfo: { name: 'repeat', version: '1' },
    protocolVersion: '2025-11-25',
  }), /initialize may only be called once/);

  const tools = await client.sendRequest('tools/list');
  assert.ok(isJsonObject(tools));
  assert.deepEqual(Array.isArray(tools.tools) ? tools.tools.map(toolName) : [], ['search', 'execute']);

  await assert.rejects(client.sendRequest('tools/call', {
    arguments: {},
    name: 'missing-tool',
  }), (error: unknown) => {
    assert.ok(error instanceof JsonRpcResponseError);
    assert.equal(error.code, -32_602);
    assert.equal(error.message, 'Unknown tool: missing-tool');
    return true;
  });

  const invalidOuterInput = await callTool(client, 'search', { unexpected: true });
  assert.equal(invalidOuterInput.isError, true);
  assert.match(String(structured(invalidOuterInput).error), /search\.unexpected is not allowed/);

  const search = await callTool(client, 'search', {
    arguments: { job_id: JOB_ID },
    operation: 'jobs.parse',
  });
  assert.equal(search.isError, undefined);
  const searchContent = structured(search);
  assert.equal(searchContent.count, 1);
  const foundOperations = searchContent.operations;
  assert.ok(Array.isArray(foundOperations) && isJsonObject(foundOperations[0]));
  assert.equal(foundOperations[0].name, 'jobs.parse');
  assert.ok(JSON.stringify(foundOperations[0]).includes('76561198000000000'));

  const beforePreview = requests.length;
  const preview = await callTool(client, 'execute', {
    arguments: { job_id: JOB_ID, target_steamid: '76561198000000000' },
    operation: 'jobs.parse',
  });
  assert.equal(structured(preview).status, 'preview');
  assert.equal(requests.length, beforePreview, 'preview must not dispatch an HTTP mutation');

  const unconfirmed = await callTool(client, 'execute', {
    arguments: { job_id: JOB_ID, target_steamid: '76561198000000000' },
    mode: 'apply',
    operation: 'jobs.parse',
  });
  assert.equal(unconfirmed.isError, true);
  assert.match(String(structured(unconfirmed).error), /confirmed=true/);
  assert.equal(requests.length, beforePreview, 'unconfirmed mutation must not dispatch HTTP');

  const applied = await callTool(client, 'execute', {
    arguments: { job_id: JOB_ID, target_steamid: '76561198000000000' },
    confirmed: true,
    mode: 'apply',
    operation: 'jobs.parse',
  });
  assert.equal(structured(applied).status, 'completed');
  const parseRequest = requests.find((request) => request.path === `/api/jobs/${JOB_ID}/parse`);
  assert.deepEqual(parseRequest, {
    body: '{"target_steamid":"76561198000000000"}',
    method: 'POST',
    path: `/api/jobs/${JOB_ID}/parse`,
  });

  const elicited = await callTool(client, 'execute', { operation: 'jobs.get' });
  assert.equal(structured(elicited).status, 'completed');
  assert.ok(requests.some((request) => request.path === `/api/jobs/${JOB_ID}`));

  const artifact = await callTool(client, 'execute', { operation: 'artifacts.get_url' });
  assert.equal(structured(artifact).status, 'completed');
  assert.deepEqual(elicitedFields, ['job_id', 'job_id', 'kind', 'variant', 'name']);
  assert.match(JSON.stringify(structured(artifact)), /viral-60-clean\/videos\/clip-1/);

  const complexMissingInput = await callTool(client, 'execute', {
    arguments: { stream_job_id: JOB_ID },
    operation: 'streams.update_edit_plan',
  });
  assert.equal(complexMissingInput.isError, true);
  assert.match(String(structured(complexMissingInput).error), /arguments\.plan is required/);

  const status = await callTool(client, 'execute', { operation: 'studio.status' });
  const statusText = JSON.stringify(structured(status));
  assert.doesNotMatch(statusText, /C:\\\\HLAE/);
  assert.match(statusText, /"accessible":true/);

  const resources = await client.sendRequest('resources/read', { uri: 'fragforge://catalog' });
  assert.ok(isJsonObject(resources));
  assert.ok(JSON.stringify(resources).includes('artifacts.get_url'));
  assert.equal(Buffer.concat(diagnosticsChunks).toString('utf8'), '');
});

test('execute validates live variants after preview and before the target request', async (t) => {
  const requests: Array<{ method: string; path: string }> = [];
  let renderRegistry: unknown = { presets: [{ name: 'viral-60-clean' }] };
  const httpServer = createServer(async (request, response) => {
    await requestBody(request);
    const path = request.url ?? '/';
    requests.push({ method: request.method ?? 'GET', path });
    if (path === '/api/presets') return json(response, renderRegistry);
    if (path === '/api/stream-variants') {
      return json(response, { variants: [{ full_frame: true, name: 'streamer-fullframe-nocam' }] });
    }
    if (path === `/api/jobs/${JOB_ID}/renders/viral-60-clean` && request.method === 'POST') {
      return json(response, { id: JOB_ID, status: 'rendering', variant: 'viral-60-clean' }, 202);
    }
    if (path === `/api/stream-jobs/${JOB_ID}/edit-plan` && request.method === 'PUT') {
      return json(response, { status: 'ready' });
    }
    return json(response, { error: 'unexpected target request' }, 500);
  });
  await new Promise<void>((resolve) => httpServer.listen(0, '127.0.0.1', resolve));
  t.after(() => httpServer.close());
  const address = httpServer.address();
  if (address === null || typeof address === 'string') throw new Error('fake orchestrator did not bind TCP');

  const { client, server } = startMcpPair(`http://127.0.0.1:${address.port}`);
  t.after(() => {
    client.close();
    server.close();
  });
  await initializeClient(client, '2025-11-25', {});

  const recordPreview = await callTool(client, 'execute', {
    arguments: { edit: { format: 'short-9x16' }, job_id: JOB_ID, preset: 'invented-preset' },
    operation: 'jobs.record',
  });
  assert.equal(structured(recordPreview).status, 'preview');
  assert.deepEqual(requests, [], 'record preview must not read the preset registry or call the target');

  const unknownPreset = await callTool(client, 'execute', {
    arguments: { job_id: JOB_ID, preset: 'invented-preset' },
    confirmed: true,
    mode: 'apply',
    operation: 'jobs.generate',
  });
  assert.equal(unknownPreset.isError, true);
  assert.match(String(structured(unknownPreset).error), /arguments\.preset "invented-preset"[\s\S]*live render variants: viral-60-clean/);
  assert.deepEqual(
    requests,
    [{ method: 'GET', path: '/api/presets' }],
    'unknown preset must not issue the costly generate request',
  );
  requests.length = 0;

  const preview = await callTool(client, 'execute', {
    arguments: { job_id: JOB_ID, variant: 'invented-variant' },
    operation: 'renders.start',
  });
  assert.equal(structured(preview).status, 'preview');
  assert.deepEqual(requests, [], 'offline preview must not read the registry or call the target');

  const applied = await callTool(client, 'execute', {
    arguments: { job_id: JOB_ID, variant: 'viral-60-clean' },
    confirmed: true,
    mode: 'apply',
    operation: 'renders.start',
  });
  assert.equal(structured(applied).status, 'completed');
  assert.deepEqual(requests, [
    { method: 'GET', path: '/api/presets' },
    { method: 'POST', path: `/api/jobs/${JOB_ID}/renders/viral-60-clean` },
  ]);

  requests.length = 0;
  const unknown = await callTool(client, 'execute', {
    arguments: { job_id: JOB_ID, variant: 'invented-variant' },
    confirmed: true,
    mode: 'apply',
    operation: 'renders.start',
  });
  assert.equal(unknown.isError, true);
  assert.match(String(structured(unknown).error), /live render variants: viral-60-clean/);
  assert.deepEqual(requests, [{ method: 'GET', path: '/api/presets' }], 'unknown variant must not reach its target');

  requests.length = 0;
  renderRegistry = { presets: [{ label: 'missing name' }] };
  const malformed = await callTool(client, 'execute', {
    arguments: { job_id: JOB_ID, variant: 'viral-60-clean' },
    confirmed: true,
    mode: 'apply',
    operation: 'renders.start',
  });
  assert.equal(malformed.isError, true);
  assert.match(String(structured(malformed).error), /live render variant registry response is malformed/);
  assert.deepEqual(requests, [{ method: 'GET', path: '/api/presets' }], 'malformed registry must fail before the target');

  requests.length = 0;
  const unknownStream = await callTool(client, 'execute', {
    arguments: { stream_job_id: JOB_ID, variant: 'invented-stream-layout' },
    operation: 'streams.get_render',
  });
  assert.equal(unknownStream.isError, true);
  assert.match(String(structured(unknownStream).error), /live stream variants: streamer-fullframe-nocam/);
  assert.deepEqual(
    requests,
    [{ method: 'GET', path: '/api/stream-variants' }],
    'unknown stream variant must not issue the render-state GET',
  );

  requests.length = 0;
  const nested = await callTool(client, 'execute', {
    arguments: {
      plan: {
        face_crop: { height: 0.2, width: 0.2, x: 0, y: 0 },
        gameplay_crop: { height: 1, width: 1, x: 0, y: 0 },
        variant: 'invented-stream-layout',
      },
      stream_job_id: JOB_ID,
    },
    confirmed: true,
    mode: 'apply',
    operation: 'streams.update_edit_plan',
  });
  assert.equal(nested.isError, true);
  assert.match(String(structured(nested).error), /arguments\.plan\.variant "invented-stream-layout" is not one of the live stream variants/);
  assert.deepEqual(
    requests,
    [{ method: 'GET', path: '/api/stream-variants' }],
    'unknown nested plan variant must not issue the edit-plan PUT',
  );
});

test('negotiates form elicitation only for protocol versions and capabilities that support it', async (t) => {
  const cases = [
    { capabilities: { elicitation: {} }, name: '2025-11 empty shorthand', protocolVersion: '2025-11-25', wantRequests: 1 },
    { capabilities: { elicitation: { url: {} } }, name: '2025-11 URL-only', protocolVersion: '2025-11-25', wantRequests: 0 },
    { capabilities: { elicitation: {} }, name: '2025-06 form-only protocol', protocolVersion: '2025-06-18', wantRequests: 1 },
    { capabilities: { elicitation: {} }, name: '2025-03 has no elicitation', protocolVersion: '2025-03-26', wantRequests: 0 },
  ] as const;

  for (const testCase of cases) {
    await t.test(testCase.name, async (t) => {
      const clientToServer = new PassThrough();
      const serverToClient = new PassThrough();
      let elicitationRequests = 0;
      const server = new FragForgeMcpServer({
        client: new OrchestratorClient({ baseUrl: 'http://127.0.0.1:1', requestTimeoutMs: 20 }),
        input: clientToServer,
        output: serverToClient,
        serverVersion: 'test',
      });
      let client: JsonRpcConnection;
      client = new JsonRpcConnection({
        input: serverToClient,
        output: clientToServer,
        requestHandler: async (request) => {
          elicitationRequests += 1;
          await client.sendResult(request.id, { action: 'accept', content: { job_id: JOB_ID } });
        },
      });
      server.start();
      client.start();
      t.after(() => {
        client.close();
        server.close();
      });
      await client.sendRequest('initialize', {
        capabilities: testCase.capabilities,
        clientInfo: { name: 'matrix-client', version: '1' },
        protocolVersion: testCase.protocolVersion,
      });
      await client.sendNotification('notifications/initialized');
      const result = await callTool(client, 'execute', { operation: 'jobs.get' });
      assert.equal(result.isError, true);
      assert.equal(elicitationRequests, testCase.wantRequests);
      if (testCase.wantRequests === 0) {
        assert.match(String(structured(result).error), /arguments\.job_id is required/);
      }
    });
  }
});

test('notifications/cancelled stops live search discovery', async (t) => {
  let markRequestStarted: (() => void) | undefined;
  const requestStarted = new Promise<void>((resolve) => {
    markRequestStarted = resolve;
  });
  let markResponseClosed: (() => void) | undefined;
  const responseClosed = new Promise<void>((resolve) => {
    markResponseClosed = resolve;
  });
  const httpServer = createServer((_request, response) => {
    markRequestStarted?.();
    response.once('close', () => markResponseClosed?.());
    // Leave the response open until cancellation aborts the client request.
  });
  await new Promise<void>((resolve) => httpServer.listen(0, '127.0.0.1', resolve));
  t.after(() => {
    httpServer.closeAllConnections();
    httpServer.close();
  });
  const address = httpServer.address();
  if (address === null || typeof address === 'string') throw new Error('fake orchestrator did not bind TCP');

  const { client, server } = startMcpPair(`http://127.0.0.1:${address.port}`);
  t.after(() => {
    client.close();
    server.close();
  });
  await initializeClient(client, '2025-11-25', {});

  const search = callTool(client, 'search', { operation: 'jobs.get' });
  await withTestTimeout(requestStarted, 'live search request to start');
  await client.sendNotification('notifications/cancelled', { reason: 'test', requestId: 2 });
  await withTestTimeout(responseClosed, 'cancelled live search response to close');
  await assertRemainsPending(search);
  client.close();
  await assert.rejects(search, JsonRpcConnectionClosedError);
});

test('notifications/cancelled stops a pending elicitation request', async (t) => {
  const clientToServer = new PassThrough();
  const serverToClient = new PassThrough();
  let markElicitationStarted: (() => void) | undefined;
  const elicitationStarted = new Promise<void>((resolve) => {
    markElicitationStarted = resolve;
  });
  let cancelledRequestID: unknown;
  const server = new FragForgeMcpServer({
    client: new OrchestratorClient({ baseUrl: 'http://127.0.0.1:1', requestTimeoutMs: 20 }),
    input: clientToServer,
    output: serverToClient,
    serverVersion: 'test',
  });
  const client = new JsonRpcConnection({
    input: serverToClient,
    notificationHandler: (notification) => {
      if (notification.method !== 'notifications/cancelled' || !isJsonObject(notification.params)) return;
      cancelledRequestID = notification.params.requestId;
    },
    output: clientToServer,
    requestHandler: () => {
      markElicitationStarted?.();
      // The test client intentionally leaves elicitation unanswered.
    },
  });
  server.start();
  client.start();
  t.after(() => {
    client.close();
    server.close();
  });
  await initializeClient(client, '2025-11-25', { elicitation: {} });

  const execution = callTool(client, 'execute', { operation: 'jobs.get' });
  await withTestTimeout(elicitationStarted, 'elicitation request to start');
  await client.sendNotification('notifications/cancelled', { reason: 'test', requestId: 2 });
  await waitFor(() => cancelledRequestID === 1);
  await assertRemainsPending(execution);
  client.close();
  await assert.rejects(execution, JsonRpcConnectionClosedError);
  assert.equal(cancelledRequestID, 1);
});

test('pending elicitation has a bounded configurable timeout', async (t) => {
  const clientToServer = new PassThrough();
  const serverToClient = new PassThrough();
  let markElicitationStarted: (() => void) | undefined;
  const elicitationStarted = new Promise<void>((resolve) => {
    markElicitationStarted = resolve;
  });
  let cancelledRequestID: unknown;
  const server = new FragForgeMcpServer({
    client: new OrchestratorClient({ baseUrl: 'http://127.0.0.1:1', requestTimeoutMs: 20 }),
    elicitationTimeoutMs: 20,
    input: clientToServer,
    output: serverToClient,
    serverVersion: 'test',
  });
  const client = new JsonRpcConnection({
    input: serverToClient,
    notificationHandler: (notification) => {
      if (notification.method !== 'notifications/cancelled' || !isJsonObject(notification.params)) return;
      cancelledRequestID = notification.params.requestId;
    },
    output: clientToServer,
    requestHandler: () => {
      markElicitationStarted?.();
      // The client advertises elicitation but intentionally never answers.
    },
  });
  server.start();
  client.start();
  t.after(() => {
    client.close();
    server.close();
  });
  await initializeClient(client, '2025-11-25', { elicitation: {} });

  const execution = callTool(client, 'execute', { operation: 'jobs.get' });
  await withTestTimeout(elicitationStarted, 'timed elicitation request to start');
  const result = await withTestTimeout(execution, 'timed elicitation request to finish');

  assert.equal(result.isError, true);
  assert.match(String(structured(result).error), /elicitation timed out after 20ms/);
  assert.equal(cancelledRequestID, 1);
});

test('notifications/cancelled aborts a live status resource read', async (t) => {
  let markRequestStarted: (() => void) | undefined;
  const requestStarted = new Promise<void>((resolve) => {
    markRequestStarted = resolve;
  });
  let closedResponses = 0;
  const httpServer = createServer((_request, response) => {
    markRequestStarted?.();
    response.once('close', () => {
      closedResponses += 1;
    });
    // Leave both studio.status responses open until cancellation aborts them.
  });
  await new Promise<void>((resolve) => httpServer.listen(0, '127.0.0.1', resolve));
  t.after(() => {
    httpServer.closeAllConnections();
    httpServer.close();
  });
  const address = httpServer.address();
  if (address === null || typeof address === 'string') throw new Error('fake orchestrator did not bind TCP');

  const { client, server } = startMcpPair(`http://127.0.0.1:${address.port}`);
  t.after(() => {
    client.close();
    server.close();
  });
  await initializeClient(client, '2025-11-25', {});

  const resource = client.sendRequest('resources/read', { uri: 'fragforge://status' });
  await withTestTimeout(requestStarted, 'status resource request to start');
  await client.sendNotification('notifications/cancelled', { reason: 'test', requestId: 2 });
  await waitFor(() => closedResponses > 0);
  await assertRemainsPending(resource);
  client.close();
  await assert.rejects(resource, JsonRpcConnectionClosedError);
});

test('rejects excess concurrent tool calls with a bounded server error', async (t) => {
  let markRequestStarted: (() => void) | undefined;
  const requestStarted = new Promise<void>((resolve) => {
    markRequestStarted = resolve;
  });
  const httpServer = createServer((_request, _response) => {
    markRequestStarted?.();
    // Hold the first request so the concurrency limit remains occupied.
  });
  await new Promise<void>((resolve) => httpServer.listen(0, '127.0.0.1', resolve));
  t.after(() => {
    httpServer.closeAllConnections();
    httpServer.close();
  });
  const address = httpServer.address();
  if (address === null || typeof address === 'string') throw new Error('fake orchestrator did not bind TCP');

  const { client, server } = startMcpPair(`http://127.0.0.1:${address.port}`, { maxConcurrentRequests: 1 });
  t.after(() => {
    client.close();
    server.close();
  });
  await initializeClient(client, '2025-11-25', {});

  const first = callTool(client, 'execute', { arguments: { limit: 1 }, operation: 'jobs.list' });
  await withTestTimeout(requestStarted, 'concurrency-limit request to start');
  await assert.rejects(client.sendRequest('tools/call', {
    arguments: { arguments: { limit: 1 }, operation: 'jobs.list' },
    name: 'execute',
  }), (error: unknown) => {
    assert.ok(error instanceof JsonRpcResponseError);
    assert.equal(error.code, -32_001);
    assert.equal(error.message, 'FragForge MCP is busy; retry later');
    return true;
  });

  await client.sendNotification('notifications/cancelled', { reason: 'test cleanup', requestId: 2 });
  await assertRemainsPending(first);
  client.close();
  await assert.rejects(first, JsonRpcConnectionClosedError);
});

test('rejects a duplicate active JSON-RPC id across methods before it can produce two responses', async (t) => {
  let httpRequests = 0;
  let markRequestStarted: (() => void) | undefined;
  const requestStarted = new Promise<void>((resolve) => {
    markRequestStarted = resolve;
  });
  const httpServer = createServer((_request, _response) => {
    httpRequests += 1;
    markRequestStarted?.();
    // Keep the first request active while the duplicate frame arrives.
  });
  await new Promise<void>((resolve) => httpServer.listen(0, '127.0.0.1', resolve));
  t.after(() => {
    httpServer.closeAllConnections();
    httpServer.close();
  });
  const address = httpServer.address();
  if (address === null || typeof address === 'string') throw new Error('fake orchestrator did not bind TCP');

  const { client, input, protocolErrors, server } = startMcpPair(`http://127.0.0.1:${address.port}`, { maxConcurrentRequests: 1 });
  t.after(() => {
    client.close();
    server.close();
  });
  await initializeClient(client, '2025-11-25', {});

  const first = client.sendRequest('tools/call', {
    arguments: { arguments: { limit: 1 }, operation: 'jobs.list' },
    name: 'execute',
  });
  await withTestTimeout(requestStarted, 'duplicate-id request to start');
  input.write(`${JSON.stringify({
    id: 2,
    jsonrpc: '2.0',
    method: 'ping',
  })}\n`);

  await assert.rejects(first, (error: unknown) => {
    assert.ok(error instanceof JsonRpcResponseError);
    assert.equal(error.code, -32_600);
    assert.equal(error.message, 'request id is already active');
    return true;
  });
  assert.equal(httpRequests, 1);
  await waitFor(() => server.closed);
  assert.equal(server.closed, true);
  await new Promise<void>((resolve) => setImmediate(resolve));
  assert.deepEqual(protocolErrors, []);
});

test('surfaces a recoverable operation result as partial instead of completed', async (t) => {
  const httpServer = createServer((_request, response) => {
    response.setHeader('Content-Type', 'application/json');
    response.end(JSON.stringify({ partial: true, stream_job_id: JOB_ID }));
  });
  await new Promise<void>((resolve) => httpServer.listen(0, '127.0.0.1', resolve));
  t.after(() => httpServer.close());
  const address = httpServer.address();
  if (address === null || typeof address === 'string') throw new Error('fake orchestrator did not bind TCP');

  const { client, server } = startMcpPair(`http://127.0.0.1:${address.port}`);
  t.after(() => {
    client.close();
    server.close();
  });
  await initializeClient(client, '2025-11-25', {});

  const result = await callTool(client, 'execute', {
    arguments: { limit: 1 },
    operation: 'jobs.list',
  });

  const resultContent = structured(result);
  const partialResult = resultContent.result;
  assert.equal(resultContent.status, 'partial');
  assert.equal(isJsonObject(partialResult) ? partialResult.partial : undefined, true);
});

test('surfaces a cancelled post-create result as an error with durable recovery details', async (t) => {
  const httpServer = createServer((_request, response) => {
    response.setHeader('Content-Type', 'application/json');
    response.end(JSON.stringify({
      cancelled: true,
      error: 'operation was cancelled after creation',
      partial: true,
      recovery: { retry_create_from_file: false },
      stream_job_id: JOB_ID,
    }));
  });
  await new Promise<void>((resolve) => httpServer.listen(0, '127.0.0.1', resolve));
  t.after(() => httpServer.close());
  const address = httpServer.address();
  if (address === null || typeof address === 'string') throw new Error('fake orchestrator did not bind TCP');

  const { client, server } = startMcpPair(`http://127.0.0.1:${address.port}`);
  t.after(() => {
    client.close();
    server.close();
  });
  await initializeClient(client, '2025-11-25', {});

  const result = await callTool(client, 'execute', {
    arguments: { limit: 1 },
    operation: 'jobs.list',
  });

  assert.equal(result.isError, true);
  const resultContent = structured(result);
  assert.equal(resultContent.status, 'partial');
  assert.match(String(resultContent.error), /cancelled after creation/);
  const partialResult = resultContent.result;
  assert.ok(isJsonObject(partialResult));
  assert.equal(partialResult.cancelled, true);
  assert.equal(partialResult.stream_job_id, JOB_ID);
  assert.deepEqual(partialResult.recovery, { retry_create_from_file: false });
});

test('executes the returned stream initialization recovery step verbatim without re-uploading', async (t) => {
  const tempDirectory = await mkdtemp(join(tmpdir(), 'fragforge-mcp-recovery-'));
  const videoPath = join(tempDirectory, 'stream.mp4');
  await writeFile(videoPath, Buffer.from('video'));
  t.after(() => rm(tempDirectory, { force: true, recursive: true }));

  const plan = {
    gameplay_crop: { height: 1, width: 1, x: 0, y: 0 },
    variant: 'streamer-fullframe-nocam',
  };
  const readyPlan = { ...plan, updated_at: '2026-07-13T12:00:00Z' };
  const readyJob = { id: JOB_ID, status: 'ready' };
  let editPlanReads = 0;
  let persistedPlan = '';
  let uploadRequests = 0;
  const httpServer = createServer(async (request, response) => {
    const url = request.url ?? '/';
    if (request.method === 'POST' && url === '/api/stream-jobs') {
      await requestBody(request);
      uploadRequests += 1;
      json(response, { id: JOB_ID, status: 'uploaded' }, 201);
      return;
    }
    if (request.method === 'GET' && url === `/api/stream-jobs/${JOB_ID}/edit-plan`) {
      editPlanReads += 1;
      if (editPlanReads === 1) {
        json(response, { error: 'edit plan temporarily unavailable' }, 503);
        return;
      }
      json(response, plan);
      return;
    }
    if (request.method === 'PUT' && url === `/api/stream-jobs/${JOB_ID}/edit-plan`) {
      persistedPlan = await requestBody(request);
      json(response, readyPlan);
      return;
    }
    if (request.method === 'GET' && url === `/api/stream-jobs/${JOB_ID}`) {
      json(response, readyJob);
      return;
    }
    json(response, { error: 'not found' }, 404);
  });
  await new Promise<void>((resolve) => httpServer.listen(0, '127.0.0.1', resolve));
  t.after(() => httpServer.close());
  const address = httpServer.address();
  if (address === null || typeof address === 'string') throw new Error('fake orchestrator did not bind TCP');

  const { client, server } = startMcpPair(`http://127.0.0.1:${address.port}`);
  t.after(() => {
    client.close();
    server.close();
  });
  await initializeClient(client, '2025-11-25', {});

  const createContent = structured(await callTool(client, 'execute', {
    arguments: { video_path: videoPath },
    confirmed: true,
    mode: 'apply',
    operation: 'streams.create_from_file',
  }));
  assert.equal(createContent.status, 'partial');
  const partialResult = createContent.result;
  assert.ok(isRecord(partialResult));
  const recovery = partialResult.recovery;
  assert.ok(isRecord(recovery));
  assert.ok(Array.isArray(recovery.steps));
  assert.equal(recovery.steps.length, 1);
  const step = recovery.steps[0];
  assert.ok(isRecord(step));
  assert.deepEqual(step, {
    arguments: { stream_job_id: JOB_ID },
    confirmed: true,
    mode: 'apply',
    operation: 'streams.resume_initialization',
  });

  const recoveredContent = structured(await callTool(client, 'execute', step));
  assert.equal(recoveredContent.status, 'completed');
  assert.deepEqual(recoveredContent.result, { edit_plan: readyPlan, job: readyJob });
  assert.equal(persistedPlan, JSON.stringify(plan));
  assert.equal(editPlanReads, 2);
  assert.equal(uploadRequests, 1, 'recovery must not upload the video again');
});

test('cancelled stream upload stays durable and is recovered through list and get without a response', async (t) => {
  const tempDirectory = await mkdtemp(join(tmpdir(), 'fragforge-mcp-cancel-'));
  const videoPath = join(tempDirectory, 'stream.mp4');
  await writeFile(videoPath, Buffer.from('video'));
  t.after(() => rm(tempDirectory, { force: true, recursive: true }));

  let editPlanRequested = false;
  let uploadRequests = 0;
  const httpServer = createServer(async (request, response) => {
    const url = request.url ?? '/';
    if (request.method === 'POST' && url === '/api/stream-jobs') {
      await requestBody(request);
      uploadRequests += 1;
      json(response, { id: JOB_ID, status: 'uploaded' }, 201);
      return;
    }
    if (request.method === 'GET' && url === `/api/stream-jobs/${JOB_ID}/edit-plan`) {
      editPlanRequested = true;
      return;
    }
    if (request.method === 'GET' && url === '/api/stream-jobs?limit=10') {
      json(response, { jobs: [{ id: JOB_ID, status: 'uploaded' }] });
      return;
    }
    if (request.method === 'GET' && url === `/api/stream-jobs/${JOB_ID}`) {
      json(response, { id: JOB_ID, status: 'uploaded' });
      return;
    }
    json(response, { error: 'not found' }, 404);
  });
  await new Promise<void>((resolve) => httpServer.listen(0, '127.0.0.1', resolve));
  t.after(() => httpServer.close());
  const address = httpServer.address();
  if (address === null || typeof address === 'string') throw new Error('fake orchestrator did not bind TCP');

  const { client, server } = startMcpPair(`http://127.0.0.1:${address.port}`);
  t.after(() => {
    client.close();
    server.close();
  });
  await initializeClient(client, '2025-11-25', {});

  const cancelledUpload = client.sendRequest('tools/call', {
    arguments: {
      arguments: { video_path: videoPath },
      confirmed: true,
      mode: 'apply',
      operation: 'streams.create_from_file',
    },
    name: 'execute',
  });
  void cancelledUpload.catch(() => undefined);
  await waitFor(() => editPlanRequested);
  await client.sendNotification('notifications/cancelled', { reason: 'test', requestId: 2 });
  await assertRemainsPending(cancelledUpload);

  const listed = structured(await callTool(client, 'execute', {
    arguments: { limit: 10 },
    operation: 'streams.list',
  }));
  const listResult = listed.result;
  assert.ok(isRecord(listResult));
  assert.ok(Array.isArray(listResult.jobs));
  assert.equal(isRecord(listResult.jobs[0]) ? listResult.jobs[0].id : undefined, JOB_ID);

  const recovered = structured(await callTool(client, 'execute', {
    arguments: { stream_job_id: JOB_ID },
    operation: 'streams.get',
  }));
  assert.ok(isRecord(recovered.result));
  assert.equal(recovered.result.id, JOB_ID);
  assert.equal(uploadRequests, 1, 'recovery must not upload the video again');
  await assertRemainsPending(cancelledUpload);
});

async function answerElicitation(
  client: JsonRpcConnection,
  request: JsonRpcRequest,
  fields: string[] = [],
): Promise<void> {
  assert.equal(request.method, 'elicitation/create');
  assert.ok(isJsonObject(request.params));
  const requestedSchema = request.params.requestedSchema;
  assert.ok(isJsonObject(requestedSchema));
  const properties = requestedSchema.properties;
  assert.ok(isJsonObject(properties));
  const field = Object.keys(properties)[0];
  if (field === undefined) throw new Error('elicitation schema has no field');
  const values: Record<string, string> = {
    job_id: JOB_ID,
    kind: 'video',
    name: 'clip-1',
    variant: 'viral-60-clean',
  };
  const value = values[field];
  if (value === undefined) throw new Error(`unexpected elicitation field ${field}`);
  fields.push(field);
  await client.sendResult(request.id, { action: 'accept', content: { [field]: value } });
}

async function callTool(client: JsonRpcConnection, name: string, args: Record<string, unknown>): Promise<Record<string, unknown>> {
  const result = await client.sendRequest('tools/call', { arguments: args, name });
  assert.ok(isRecord(result));
  return result;
}

function startMcpPair(
  baseUrl: string,
  options: { maxConcurrentRequests?: number } = {},
): { client: JsonRpcConnection; input: PassThrough; protocolErrors: Error[]; server: FragForgeMcpServer } {
  const clientToServer = new PassThrough();
  const serverToClient = new PassThrough();
  const server = new FragForgeMcpServer({
    client: new OrchestratorClient({ baseUrl, requestTimeoutMs: 10_000 }),
    input: clientToServer,
    maxConcurrentRequests: options.maxConcurrentRequests,
    output: serverToClient,
    serverVersion: 'test',
  });
  const protocolErrors: Error[] = [];
  const client = new JsonRpcConnection({
    errorHandler: (error) => protocolErrors.push(error),
    input: serverToClient,
    output: clientToServer,
  });
  server.start();
  client.start();
  return { client, input: clientToServer, protocolErrors, server };
}

async function waitFor(predicate: () => boolean, timeoutMs = 2_000): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    if (predicate()) return;
    await new Promise<void>((resolve) => setTimeout(resolve, 5));
  }
  throw new Error(`timed out after ${timeoutMs}ms waiting for condition`);
}

async function withTestTimeout<T>(
  promise: Promise<T>,
  label: string,
  timeoutMs = TEST_EVENT_TIMEOUT_MS,
): Promise<T> {
  let timeout: ReturnType<typeof setTimeout> | undefined;
  try {
    return await Promise.race([
      promise,
      new Promise<never>((_resolve, reject) => {
        timeout = setTimeout(() => reject(new Error(`timed out after ${timeoutMs}ms waiting for ${label}`)), timeoutMs);
      }),
    ]);
  } finally {
    if (timeout !== undefined) clearTimeout(timeout);
  }
}

async function assertRemainsPending(promise: Promise<unknown>): Promise<void> {
  let timeout: ReturnType<typeof setTimeout> | undefined;
  try {
    const state = await Promise.race([
      promise.then(
        () => 'resolved' as const,
        () => 'rejected' as const,
      ),
      new Promise<'pending'>((resolve) => {
        timeout = setTimeout(() => resolve('pending'), PENDING_OBSERVATION_MS);
      }),
    ]);
    assert.equal(state, 'pending', 'cancelled request must not receive a response');
  } finally {
    if (timeout !== undefined) clearTimeout(timeout);
  }
}

async function initializeClient(client: JsonRpcConnection, protocolVersion: string, capabilities: Record<string, unknown>): Promise<void> {
  await client.sendRequest('initialize', {
    capabilities,
    clientInfo: { name: 'cancellation-client', version: '1' },
    protocolVersion,
  });
  await client.sendNotification('notifications/initialized');
}

function structured(result: Record<string, unknown>): Record<string, unknown> {
  assert.ok(isRecord(result.structuredContent));
  return result.structuredContent;
}

function toolName(value: unknown): string | undefined {
  return isRecord(value) && typeof value.name === 'string' ? value.name : undefined;
}

async function requestBody(request: IncomingMessage): Promise<string> {
  const chunks: Buffer[] = [];
  for await (const chunk of request) chunks.push(Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk));
  return Buffer.concat(chunks).toString('utf8');
}

function routeFakeOrchestrator(request: IncomingMessage, response: ServerResponse): void {
  const url = request.url ?? '/';
  if (url === '/healthz') return json(response, { status: 'ok' });
  if (url === '/api/presets') return json(response, { presets: [{ name: 'viral-60-clean' }] });
  if (url === '/api/capabilities') {
    return json(response, { record: { enabled: true, tools: [{ accessible: true, name: 'ZV_HLAE_PATH', path: 'C:\\HLAE\\HLAE.exe' }] } });
  }
  if (url === '/api/jobs?limit=100') return json(response, { jobs: [{ id: JOB_ID, status: 'scanned' }] });
  if (url === '/api/presets') return json(response, { presets: [{ name: 'viral-60-clean' }] });
  if (url === `/api/jobs/${JOB_ID}/roster`) {
    return json(response, { players: [{ name: 'target', steamid64: '76561198000000000', team: 'T' }] });
  }
  if (url === `/api/jobs/${JOB_ID}/parse` && request.method === 'POST') return json(response, { id: JOB_ID, status: 'parsing' }, 202);
  if (url === `/api/jobs/${JOB_ID}`) return json(response, { id: JOB_ID, status: 'parsed' });
  if (url === `/api/jobs/${JOB_ID}/renders/viral-60-clean/videos/clip-1`) {
    response.setHeader('Content-Type', 'video/mp4');
    response.end('video');
    return;
  }
  response.statusCode = 404;
  json(response, { error: 'not found' });
}

function json(response: ServerResponse, value: unknown, status = 200): void {
  response.statusCode = status;
  response.setHeader('Content-Type', 'application/json');
  response.end(JSON.stringify(value));
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}
