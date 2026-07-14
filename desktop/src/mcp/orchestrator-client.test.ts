import test from 'node:test';
import assert from 'node:assert/strict';
import { Buffer } from 'node:buffer';
import { createHmac } from 'node:crypto';
import * as fs from 'node:fs';
import * as http from 'node:http';
import * as os from 'node:os';
import * as path from 'node:path';
import {
  OrchestratorClient,
  OrchestratorHttpError,
} from './orchestrator-client.ts';

type HttpResponder = (
  request: http.IncomingMessage,
  response: http.ServerResponse,
) => void | Promise<void>;

interface FakeServer {
  baseUrl: string;
  port: number;
}

async function startServer(t: test.TestContext, responder: HttpResponder): Promise<FakeServer> {
  const server = http.createServer((request, response) => {
    void Promise.resolve(responder(request, response)).catch((error: unknown) => {
      response.statusCode = 500;
      response.end(String(error));
    });
  });
  await new Promise<void>((resolve, reject) => {
    server.once('error', reject);
    server.listen(0, '127.0.0.1', resolve);
  });
  const address = server.address();
  if (address === null || typeof address === 'string') throw new Error('fake server has no TCP address');
  t.after(async () => {
    server.closeAllConnections();
    await new Promise<void>((resolve) => server.close(() => resolve()));
  });
  return { baseUrl: `http://127.0.0.1:${address.port}`, port: address.port };
}

function temporaryDirectory(t: test.TestContext): string {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'fragforge-mcp-client-'));
  t.after(() => fs.rmSync(directory, { force: true, recursive: true }));
  return directory;
}

function sendJson(response: http.ServerResponse, value: unknown, status = 200): void {
  response.statusCode = status;
  response.setHeader('Content-Type', 'application/json');
  response.end(JSON.stringify(value));
}

function answerVerifiedHealth(request: http.IncomingMessage, response: http.ServerResponse, token: string): boolean {
  const url = new URL(request.url ?? '/', 'http://127.0.0.1');
  if (url.pathname !== '/healthz') return false;
  const challenge = url.searchParams.get('challenge') ?? '';
  const endpoint = request.headers.host ?? '';
  sendJson(response, {
    endpoint,
    proof: createHmac('sha256', token).update(`${challenge}\n${endpoint}`).digest('hex'),
    service: 'fragforge',
    status: 'ok',
  });
  return true;
}

async function readBody(request: http.IncomingMessage): Promise<Buffer> {
  const chunks: Buffer[] = [];
  for await (const chunk of request) {
    if (Buffer.isBuffer(chunk)) chunks.push(chunk);
    else if (chunk instanceof Uint8Array) chunks.push(Buffer.from(chunk));
    else chunks.push(Buffer.from(String(chunk)));
  }
  return Buffer.concat(chunks);
}

async function unusedLoopbackPort(): Promise<number> {
  const server = http.createServer();
  await new Promise<void>((resolve, reject) => {
    server.once('error', reject);
    server.listen(0, '127.0.0.1', resolve);
  });
  const address = server.address();
  if (address === null || typeof address === 'string') throw new Error('port probe has no TCP address');
  const { port } = address;
  await new Promise<void>((resolve) => server.close(() => resolve()));
  return port;
}

async function rejectedValue(promise: Promise<unknown>): Promise<unknown> {
  let rejection: unknown;
  try {
    await promise;
  } catch (error) {
    rejection = error;
  }
  if (rejection === undefined) throw new Error('expected promise to reject');
  return rejection;
}

async function waitForProbeCancellation(closed: Promise<void>): Promise<void> {
  let timeout: NodeJS.Timeout | undefined;
  try {
    await Promise.race([
      closed,
      new Promise<void>((_resolve, reject) => {
        timeout = setTimeout(() => reject(new Error('artifact probe response body was not cancelled')), 1_000);
      }),
    ]);
  } finally {
    if (timeout !== undefined) clearTimeout(timeout);
  }
}

