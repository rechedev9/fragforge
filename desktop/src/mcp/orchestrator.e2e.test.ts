import assert from 'node:assert/strict';
import { spawn, type ChildProcess } from 'node:child_process';
import { existsSync } from 'node:fs';
import { mkdtemp, rm, writeFile } from 'node:fs/promises';
import { createServer } from 'node:net';
import * as os from 'node:os';
import * as path from 'node:path';
import test from 'node:test';
import { JsonRpcConnection } from './json-rpc.ts';
import { isJsonObject, type JsonObject } from './json.ts';

const MUTATION_TOKEN = 'real-e2e-local-token';
const REQUIRE_REAL_ORCHESTRATOR_ENV = 'FRAGFORGE_REQUIRE_REAL_ORCHESTRATOR';

test('real stdio MCP drives the real in-memory orchestrator without media tools', { timeout: 30_000 }, async (t) => {
  const repositoryRoot = path.resolve(process.cwd(), '..');
  const orchestratorExe = path.join(repositoryRoot, 'bin', 'zv-orchestrator.exe');
  if (!existsSync(orchestratorExe)) {
    if (process.env[REQUIRE_REAL_ORCHESTRATOR_ENV] === '1') {
      assert.fail(`required real orchestrator is missing: ${orchestratorExe}`);
    }
    t.skip('build bin/zv-orchestrator.exe before the real orchestrator E2E');
    return;
  }

  const temporaryDirectory = await mkdtemp(path.join(os.tmpdir(), 'fragforge-mcp-real-e2e-'));
  t.after(() => rm(temporaryDirectory, { force: true, recursive: true }));
  const port = await unusedPort();
  const baseUrl = `http://127.0.0.1:${port}`;
  const orchestrator = spawn(orchestratorExe, [], {
    env: minimalOrchestratorEnvironment(temporaryDirectory, port),
    stdio: ['ignore', 'ignore', 'pipe'],
    windowsHide: true,
  });
  const orchestratorErrors: Buffer[] = [];
  orchestrator.stderr.on('data', (chunk: Buffer) => orchestratorErrors.push(chunk));
  t.after(() => {
    if (orchestrator.exitCode === null) orchestrator.kill();
  });
  await waitForHealth(baseUrl, orchestratorErrors);

  const portsFile = path.join(temporaryDirectory, 'ports.json');
  await writeFile(portsFile, JSON.stringify({ orchestrator: port }));
  const mcp = spawn(
    process.execPath,
    ['--no-warnings', '--experimental-strip-types', path.join(process.cwd(), 'src', 'mcp', 'stdio.ts')],
    {
      env: {
        ...minimalNodeEnvironment(),
        FRAGFORGE_MUTATION_TOKEN: MUTATION_TOKEN,
        FRAGFORGE_PORTS_FILE: portsFile,
      },
      stdio: ['pipe', 'pipe', 'pipe'],
      windowsHide: true,
    },
  );
  const mcpErrors: Buffer[] = [];
  const protocolErrors: Error[] = [];
  mcp.stderr.on('data', (chunk: Buffer) => mcpErrors.push(chunk));
  t.after(() => {
    if (mcp.exitCode === null) mcp.kill();
  });
  const client = new JsonRpcConnection({
    errorHandler: (error) => protocolErrors.push(error),
    input: mcp.stdout,
    output: mcp.stdin,
  });
  client.start();
  t.after(() => client.close());

  await client.sendRequest('initialize', {
    capabilities: {},
    clientInfo: { name: 'real-orchestrator-e2e', version: '1' },
    protocolVersion: '2025-11-25',
  });
  await client.sendNotification('notifications/initialized');

  const status = await callTool(client, 'execute', { operation: 'studio.status' });
  assert.equal(structured(status).status, 'completed');
  assert.match(JSON.stringify(status), /"record"/);
  assert.match(JSON.stringify(status), /"xai_enabled":true/, 'MCP must expose Grok subtitle readiness without exposing the API key');
  assert.equal(JSON.stringify(status).includes('mcp-e2e-placeholder-not-a-secret'), false);

  const streamVariants = await callTool(client, 'execute', { operation: 'catalog.stream_variants' });
  assert.equal(structured(streamVariants).status, 'completed');
  assert.match(JSON.stringify(streamVariants), /streamer-vertical-stack-40-60/);

  const metrics = await callTool(client, 'execute', { operation: 'studio.metrics' });
  assert.equal(structured(metrics).status, 'completed');
  assert.equal(operationResult(metrics).format, 'prometheus');
  assert.equal(typeof operationResult(metrics).text, 'string');

  const subtitleSearch = await callTool(client, 'search', {
    include_dynamic_inputs: false,
    query: 'subtitulos con grok',
  });
  assert.ok(Number(structured(subtitleSearch).count) > 0);
  assert.match(JSON.stringify(subtitleSearch), /streams\.configure_captions/);

  const discovered = await callTool(client, 'search', { operation: 'jobs.list' });
  assert.equal(structured(discovered).count, 1);
  assert.match(JSON.stringify(discovered), /"dynamic_inputs":\[\]/);

  const demoPath = path.join(temporaryDirectory, 'minimal.dem');
  await writeFile(demoPath, Buffer.concat([Buffer.from('PBDEMS2\0'), Buffer.alloc(32)]));
  const beforePreview = await listJobs(client);
  assert.deepEqual(beforePreview, []);
  const preview = await callTool(client, 'execute', {
    arguments: { demo_path: demoPath },
    operation: 'jobs.create',
  });
  assert.equal(structured(preview).status, 'preview');
  assert.deepEqual(await listJobs(client), [], 'preview must not create a real job');

  const applied = await callTool(client, 'execute', {
    arguments: { demo_path: demoPath },
    confirmed: true,
    mode: 'apply',
    operation: 'jobs.create',
  });
  assert.equal(structured(applied).status, 'completed');
  const jobs = await listJobs(client);
  assert.equal(jobs.length, 1);

  const streamPath = path.join(temporaryDirectory, 'minimal.mp4');
  await writeFile(streamPath, Buffer.from([0, 0, 0, 0]));
  const streamCreated = await callTool(client, 'execute', {
    arguments: { title: 'MCP real E2E', video_path: streamPath },
    confirmed: true,
    mode: 'apply',
    operation: 'streams.create_from_file',
  });
  assert.equal(structured(streamCreated).status, 'completed', JSON.stringify(streamCreated));
  assert.match(JSON.stringify(streamCreated), /"status":"ready"/);
  const createdStreamResult = operationResult(streamCreated);
  const createdStreamJob = createdStreamResult.job;
  if (!isJsonObject(createdStreamJob) || typeof createdStreamJob.id !== 'string') {
    throw new Error('streams.create_from_file did not return a stream job ID');
  }
  const streamJobID = createdStreamJob.id;

  const editPlanBefore = await callTool(client, 'execute', {
    arguments: { stream_job_id: streamJobID },
    operation: 'streams.get_edit_plan',
  });
  const captionsPreview = await callTool(client, 'execute', {
    arguments: { enabled: true, language: 'es', stream_job_id: streamJobID },
    operation: 'streams.configure_captions',
  });
  assert.equal(structured(captionsPreview).status, 'preview');
  const editPlanAfterPreview = await callTool(client, 'execute', {
    arguments: { stream_job_id: streamJobID },
    operation: 'streams.get_edit_plan',
  });
  assert.deepEqual(operationResult(editPlanAfterPreview), operationResult(editPlanBefore), 'subtitle preview must not mutate the edit plan');

  const captionsApplied = await callTool(client, 'execute', {
    arguments: { enabled: true, language: 'es', stream_job_id: streamJobID },
    confirmed: true,
    mode: 'apply',
    operation: 'streams.configure_captions',
  });
  assert.equal(structured(captionsApplied).status, 'completed', JSON.stringify(captionsApplied));
  const editPlanAfterApply = await callTool(client, 'execute', {
    arguments: { stream_job_id: streamJobID },
    operation: 'streams.get_edit_plan',
  });
  const appliedCaptions = operationResult(editPlanAfterApply).captions;
  assert.deepEqual(appliedCaptions, { enabled: true, language: 'es' });

  const sourceArtifact = await callTool(client, 'execute', {
    arguments: { kind: 'source', stream_job_id: streamJobID },
    operation: 'artifacts.get_stream_url',
  });
  const sourceURL = operationResult(sourceArtifact).url;
  assert.equal(sourceURL, `${baseUrl}/api/stream-jobs/${streamJobID}/source`);
  if (typeof sourceURL !== 'string') throw new Error('source artifact did not return a URL');
  const sourceResponse = await fetch(sourceURL);
  assert.equal(sourceResponse.status, 200);
  assert.deepEqual(Buffer.from(await sourceResponse.arrayBuffer()), Buffer.from([0, 0, 0, 0]));

  const streams = await callTool(client, 'execute', { arguments: { limit: 10 }, operation: 'streams.list' });
  assert.match(JSON.stringify(streams), /"status":"ready"/);
  mcp.stdin.end();
  await waitForCleanExit(mcp);
  assert.deepEqual(protocolErrors, []);
  assert.equal(Buffer.concat(mcpErrors).toString('utf8'), '');
});

