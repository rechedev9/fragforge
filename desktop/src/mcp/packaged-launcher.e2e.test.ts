import assert from 'node:assert/strict';
import { spawn, spawnSync, type ChildProcess } from 'node:child_process';
import { existsSync } from 'node:fs';
import { dirname, join } from 'node:path';
import test, { type TestContext } from 'node:test';
import { fileURLToPath } from 'node:url';
import { operationCatalogResource } from './discovery.ts';
import { JsonRpcConnection } from './json-rpc.ts';
import { isJsonObject } from './json.ts';

const desktopDirectory = join(dirname(fileURLToPath(import.meta.url)), '..', '..');
const unpackedDirectory = join(desktopDirectory, 'dist-installer', 'win-unpacked');
const launcherPath = join(unpackedDirectory, 'fragforge-mcp.cmd');
const appArchivePath = join(unpackedDirectory, 'resources', 'app.asar');
const packagedExecutablePath = join(unpackedDirectory, 'FragForge Studio.exe');
const requirePackagedMCP = process.env.FRAGFORGE_REQUIRE_PACKAGED_MCP === '1';
const REQUEST_TIMEOUT_MS = 3_000;
const NATURAL_EXIT_GRACE_MS = 250;
const FORCED_EXIT_TIMEOUT_MS = 2_000;

test('the packaged desktop contains its sandboxed settings preload', { timeout: 5_000 }, (t) => {
  if (process.platform !== 'win32') {
    unavailable(t, 'the packaged FragForge application is a Windows-only artifact');
    return;
  }
  if (!existsSync(packagedExecutablePath) || !existsSync(appArchivePath)) {
    unavailable(t, 'the unpacked FragForge application is missing');
    return;
  }

  const preloadPath = join(appArchivePath, 'dist', 'preload.js');
  const script = `process.stdout.write(String(require('node:fs').existsSync(${JSON.stringify(preloadPath)})))`;
  const probe = spawnSync(packagedExecutablePath, ['-e', script], {
    encoding: 'utf8',
    env: { ...process.env, ELECTRON_RUN_AS_NODE: '1' },
    timeout: 3_000,
    windowsHide: true,
  });

  assert.equal(probe.status, 0, probe.stderr);
  assert.equal(probe.stdout, 'true');
});

test('the real unpacked installer launches its MCP entry from app.asar', { timeout: 10_000 }, async (t) => {
  if (process.platform !== 'win32') {
    unavailable(t, 'the packaged FragForge launcher is a Windows-only artifact');
    return;
  }
  if (!existsSync(launcherPath)) {
    unavailable(t, `packaged MCP launcher is missing: ${launcherPath}`);
    return;
  }
  if (!existsSync(appArchivePath)) {
    unavailable(t, `packaged Electron archive is missing: ${appArchivePath}`);
    return;
  }

  const child = spawn('cmd.exe', ['/d', '/s', '/c', launcherPath], {
    env: {
      ...process.env,
      FRAGFORGE_MUTATION_TOKEN: '',
    },
    stdio: ['pipe', 'pipe', 'pipe'],
    windowsHide: true,
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
  t.after(() => cleanupPackagedProcess(client, child));

  const initialized = await client.sendRequest('initialize', {
    capabilities: {},
    clientInfo: { name: 'packaged-launcher-e2e', version: '1' },
    protocolVersion: '2025-11-25',
  }, AbortSignal.timeout(REQUEST_TIMEOUT_MS));
  assert.ok(isJsonObject(initialized));
  assert.equal(initialized.protocolVersion, '2025-11-25');
  await client.sendNotification('notifications/initialized');

  const tools = await client.sendRequest('tools/list', undefined, AbortSignal.timeout(REQUEST_TIMEOUT_MS));
  assert.ok(isJsonObject(tools));
  assert.equal(Array.isArray(tools.tools) ? tools.tools.length : 0, 2);

  const resource = await client.sendRequest(
    'resources/read',
    { uri: 'fragforge://catalog' },
    AbortSignal.timeout(REQUEST_TIMEOUT_MS),
  );
  assert.ok(isJsonObject(resource));
  assert.ok(Array.isArray(resource.contents));
  const content = resource.contents[0];
  assert.ok(isJsonObject(content));
  const catalogText = content.text;
  if (typeof catalogText !== 'string') throw new Error('packaged catalog resource has no text');
  const catalog: unknown = JSON.parse(catalogText);
  assert.ok(isJsonObject(catalog));
  assert.ok(Array.isArray(catalog.operations));
  const sourceCatalog = operationCatalogResource();
  assert.ok(Array.isArray(sourceCatalog.operations));
  const names = catalog.operations
    .filter(isJsonObject)
    .map((operation) => operation.name)
    .filter((name): name is string => typeof name === 'string');
  const sourceNames = sourceCatalog.operations
    .filter(isJsonObject)
    .map((operation) => operation.name)
    .filter((name): name is string => typeof name === 'string');
  assert.deepEqual(names, sourceNames, 'packaged operation names differ from the current source catalog');
  assert.deepEqual(
    catalog.operations,
    sourceCatalog.operations,
    'packaged operation descriptors differ from the current source catalog',
  );

  child.stdin.end();
  await waitForCleanExit(child);
  assert.deepEqual(protocolErrors, []);
  assert.equal(Buffer.concat(stderr).toString('utf8'), '');
});

function unavailable(t: TestContext, message: string): void {
  if (requirePackagedMCP) assert.fail(`${message}; run npm run dist to build and verify it`);
  t.skip(message);
}

async function waitForCleanExit(child: ChildProcess): Promise<void> {
  const exited = await waitForProcessExit(child, 3_000);
  assert.equal(exited, true, 'packaged MCP process did not exit after stdin closed');
  assert.equal(child.signalCode, null);
  assert.equal(child.exitCode, 0);
}

async function cleanupPackagedProcess(client: JsonRpcConnection, child: ChildProcess): Promise<void> {
  client.close();
  const stdin = child.stdin;
  if (stdin !== null && !stdin.destroyed && !stdin.writableEnded) stdin.end();
  if (await waitForProcessExit(child, NATURAL_EXIT_GRACE_MS)) return;

  terminateProcessTree(child);
  const exited = await waitForProcessExit(child, FORCED_EXIT_TIMEOUT_MS);
  assert.equal(exited, true, 'packaged MCP process tree did not terminate during cleanup');
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