test('discovers and normalizes the loopback orchestrator URL from ports.json', async (t) => {
  const directory = temporaryDirectory(t);
  const portsFile = path.join(directory, 'ports.json');
  fs.writeFileSync(portsFile, JSON.stringify({ discovery_secret: 'a'.repeat(64), keep: true, orchestrator: 43_210, web: 43_211 }));

  const client = new OrchestratorClient({ portsFile });

  assert.equal(await client.baseUrl(), 'http://127.0.0.1:43210');
});

test('rejects mutation tokens too short to redact safely', () => {
  assert.throws(
    () => new OrchestratorClient({ mutationToken: 'a' }),
    /mutationToken must contain at least 8 characters/,
  );
});

test('an explicit loopback URL wins over a malformed ports file and loses its trailing slash', async (t) => {
  const directory = temporaryDirectory(t);
  const portsFile = path.join(directory, 'ports.json');
  fs.writeFileSync(portsFile, '{broken');

  const client = new OrchestratorClient({
    baseUrl: 'http://localhost:32123/',
    portsFile,
  });

  assert.equal(await client.baseUrl(), 'http://localhost:32123');
});

test('accepts bracketed IPv6 loopback URLs', async () => {
  const client = new OrchestratorClient({ baseUrl: 'http://[::1]:32123/' });

  assert.equal(await client.baseUrl(), 'http://[::1]:32123');
});

test('rejects invalid ports files and non-loopback or credential-bearing URLs', async (t) => {
  const directory = temporaryDirectory(t);
  const portsFile = path.join(directory, 'ports.json');
  const invalidPorts: unknown[] = [0, 65_536, 3.5, '8080', null];
  for (const port of invalidPorts) {
    fs.writeFileSync(portsFile, JSON.stringify({ orchestrator: port }));
    await assert.rejects(
      new OrchestratorClient({ portsFile }).baseUrl(),
      /invalid orchestrator port/,
    );
  }
  for (const discoverySecret of ['', 'a'.repeat(63), 'A'.repeat(64), 'g'.repeat(64), 42]) {
    fs.writeFileSync(portsFile, JSON.stringify({ discovery_secret: discoverySecret, orchestrator: 8080 }));
    await assert.rejects(
      new OrchestratorClient({ mutationToken: 'fallback-token', portsFile }).baseUrl(),
      /invalid discovery_secret/,
    );
  }

  const invalidUrls = [
    'https://127.0.0.1:8080',
    'http://example.com:8080',
    'http://user:secret@127.0.0.1:8080',
    'http://127.0.0.1:8080?token=secret',
    'http://127.0.0.1:8080/#fragment',
  ];
  for (const baseUrl of invalidUrls) {
    const error = await rejectedValue(new OrchestratorClient({ baseUrl }).baseUrl());
    assert.equal(String(error).includes('secret'), false);
  }
});

test('keeps request and artifact paths on the configured origin', async (t) => {
  const client = new OrchestratorClient({ baseUrl: 'http://127.0.0.1:12345' });

  await assert.rejects(
    client.request({ path: 'http://example.com/api/jobs' }),
    /escaped the loopback origin/,
  );
  await assert.rejects(
    client.artifactUrl('//example.com/video.mp4'),
    /escaped the loopback origin/,
  );
  await assert.rejects(
    client.requestText('http://example.com/metrics'),
    /escaped the loopback origin/,
  );
  const server = await startServer(t, (_request, response) => sendJson(response, { auth: { read_requires_token: false } }));
  const safeClient = new OrchestratorClient({ baseUrl: server.baseUrl });
  assert.equal(await safeClient.artifactUrl('/api/jobs/job-1/final'), `${server.baseUrl}/api/jobs/job-1/final`);
});

