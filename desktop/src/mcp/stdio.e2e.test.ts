import assert from 'node:assert/strict';
import { spawn, spawnSync, type ChildProcess } from 'node:child_process';
import { existsSync } from 'node:fs';
import { createServer, type ServerResponse } from 'node:http';
import * as path from 'node:path';
import test from 'node:test';
import { JsonRpcConnection, JsonRpcConnectionClosedError } from './json-rpc.ts';
import { isJsonObject } from './json.ts';

const REQUEST_TIMEOUT_MS = 3_000;
const NATURAL_EXIT_GRACE_MS = 250;
const FORCED_EXIT_TIMEOUT_MS = 2_000;

test('the real TypeScript stdio entry interoperates end to end without stdout contamination', { timeout: 10_000 }, async (t) => {
  const orchestrator = createServer((request, response) => {
    if (request.url === '/api/jobs?limit=2') {
      json(response, { jobs: [{ id: 'job-from-e2e', status: 'parsed' }] });
      return;
    }
    response.statusCode = 404;
    json(response, { error: 'not found' });
  });
  await new Promise<void>((resolve) => orchestrator.listen(0, '127.0.0.1', resolve));
  t.after(() => orchestrator.close());
  const address = orchestrator.address();
  if (address === null || typeof address === 'string') throw new Error('fake orchestrator did not bind TCP');

  const entry = path.join(process.cwd(), 'src', 'mcp', 'stdio.ts');
  const child = spawn(process.execPath, ['--no-warnings', '--experimental-strip-types', entry], {
    env: {
      ...process.env,
      FRAGFORGE_MUTATION_TOKEN: '',
      FRAGFORGE_ORCHESTRATOR_URL: `http://127.0.0.1:${address.port}`,
    },
    stdio: ['pipe', 'pipe', 'pipe'],
  });
  const stderr: Buffer[] = [];
  const protocolErrors: Error[] = [];
  child.stderr.on('data', (chunk: Buffer) => stderr.push(chunk));
  const client = new JsonRpcConnection({
    errorHandler: (error) => protocolErrors.push(error),
    input: child.stdout,
    output: child.stdin,
  });
  client.start();
  t.after(() => cleanupMcpProcess(client, child));

  const initialized = await client.sendRequest('initialize', {
    capabilities: {},
    clientInfo: { name: 'stdio-e2e', version: '1' },
    protocolVersion: '2025-11-25',
  }, AbortSignal.timeout(REQUEST_TIMEOUT_MS));
  assert.ok(isJsonObject(initialized));
  assert.equal(initialized.protocolVersion, '2025-11-25');
  await client.sendNotification('notifications/initialized');

  const tools = await client.sendRequest('tools/list', undefined, AbortSignal.timeout(REQUEST_TIMEOUT_MS));
  assert.ok(isJsonObject(tools));
  assert.equal(Array.isArray(tools.tools) ? tools.tools.length : 0, 2);

  const searched = await client.sendRequest('tools/call', {
    arguments: { include_dynamic_inputs: false, query: 'list demo jobs' },
    name: 'search',
  }, AbortSignal.timeout(REQUEST_TIMEOUT_MS));
  assert.ok(isJsonObject(searched));
  assert.ok(JSON.stringify(searched).includes('jobs.list'));

  const executed = await client.sendRequest('tools/call', {
    arguments: { arguments: { limit: 2 }, operation: 'jobs.list' },
    name: 'execute',
  }, AbortSignal.timeout(REQUEST_TIMEOUT_MS));
  assert.ok(isJsonObject(executed));
  assert.ok(JSON.stringify(executed).includes('job-from-e2e'));
  child.stdin.end();
  await waitForCleanExit(child);
  assert.deepEqual(protocolErrors, []);
  assert.equal(Buffer.concat(stderr).toString('utf8'), '');
});

