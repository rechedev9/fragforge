import test from 'node:test';
import assert from 'node:assert/strict';
import * as http from 'node:http';
import { waitForHttp } from './http-health.ts';

test('resolves when the endpoint becomes healthy', async (t) => {
  let requests = 0;
  const server = http.createServer((_request, response) => {
    requests += 1;
    response.writeHead(requests === 1 ? 503 : 204);
    response.end();
  });
  await listen(server);
  t.after(() => server.close());

  await waitForHttp(serverUrl(server), 1000, new AbortController().signal, { pollIntervalMs: 1 });
  assert.equal(requests, 2);
});

test('cancellation removes pending retries', async (t) => {
  let requests = 0;
  let firstRequest: (() => void) | null = null;
  const firstRequestSeen = new Promise<void>((resolve) => {
    firstRequest = resolve;
  });
  const server = http.createServer((_request, response) => {
    requests += 1;
    firstRequest?.();
    response.writeHead(503);
    response.end();
  });
  await listen(server);
  t.after(() => server.close());
  const controller = new AbortController();
  const waiting = waitForHttp(serverUrl(server), 1000, controller.signal, { pollIntervalMs: 10 });
  await firstRequestSeen;

  controller.abort();
  await assert.rejects(waiting, /cancelled waiting/);
  await delay(30);
  assert.equal(requests, 1);
});

test('rejects after the overall deadline', async (t) => {
  const server = http.createServer((_request, response) => {
    response.writeHead(503);
    response.end();
  });
  await listen(server);
  t.after(() => server.close());

  await assert.rejects(
    waitForHttp(serverUrl(server), 5, new AbortController().signal, { pollIntervalMs: 2 }),
    /timed out waiting/,
  );
});

function listen(server: http.Server): Promise<void> {
  return new Promise((resolve, reject) => {
    server.once('error', reject);
    server.listen(0, '127.0.0.1', resolve);
  });
}

function serverUrl(server: http.Server): string {
  const address = server.address();
  if (address === null || typeof address === 'string') {
    throw new Error('test server has no TCP address');
  }
  return `http://127.0.0.1:${address.port}`;
}

function delay(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}