test('returns artifact URLs only after the concrete artifact is available', async (t) => {
  const methods: string[] = [];
  const server = await startServer(t, (request, response) => {
    methods.push(`${request.method} ${request.url}`);
    if (request.url === '/api/capabilities') {
      sendJson(response, { auth: { read_requires_token: false } });
      return;
    }
    if (request.url === '/api/jobs/job-1/final' && request.method === 'GET') {
      response.statusCode = 404;
      response.end();
      return;
    }
    sendJson(response, { error: 'not found' }, 404);
  });
  const client = new OrchestratorClient({ baseUrl: server.baseUrl });

  await assert.rejects(client.artifactUrl('/api/jobs/job-1/final'), (error: unknown) => {
    assert.ok(error instanceof OrchestratorHttpError);
    assert.equal(error.status, 404);
    assert.match(error.message, /artifact is unavailable/);
    return true;
  });
  assert.deepEqual(methods, ['GET /api/capabilities', 'GET /api/jobs/job-1/final']);
});

test('probes artifact availability with a bounded range and cancels the response body', async (t) => {
  let rangeHeader: string | undefined;
  let markProbeClosed: (() => void) | undefined;
  const probeClosed = new Promise<void>((resolve) => {
    markProbeClosed = resolve;
  });
  const server = await startServer(t, (request, response) => {
    if (request.url === '/api/capabilities') {
      sendJson(response, { auth: { read_requires_token: false } });
      return;
    }
    rangeHeader = request.headers.range;
    response.once('close', () => markProbeClosed?.());
    response.writeHead(206, {
      'Content-Length': 1_000_000,
      'Content-Range': 'bytes 0-0/1000000',
      'Content-Type': 'video/mp4',
    });
    response.flushHeaders();
  });
  const client = new OrchestratorClient({ baseUrl: server.baseUrl });

  assert.equal(
    await client.artifactUrl('/api/jobs/job-1/final'),
    `${server.baseUrl}/api/jobs/job-1/final`,
  );
  assert.equal(rangeHeader, 'bytes=0-0');
  await waitForProbeCancellation(probeClosed);
});

test('authenticates an automatically discovered FragForge port before sending its token', async (t) => {
  const token = 'discovery-proof-secret';
  const receivedTokens: Array<string | undefined> = [];
  const server = await startServer(t, (request, response) => {
    const header = request.headers['x-fragforge-token'];
    receivedTokens.push(Array.isArray(header) ? header.join(',') : header);
    if (answerVerifiedHealth(request, response, token)) return;
    sendJson(response, { jobs: [] });
  });
  const directory = temporaryDirectory(t);
  const portsFile = path.join(directory, 'ports.json');
  fs.writeFileSync(portsFile, JSON.stringify({ orchestrator: server.port }));
  const client = new OrchestratorClient({ mutationToken: token, portsFile });

  assert.deepEqual(await client.request({ path: '/api/jobs' }), { jobs: [] });
  assert.deepEqual(receivedTokens, [undefined, token]);
});

test('authenticates automatic discovery with the per-boot secret without sending it as a header', async (t) => {
  const discoverySecret = 'b'.repeat(64);
  const receivedTokens: Array<string | undefined> = [];
  const server = await startServer(t, (request, response) => {
    const header = request.headers['x-fragforge-token'];
    receivedTokens.push(Array.isArray(header) ? header.join(',') : header);
    if (answerVerifiedHealth(request, response, discoverySecret)) return;
    sendJson(response, { jobs: [] });
  });
  const directory = temporaryDirectory(t);
  const portsFile = path.join(directory, 'ports.json');
  fs.writeFileSync(portsFile, JSON.stringify({
    discovery_secret: discoverySecret,
    orchestrator: server.port,
  }));
  const client = new OrchestratorClient({ portsFile });

  assert.deepEqual(await client.request({ path: '/api/jobs' }), { jobs: [] });
  assert.deepEqual(receivedTokens, [undefined, undefined]);
});