test('the packaged Windows launcher uses Electron Node mode with working stdio', { timeout: 10_000 }, async (t) => {
  const compiledEntry = path.join(process.cwd(), 'dist', 'mcp', 'stdio.js');
  if (!existsSync(compiledEntry)) {
    t.skip('run npm run build before the packaged-launcher E2E');
    return;
  }
  const child = spawn(
    'cmd.exe',
    ['/d', '/s', '/c', path.join(process.cwd(), 'scripts', 'fragforge-mcp.cmd')],
    {
      env: { ...process.env, FRAGFORGE_MUTATION_TOKEN: '' },
      stdio: ['pipe', 'pipe', 'pipe'],
      windowsHide: true,
    },
  );
  const stderr: Buffer[] = [];
  const protocolErrors: Error[] = [];
  child.stderr.on('data', (chunk: Buffer) => stderr.push(chunk));
  const client = new JsonRpcConnection({
    errorHandler: (error) => protocolErrors.push(error),
    input: child.stdout,
    output: child.stdin,
  });
  client.start();
  t.after(() => cleanupMcpProcess(client, child));

  const initialized = await client.sendRequest('initialize', {
    capabilities: {},
    clientInfo: { name: 'launcher-e2e', version: '1' },
    protocolVersion: '2025-11-25',
  }, AbortSignal.timeout(REQUEST_TIMEOUT_MS));
  assert.ok(isJsonObject(initialized));
  await client.sendNotification('notifications/initialized');
  const tools = await client.sendRequest('tools/list', undefined, AbortSignal.timeout(REQUEST_TIMEOUT_MS));
  assert.ok(isJsonObject(tools));
  assert.equal(Array.isArray(tools.tools) ? tools.tools.length : 0, 2);
  child.stdin.end();
  await waitForCleanExit(child);
  assert.deepEqual(protocolErrors, []);
  assert.equal(Buffer.concat(stderr).toString('utf8'), '');
});

test('closing stdin aborts an in-flight tool call and exits without termination signals', { timeout: 10_000 }, async (t) => {
  let markRequestStarted: (() => void) | undefined;
  const requestStarted = new Promise<void>((resolve) => {
    markRequestStarted = resolve;
  });
  let markResponseClosed: (() => void) | undefined;
  const responseClosed = new Promise<void>((resolve) => {
    markResponseClosed = resolve;
  });
  const orchestrator = createServer((request, response) => {
    if (request.url === '/api/jobs?limit=1') {
      markRequestStarted?.();
      response.once('close', () => markResponseClosed?.());
      return;
    }
    response.statusCode = 404;
    json(response, { error: 'not found' });
  });
  await new Promise<void>((resolve) => orchestrator.listen(0, '127.0.0.1', resolve));
  t.after(() => {
    orchestrator.closeAllConnections();
    orchestrator.close();
  });
  const address = orchestrator.address();
  if (address === null || typeof address === 'string') throw new Error('fake orchestrator did not bind TCP');

  const child = spawn(
    process.execPath,
    ['--no-warnings', '--experimental-strip-types', path.join(process.cwd(), 'src', 'mcp', 'stdio.ts')],
    {
      env: {
        ...process.env,
        FRAGFORGE_MUTATION_TOKEN: '',
        FRAGFORGE_ORCHESTRATOR_URL: `http://127.0.0.1:${address.port}`,
      },
      stdio: ['pipe', 'pipe', 'pipe'],
    },
  );
  const stderr: Buffer[] = [];
  const protocolErrors: Error[] = [];
  child.stderr.on('data', (chunk: Buffer) => stderr.push(chunk));
  const client = new JsonRpcConnection({
    errorHandler: (error) => protocolErrors.push(error),
    input: child.stdout,
    output: child.stdin,
  });
  client.start();
  t.after(() => cleanupMcpProcess(client, child));

  await client.sendRequest('initialize', {
    capabilities: {},
    clientInfo: { name: 'shutdown-e2e', version: '1' },
    protocolVersion: '2025-11-25',
  }, AbortSignal.timeout(REQUEST_TIMEOUT_MS));
  await client.sendNotification('notifications/initialized');
  const pending = client.sendRequest('tools/call', {
    arguments: { arguments: { limit: 1 }, operation: 'jobs.list' },
    name: 'execute',
  }, AbortSignal.timeout(REQUEST_TIMEOUT_MS));
  void pending.catch(() => undefined);
  await requestStarted;

  child.stdin.end();
  await waitForCleanExit(child);
  await assert.rejects(pending, JsonRpcConnectionClosedError);
  await responseClosed;
  assert.deepEqual(protocolErrors, []);
  assert.equal(Buffer.concat(stderr).toString('utf8'), '');
});

