import assert from 'node:assert/strict';
import { createServer, type ServerResponse } from 'node:http';
import test from 'node:test';
import { McpOperationGateway } from './operation-gateway.ts';
import { OrchestratorClient } from './orchestrator-client.ts';

const JOB_ID = '11111111-1111-4111-8111-111111111111';

test('runs reads but previews mutations until explicitly privileged', async (t) => {
  const requests: Array<{ method: string; path: string }> = [];
  const server = createServer((request, response) => {
    requests.push({ method: request.method ?? 'GET', path: request.url ?? '/' });
    if (request.url === `/api/jobs/${JOB_ID}` && request.method === 'GET') {
      return json(response, { id: JOB_ID, status: 'parsed' });
    }
    if (request.url === `/api/jobs/${JOB_ID}/parse` && request.method === 'POST') {
      return json(response, { id: JOB_ID, status: 'parsing' });
    }
    return json(response, { error: 'unexpected request' }, 500);
  });
  await listen(server);
  t.after(() => server.close());

  const gateway = new McpOperationGateway({ client: new OrchestratorClient({ baseUrl: serverUrl(server) }) });
  const preview = await gateway.execute({
    arguments: { job_id: JOB_ID, target_steamid: '76561198000000000' },
    operation: 'jobs.parse',
  });
  assert.equal(preview.kind, 'preview');
  assert.equal(preview.operation, 'jobs.parse');
  assert.equal(preview.risk, 'write');
  assert.equal(preview.requiresConfirmation, true);
  assert.deepEqual(requests, [], 'an unprivileged mutation must not reach the orchestrator');

  const read = await gateway.execute({ arguments: { job_id: JOB_ID }, operation: 'jobs.get' });
  assert.equal(read.kind, 'executed');
  assert.equal(read.status, 'completed');
  assert.deepEqual(read.result, { id: JOB_ID, status: 'parsed' });
  assert.deepEqual(requests, [{ method: 'GET', path: `/api/jobs/${JOB_ID}` }]);

  const applied = await gateway.execute({
    arguments: { job_id: JOB_ID, target_steamid: '76561198000000000' },
    operation: 'jobs.parse',
  }, { privileged: true });
  assert.equal(applied.kind, 'executed');
  assert.equal(applied.status, 'completed');
  assert.deepEqual(requests, [
    { method: 'GET', path: `/api/jobs/${JOB_ID}` },
    { method: 'POST', path: `/api/jobs/${JOB_ID}/parse` },
  ]);
});

test('keeps mutation previews offline and validates live inputs before privileged dispatch', async (t) => {
  const requests: Array<{ method: string; path: string }> = [];
  const server = createServer((request, response) => {
    requests.push({ method: request.method ?? 'GET', path: request.url ?? '/' });
    if (request.url === '/api/presets' && request.method === 'GET') {
      return json(response, { presets: [{ name: 'viral-60-clean' }] });
    }
    return json(response, { error: 'unexpected request' }, 500);
  });
  await listen(server);
  t.after(() => server.close());

  const gateway = new McpOperationGateway({ client: new OrchestratorClient({ baseUrl: serverUrl(server) }) });
  const request = { arguments: { job_id: JOB_ID, variant: 'invented-variant' }, operation: 'renders.start' };
  const preview = await gateway.execute(request);
  assert.equal(preview.kind, 'preview');
  assert.deepEqual(requests, [], 'a preview must not fetch the live variant registry');

  await assert.rejects(gateway.execute(request, { privileged: true }), /live render variants: viral-60-clean/);
  assert.deepEqual(requests, [{ method: 'GET', path: '/api/presets' }]);
});

function json(response: ServerResponse, value: unknown, status = 200): void {
  response.writeHead(status, { 'content-type': 'application/json' });
  response.end(JSON.stringify(value));
}

function listen(server: ReturnType<typeof createServer>): Promise<void> {
  return new Promise((resolve, reject) => {
    server.once('error', reject);
    server.listen(0, '127.0.0.1', resolve);
  });
}

function serverUrl(server: ReturnType<typeof createServer>): string {
  const address = server.address();
  if (address === null || typeof address === 'string') throw new Error('fake orchestrator did not bind TCP');
  return `http://127.0.0.1:${address.port}`;
}