test('uses the per-boot discovery secret for proof while keeping the mutation token for API auth', async (t) => {
  const discoverySecret = 'd'.repeat(64);
  const mutationToken = 'separate-mutation-token';
  const receivedTokens: Array<string | undefined> = [];
  const server = await startServer(t, (request, response) => {
    const header = request.headers['x-fragforge-token'];
    receivedTokens.push(Array.isArray(header) ? header.join(',') : header);
    if (answerVerifiedHealth(request, response, discoverySecret)) return;
    sendJson(response, { jobs: [] });
  });
  const directory = temporaryDirectory(t);
  const portsFile = path.join(directory, 'ports.json');
  fs.writeFileSync(portsFile, JSON.stringify({ discovery_secret: discoverySecret, orchestrator: server.port }));
  const client = new OrchestratorClient({ mutationToken, portsFile });

  assert.deepEqual(await client.request({ path: '/api/jobs' }), { jobs: [] });
  assert.deepEqual(receivedTokens, [undefined, mutationToken]);
});

test('refuses tokenless automatic discovery when ports.json has no discovery secret', async (t) => {
  let requests = 0;
  const server = await startServer(t, (_request, response) => {
    requests += 1;
    sendJson(response, { service: 'fragforge', status: 'ok' });
  });
  const directory = temporaryDirectory(t);
  const portsFile = path.join(directory, 'ports.json');
  fs.writeFileSync(portsFile, JSON.stringify({ orchestrator: server.port }));
  const client = new OrchestratorClient({ portsFile });

  await assert.rejects(client.request({ path: '/api/jobs' }), /automatic FragForge discovery requires discovery_secret/);
  assert.equal(requests, 0);
});

test('reverifies when ports.json changes before sending a token to the new origin', async (t) => {
  const token = 'port-change-secret';
  const requests: string[] = [];
  const first = await startServer(t, (request, response) => {
    requests.push(`first:${request.url?.startsWith('/healthz') === true ? 'health' : 'api'}`);
    if (answerVerifiedHealth(request, response, token)) return;
    sendJson(response, { origin: 'first' });
  });
  const second = await startServer(t, (request, response) => {
    requests.push(`second:${request.url?.startsWith('/healthz') === true ? 'health' : 'api'}`);
    if (answerVerifiedHealth(request, response, token)) return;
    sendJson(response, { origin: 'second' });
  });
  const directory = temporaryDirectory(t);
  const portsFile = path.join(directory, 'ports.json');
  fs.writeFileSync(portsFile, JSON.stringify({ orchestrator: first.port }));
  const client = new OrchestratorClient({ mutationToken: token, portsFile });

  assert.deepEqual(await client.request({ path: '/api/jobs' }), { origin: 'first' });
  fs.writeFileSync(portsFile, JSON.stringify({ orchestrator: second.port }));
  assert.deepEqual(await client.request({ path: '/api/jobs' }), { origin: 'second' });
  assert.deepEqual(requests, ['first:health', 'first:api', 'second:health', 'second:api']);
});

test('reverifies the same discovered origin before every request', async (t) => {
  const token = 'same-port-secret';
  let identifiesAsFragForge = true;
  let apiRequests = 0;
  const server = await startServer(t, (request, response) => {
    if (request.url?.startsWith('/healthz') === true) {
      if (identifiesAsFragForge) answerVerifiedHealth(request, response, token);
      else sendJson(response, { service: 'replacement-listener', status: 'ok' });
      return;
    }
    apiRequests += 1;
    identifiesAsFragForge = false;
    sendJson(response, { ok: true });
  });
  const directory = temporaryDirectory(t);
  const portsFile = path.join(directory, 'ports.json');
  fs.writeFileSync(portsFile, JSON.stringify({ orchestrator: server.port }));
  const client = new OrchestratorClient({ mutationToken: token, portsFile });

  assert.deepEqual(await client.request({ path: '/first' }), { ok: true });
  await assert.rejects(client.request({ path: '/second' }), /health check did not identify FragForge/);
  assert.equal(apiRequests, 1);
});