test('an oversized frame returns one protocol error and terminates the stdio process', { timeout: 10_000 }, async (t) => {
  const child = spawn(
    process.execPath,
    ['--no-warnings', '--experimental-strip-types', path.join(process.cwd(), 'src', 'mcp', 'stdio.ts')],
    {
      env: { ...process.env, FRAGFORGE_ORCHESTRATOR_URL: 'http://127.0.0.1:8080' },
      stdio: ['pipe', 'pipe', 'pipe'],
    },
  );
  t.after(() => cleanupChildProcess(child));
  const stdout: Buffer[] = [];
  const stdinErrors: Error[] = [];
  child.stdout.on('data', (chunk: Buffer) => stdout.push(chunk));
  child.stdin.on('error', (error) => stdinErrors.push(error));
  child.stdin.write(`${JSON.stringify({
    id: 1,
    jsonrpc: '2.0',
    method: 'tools/call',
    params: { padding: 'x'.repeat(2 * 1024 * 1024) },
  })}\n`);

  await waitForExit(child);

  assert.equal(child.signalCode, null);
  assert.equal(child.exitCode, 1);
  const frames = Buffer.concat(stdout).toString('utf8').trim().split('\n').filter(Boolean);
  assert.equal(frames.length, 1);
  const response: unknown = JSON.parse(frames[0] ?? 'null');
  assert.ok(isJsonObject(response));
  assert.ok(isJsonObject(response.error));
  assert.equal(response.error.code, -32_600);
  assert.ok(stdinErrors.every((error) => 'code' in error && (error.code === 'EOF' || error.code === 'EPIPE')));
});

function json(response: ServerResponse, value: unknown): void {
  response.setHeader('Content-Type', 'application/json');
  response.end(JSON.stringify(value));
}

async function waitForCleanExit(child: ChildProcess): Promise<void> {
  const exited = await waitForProcessExit(child, 3_000);
  assert.equal(exited, true, 'MCP process did not exit after stdin closed');
  assert.equal(child.signalCode, null);
  assert.equal(child.exitCode, 0);
}

async function waitForExit(child: ChildProcess): Promise<void> {
  const exited = await waitForProcessExit(child, 3_000);
  assert.equal(exited, true, 'MCP process did not exit');
}

async function cleanupMcpProcess(client: JsonRpcConnection, child: ChildProcess): Promise<void> {
  client.close();
  await cleanupChildProcess(child);
}

async function cleanupChildProcess(child: ChildProcess): Promise<void> {
  const stdin = child.stdin;
  if (stdin !== null && !stdin.destroyed && !stdin.writableEnded) stdin.end();
  if (await waitForProcessExit(child, NATURAL_EXIT_GRACE_MS)) return;

  terminateProcessTree(child);
  const exited = await waitForProcessExit(child, FORCED_EXIT_TIMEOUT_MS);
  assert.equal(exited, true, 'MCP process tree did not terminate during cleanup');
}

function terminateProcessTree(child: ChildProcess): void {
  if (child.pid === undefined || hasExited(child)) return;
  if (process.platform === 'win32') {
    const result = spawnSync('taskkill.exe', ['/pid', String(child.pid), '/t', '/f'], {
      stdio: 'ignore',
      timeout: FORCED_EXIT_TIMEOUT_MS,
      windowsHide: true,
    });
    if (result.status === 0 || hasExited(child)) return;
  }
  child.kill();
}

async function waitForProcessExit(child: ChildProcess, timeoutMs: number): Promise<boolean> {
  if (hasExited(child)) return true;
  return await new Promise<boolean>((resolve) => {
    const handleClose = (): void => {
      clearTimeout(timeout);
      resolve(true);
    };
    const timeout = setTimeout(() => {
      child.off('close', handleClose);
      resolve(hasExited(child));
    }, timeoutMs);
    child.once('close', handleClose);
    if (hasExited(child)) {
      child.off('close', handleClose);
      clearTimeout(timeout);
      resolve(true);
    }
  });
}

function hasExited(child: ChildProcess): boolean {
  return child.exitCode !== null || child.signalCode !== null;
}