async function listJobs(client: JsonRpcConnection): Promise<JsonObject[]> {
  const result = await callTool(client, 'execute', { arguments: { limit: 10 }, operation: 'jobs.list' });
  const content = structured(result);
  const operationResult = content.result;
  if (!isJsonObject(operationResult) || !Array.isArray(operationResult.jobs)) throw new Error('jobs.list returned an invalid result');
  return operationResult.jobs.filter(isJsonObject);
}

async function callTool(client: JsonRpcConnection, name: string, args: JsonObject): Promise<JsonObject> {
  const result = await client.sendRequest('tools/call', { arguments: args, name });
  if (!isJsonObject(result)) throw new Error(`${name} returned an invalid MCP result`);
  return result;
}

function structured(result: JsonObject): JsonObject {
  const content = result.structuredContent;
  if (!isJsonObject(content)) throw new Error('tool result has no structuredContent');
  return content;
}

function operationResult(result: JsonObject): JsonObject {
  const content = structured(result);
  const value = content.result;
  if (!isJsonObject(value)) {
    const detail = typeof content.error === 'string' ? `: ${content.error}` : '';
    throw new Error(`tool result has no object result${detail}`);
  }
  return value;
}

async function unusedPort(): Promise<number> {
  const server = createServer();
  await new Promise<void>((resolve, reject) => {
    server.once('error', reject);
    server.listen(0, '127.0.0.1', resolve);
  });
  const address = server.address();
  if (address === null || typeof address === 'string') throw new Error('port probe did not bind TCP');
  await new Promise<void>((resolve) => server.close(() => resolve()));
  return address.port;
}