test('verifies artifact capabilities on the same discovered origin used by the returned URL', async (t) => {
  const token = 'artifact-origin-proof';
  const directory = temporaryDirectory(t);
  const portsFile = path.join(directory, 'ports.json');
  const firstRequests: string[] = [];
  const secondRequests: string[] = [];
  let secondPort = 0;
  const first = await startServer(t, (request, response) => {
    if (request.url?.startsWith('/healthz') === true) {
      firstRequests.push('health');
      answerVerifiedHealth(request, response, token);
      fs.writeFileSync(portsFile, JSON.stringify({ orchestrator: secondPort }));
      return;
    }
    firstRequests.push(request.url ?? 'unknown');
    sendJson(response, { auth: { read_requires_token: false } });
  });
  const second = await startServer(t, (request, response) => {
    secondRequests.push(request.url ?? 'unknown');
    if (answerVerifiedHealth(request, response, token)) return;
    sendJson(response, { auth: { read_requires_token: false } });
  });
  secondPort = second.port;
  fs.writeFileSync(portsFile, JSON.stringify({ orchestrator: first.port }));
  const client = new OrchestratorClient({ mutationToken: token, portsFile });

  const url = await client.artifactUrl('/api/jobs/job-1/final');

  assert.equal(url, `${first.baseUrl}/api/jobs/job-1/final`);
  assert.deepEqual(firstRequests, ['health', '/api/capabilities', '/api/jobs/job-1/final']);
  assert.deepEqual(secondRequests, []);
});

test('cancels discovered-origin verification with the caller signal', async (t) => {
  const discoverySecret = 'c'.repeat(64);
  let markHealthStarted: (() => void) | undefined;
  const healthStarted = new Promise<void>((resolve) => {
    markHealthStarted = resolve;
  });
  let apiRequests = 0;
  const server = await startServer(t, (request, response) => {
    if (request.url?.startsWith('/healthz') === true) {
      markHealthStarted?.();
      return;
    }
    apiRequests += 1;
    sendJson(response, { ok: true });
  });
  const directory = temporaryDirectory(t);
  const portsFile = path.join(directory, 'ports.json');
  fs.writeFileSync(portsFile, JSON.stringify({ discovery_secret: discoverySecret, orchestrator: server.port }));
  const controller = new AbortController();
  const client = new OrchestratorClient({ portsFile, requestTimeoutMs: 10_000 });

  const request = client.request({ path: '/api/jobs', signal: controller.signal });
  await healthStarted;
  controller.abort();
  await assert.rejects(request, /operation was cancelled/);
  assert.equal(apiRequests, 0);
});

test('refuses an unidentified discovered listener before sending token or request data', async (t) => {
  const receivedTokens: Array<string | undefined> = [];
  let apiRequests = 0;
  const server = await startServer(t, (request, response) => {
    const header = request.headers['x-fragforge-token'];
    receivedTokens.push(Array.isArray(header) ? header.join(',') : header);
    if (request.url?.startsWith('/healthz') === true) {
      sendJson(response, { service: 'not-fragforge', status: 'ok' });
      return;
    }
    apiRequests += 1;
    sendJson(response, { accepted: true });
  });
  const directory = temporaryDirectory(t);
  const portsFile = path.join(directory, 'ports.json');
  fs.writeFileSync(portsFile, JSON.stringify({ orchestrator: server.port }));
  const client = new OrchestratorClient({ mutationToken: 'must-not-leak', portsFile });

  await assert.rejects(client.request({ body: { private: 'payload' }, method: 'POST', path: '/api/jobs' }), /refusing discovered orchestrator/);
  assert.deepEqual(receivedTokens, [undefined]);
  assert.equal(apiRequests, 0);
});

test('bounds the unauthenticated health response before parsing it', async (t) => {
  const discoverySecret = 'e'.repeat(64);
  let apiRequests = 0;
  const server = await startServer(t, (request, response) => {
    if (request.url?.startsWith('/healthz') === true) {
      response.end(Buffer.alloc((64 << 10) + 1, 0x78));
      return;
    }
    apiRequests += 1;
    sendJson(response, { accepted: true });
  });
  const directory = temporaryDirectory(t);
  const portsFile = path.join(directory, 'ports.json');
  fs.writeFileSync(portsFile, JSON.stringify({ discovery_secret: discoverySecret, orchestrator: server.port }));
  const client = new OrchestratorClient({ portsFile });

  await assert.rejects(client.request({ path: '/api/jobs' }), /64 KiB limit/);
  assert.equal(apiRequests, 0);
});

test('rejects bare artifact URLs when orchestrator reads require authentication', async (t) => {
  const server = await startServer(t, (request, response) => {
    if (request.url === '/api/capabilities') {
      sendJson(response, { auth: { read_requires_token: true } });
      return;
    }
    sendJson(response, { error: 'not found' }, 404);
  });
  const client = new OrchestratorClient({ baseUrl: server.baseUrl, mutationToken: 'read-secret' });

  await assert.rejects(
    client.artifactUrl('/api/jobs/job-1/final'),
    /artifact URLs are unavailable while orchestrator read authentication is enabled/,
  );
});

test('maps missing-token read authentication to the artifact URL mode error', async (t) => {
  const server = await startServer(t, (_request, response) => {
    sendJson(response, { error: 'mutation token required' }, 401);
  });
  const client = new OrchestratorClient({ baseUrl: server.baseUrl });

  await assert.rejects(
    client.artifactUrl('/api/jobs/job-1/final'),
    /artifact URLs are unavailable while orchestrator read authentication is enabled/,
  );
});

test('maps JSON HTTP failures, empty successes, and invalid response values', async (t) => {
  const server = await startServer(t, (request, response) => {
    if (request.url === '/empty') {
      response.statusCode = 204;
      response.end();
      return;
    }
    if (request.url === '/failure') {
      sendJson(response, { error: 'capture is not configured' }, 409);
      return;
    }
    if (request.url === '/non-json-value') {
      response.end('undefined');
      return;
    }
    sendJson(response, { ok: true });
  });
  const client = new OrchestratorClient({ baseUrl: server.baseUrl });

  assert.equal(await client.request({ path: '/empty' }), null);
  await assert.rejects(client.request({ path: '/failure' }), (error: unknown) => {
    assert.ok(error instanceof OrchestratorHttpError);
    assert.equal(error.status, 409);
    assert.equal(error.message, 'FragForge orchestrator returned HTTP 409: capture is not configured');
    return true;
  });
  await assert.rejects(
    client.request({ path: '/non-json-value' }),
    /Studio is offline or unreachable.*Unexpected token|Studio is offline or unreachable.*not valid JSON/,
  );
});

test('times out stalled requests and maps refused connections to an offline error', async (t) => {
  const server = await startServer(t, (_request, _response) => {
    // Intentionally leave the response open until the client aborts it.
  });
  const timedClient = new OrchestratorClient({
    baseUrl: server.baseUrl,
    requestTimeoutMs: 20,
  });

  await assert.rejects(
    timedClient.request({ path: '/slow' }),
    /timed out after 20ms/,
  );

  const port = await unusedLoopbackPort();
  const offlineClient = new OrchestratorClient({
    baseUrl: `http://127.0.0.1:${port}`,
    mutationToken: 'never-include-this-token',
    requestTimeoutMs: 200,
  });
  const error = await rejectedValue(offlineClient.request({ path: '/healthz' }));
  assert.match(String(error), /Studio is offline or unreachable/);
  assert.equal(String(error).includes('never-include-this-token'), false);
});

test('cancels an in-flight orchestrator request from the caller signal', async (t) => {
  const server = await startServer(t, (_request, _response) => {
    // Keep the response open until cancellation propagates to fetch.
  });
  const controller = new AbortController();
  const client = new OrchestratorClient({ baseUrl: server.baseUrl, requestTimeoutMs: 10_000 });

  const request = client.request({ path: '/slow', signal: controller.signal });
  controller.abort();

  await assert.rejects(request, /operation was cancelled/);
});