async function waitForHealth(baseUrl: string, errors: Buffer[]): Promise<void> {
  const deadline = Date.now() + 10_000;
  while (Date.now() < deadline) {
    try {
      const response = await fetch(`${baseUrl}/healthz`);
      if (response.ok) return;
    } catch {
      // The orchestrator is still starting.
    }
    await new Promise((resolve) => setTimeout(resolve, 50));
  }
  throw new Error(`real orchestrator did not become healthy: ${Buffer.concat(errors).toString('utf8')}`);
}

function minimalOrchestratorEnvironment(dataDirectory: string, port: number): NodeJS.ProcessEnv {
  return {
    Path: '',
    SystemRoot: process.env.SystemRoot,
    TEMP: process.env.TEMP,
    TMP: process.env.TMP,
    WINDIR: process.env.WINDIR,
    ZV_DATABASE_URL: 'memory',
    ZV_DATA_DIR: dataDirectory,
    ZV_HTTP_ADDR: `127.0.0.1:${port}`,
    ZV_MUTATION_TOKEN: MUTATION_TOKEN,
    XAI_API_KEY: 'mcp-e2e-placeholder-not-a-secret',
  };
}

function minimalNodeEnvironment(): NodeJS.ProcessEnv {
  return {
    APPDATA: process.env.APPDATA,
    Path: process.env.Path,
    SystemRoot: process.env.SystemRoot,
    TEMP: process.env.TEMP,
    TMP: process.env.TMP,
    WINDIR: process.env.WINDIR,
  };
}

async function waitForCleanExit(child: ChildProcess): Promise<void> {
  if (child.exitCode === null && child.signalCode === null) {
    await new Promise<void>((resolve, reject) => {
      const timeout = setTimeout(() => {
        child.off('close', handleClose);
        reject(new Error('MCP process did not exit after stdin closed'));
      }, 3_000);
      const handleClose = (): void => {
        clearTimeout(timeout);
        resolve();
      };
      child.once('close', handleClose);
    });
  }
  assert.equal(child.signalCode, null);
  assert.equal(child.exitCode, 0);
}