test('sends the mutation token as a header without exposing it in mapped HTTP errors', async (t) => {
  const headers: Array<string | undefined> = [];
  const token = 'local mutation token with spaces';
  const server = await startServer(t, (request, response) => {
    const header = request.headers['x-fragforge-token'];
    headers.push(Array.isArray(header) ? header.join(',') : header);
    sendJson(response, { error: `unauthorized echo=${token}` }, 401);
  });
  const client = new OrchestratorClient({ baseUrl: server.baseUrl, mutationToken: token });

  const error = await rejectedValue(client.request({ path: '/api/jobs' }));

  assert.deepEqual(headers, [token]);
  assert.equal(String(error).includes(token), false);
  assert.match(String(error), /HTTP 401: unauthorized echo=\[redacted\]/);
});

test('redacts a reflected mutation token from successful JSON responses', async (t) => {
  const token = 'success-reflection-secret';
  const server = await startServer(t, (_request, response) => {
    sendJson(response, { nested: { message: `reflected ${token}` }, token });
  });
  const client = new OrchestratorClient({ baseUrl: server.baseUrl, mutationToken: token });

  const response = await client.request({ path: '/api/jobs' });

  assert.equal(JSON.stringify(response).includes(token), false);
  assert.match(JSON.stringify(response), /\[redacted\]/);
});

test('reads bounded text responses and redacts the mutation token', async (t) => {
  const token = 'metrics-reflection-secret';
  const receivedTokens: Array<string | undefined> = [];
  const server = await startServer(t, (request, response) => {
    const header = request.headers['x-fragforge-token'];
    receivedTokens.push(Array.isArray(header) ? header.join(',') : header);
    response.setHeader('Content-Type', 'text/plain; version=0.0.4');
    response.end(`fragforge_stage_runs_total{detail="${token}"} 1\n`);
  });
  const client = new OrchestratorClient({ baseUrl: server.baseUrl, mutationToken: token });

  const response = await client.requestText('/metrics');

  assert.deepEqual(receivedTokens, [token]);
  assert.equal(response.includes(token), false);
  assert.match(response, /detail="\[redacted\]"/);
});

test('rejects oversized JSON and text responses without buffering them unbounded', async (t) => {
  const server = await startServer(t, (request, response) => {
    if (request.url === '/metrics') {
      response.setHeader('Content-Type', 'text/plain');
      response.end(Buffer.alloc((2 << 20) + 1, 0x78));
      return;
    }
    response.setHeader('Content-Type', 'application/json');
    response.end(Buffer.concat([
      Buffer.from('{"value":"'),
      Buffer.alloc((10 << 20) + 1, 0x78),
      Buffer.from('"}'),
    ]));
  });
  const client = new OrchestratorClient({ baseUrl: server.baseUrl });

  await assert.rejects(client.requestText('/metrics'), /response exceeded the 2 MiB limit/);
  await assert.rejects(client.request({ path: '/large-json' }), /response exceeded the 10 MiB limit/);
});

test('does not follow redirects or forward the mutation token to another origin', async (t) => {
  const receivedTokens: Array<string | undefined> = [];
  const target = await startServer(t, (request, response) => {
    const header = request.headers['x-fragforge-token'];
    receivedTokens.push(Array.isArray(header) ? header.join(',') : header);
    sendJson(response, { should_not_be_reached: true });
  });
  const redirect = await startServer(t, (_request, response) => {
    response.statusCode = 302;
    response.setHeader('Location', `${target.baseUrl}/token-sink`);
    response.end();
  });
  const token = 'redirect-secret';
  const client = new OrchestratorClient({
    baseUrl: redirect.baseUrl,
    mutationToken: token,
  });

  const error = await rejectedValue(client.request({ path: '/redirect' }));

  assert.deepEqual(receivedTokens, []);
  assert.equal(String(error).includes(token), false);
  assert.match(String(error), /Studio is offline or unreachable/);
});

test('uploads demo and stream video files as multipart with config and auth', async (t) => {
  const uploads: Array<{
    body: string;
    contentType: string | undefined;
    method: string | undefined;
    token: string | undefined;
    url: string | undefined;
  }> = [];
  const server = await startServer(t, async (request, response) => {
    const contentType = request.headers['content-type'];
    const token = request.headers['x-fragforge-token'];
    uploads.push({
      body: (await readBody(request)).toString('utf8'),
      contentType: Array.isArray(contentType) ? contentType.join(',') : contentType,
      method: request.method,
      token: Array.isArray(token) ? token.join(',') : token,
      url: request.url,
    });
    sendJson(response, { accepted: true });
  });
  const directory = temporaryDirectory(t);
  const demo = path.join(directory, 'Match.DEM');
  const video = path.join(directory, 'clip.webm');
  fs.writeFileSync(demo, Buffer.from([0x44, 0x45, 0x4d, 0x00]));
  fs.writeFileSync(video, Buffer.from([0x1a, 0x45, 0xdf, 0xa3]));
  const client = new OrchestratorClient({
    baseUrl: server.baseUrl,
    mutationToken: 'upload-token',
  });

  assert.deepEqual(
    await client.uploadDemo(demo, { rules: { best: true }, target_steamid: '76561198000000000' }),
    { accepted: true },
  );
  assert.deepEqual(
    await client.uploadStreamVideo(video, { title: 'Local clip' }),
    { accepted: true },
  );

  assert.equal(uploads.length, 2);
  assert.equal(uploads[0]?.method, 'POST');
  assert.equal(uploads[0]?.url, '/api/jobs');
  assert.equal(uploads[1]?.url, '/api/stream-jobs');
  for (const upload of uploads) {
    assert.match(upload.contentType ?? '', /^multipart\/form-data; boundary=/);
    assert.equal(upload.token, 'upload-token');
    assert.match(upload.body, /name="config"/);
  }
  assert.match(uploads[0]?.body ?? '', /name="demo"; filename="Match\.DEM"/);
  assert.match(uploads[0]?.body ?? '', /"target_steamid":"76561198000000000"/);
  assert.match(uploads[1]?.body ?? '', /name="video"; filename="clip\.webm"/);
  assert.match(uploads[1]?.body ?? '', /"title":"Local clip"/);
});

test('rejects unsafe upload paths, unsupported types, directories, and empty files before HTTP', async (t) => {
  const directory = temporaryDirectory(t);
  const emptyDemo = path.join(directory, 'empty.dem');
  const wrongType = path.join(directory, 'match.txt');
  const directoryDemo = path.join(directory, 'folder.dem');
  fs.writeFileSync(emptyDemo, '');
  fs.writeFileSync(wrongType, 'demo');
  fs.mkdirSync(directoryDemo);
  const client = new OrchestratorClient({ baseUrl: 'http://127.0.0.1:1' });

  await assert.rejects(client.uploadDemo('relative.dem', {}), /demo_path must be absolute/);
  await assert.rejects(client.uploadDemo(wrongType, {}), /demo_path must use one of: \.dem/);
  await assert.rejects(client.uploadDemo(emptyDemo, {}), /demo_path must not be empty/);
  await assert.rejects(client.uploadDemo(directoryDemo, {}), /demo_path must point to a file/);
  await assert.rejects(
    client.uploadStreamVideo(path.join(directory, 'clip.txt'), {}),
    /stream video_path must use one of: .*\.webm/,
  );
});

test('rejects uploads above the orchestrator limits before opening an HTTP request', async (t) => {
  const directory = temporaryDirectory(t);
  const oversizedDemo = path.join(directory, 'oversized.dem');
  const oversizedStream = path.join(directory, 'oversized.mp4');
  fs.writeFileSync(oversizedDemo, Buffer.from([0]));
  fs.writeFileSync(oversizedStream, Buffer.from([0]));
  fs.truncateSync(oversizedDemo, (500 << 20) + 1);
  fs.truncateSync(oversizedStream, (8 * 2 ** 30) + 1);
  const client = new OrchestratorClient({ baseUrl: 'http://127.0.0.1:1' });

  await assert.rejects(client.uploadDemo(oversizedDemo, {}), /exceeds the 500 MiB limit/);
  await assert.rejects(client.uploadStreamVideo(oversizedStream, {}), /exceeds the 8 GiB limit/);
});
