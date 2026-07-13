import { Buffer } from 'node:buffer';
import { spawn, type ChildProcess, type ChildProcessWithoutNullStreams } from 'node:child_process';
import { randomBytes } from 'node:crypto';
import { existsSync } from 'node:fs';
import { mkdir, mkdtemp, rm, writeFile } from 'node:fs/promises';
import { createServer as createHttpServer, type ServerResponse } from 'node:http';
import { createServer } from 'node:net';
import * as os from 'node:os';
import * as path from 'node:path';
import {
  isConcreteArtifactUnavailableError,
  requireEval,
  requireEvalString,
  renderEvalMarkdown,
  runEvalScenarios,
  type EvalScenario,
  type McpEvalReport,
} from './eval-core.ts';
import { operationCatalogResource } from './discovery.ts';
import { JsonRpcConnection, type JsonRpcRequest } from './json-rpc.ts';
import { isJsonObject, type JsonObject } from './json.ts';

const PROTOCOL_VERSION = '2025-11-25';
const INVALID_UUID = '00000000-0000-0000-0000-000000000000';
const MCP_TIMEOUT_MS = 8_000;
const PROCESS_EXIT_TIMEOUT_MS = 3_000;

const evalEntry = process.argv[1];
if (evalEntry === undefined || evalEntry === '') throw new Error('MCP eval entry path is unavailable');
const mcpDirectory = path.dirname(path.resolve(evalEntry));
const desktopRoot = path.resolve(mcpDirectory, '..', '..');
const repositoryRoot = path.resolve(desktopRoot, '..');
const orchestratorExecutable = path.join(repositoryRoot, 'bin', 'zv-orchestrator.exe');
const mcpEntry = path.join(mcpDirectory, 'stdio.ts');
const reportDirectory = path.join(repositoryRoot, 'data', 'mcp-evals');

interface McpSession {
  child: ChildProcessWithoutNullStreams;
  connection: JsonRpcConnection;
  elicitedFields: string[];
  protocolErrors: Error[];
  stderr: Buffer[];
}

interface EvalContext {
  baseUrl: string;
  createdJobID?: string;
  createdStreamJobID?: string;
  demoPath: string;
  mcp: McpSession;
  orchestrator: ChildProcess;
  orchestratorStderr: Buffer[];
  port: number;
  portsFile: string;
  secret: string;
  streamPath: string;
  temporaryDirectory: string;
}

interface StartMcpOptions {
  elicitationAnswers?: JsonObject;
  orchestratorUrl?: string;
}

async function main(): Promise<void> {
  const environment = reportEnvironment();
  let context: EvalContext | undefined;
  let cleanupError: unknown;
  let report: McpEvalReport;
  try {
    context = await startEvalContext();
    report = await runEvalScenarios(evalScenarios(context), environment);
  } catch (error: unknown) {
    report = await runEvalScenarios([bootstrapFailureScenario(error)], environment);
  } finally {
    if (context !== undefined) {
      try {
        await cleanupEvalContext(context);
      } catch (error: unknown) {
        cleanupError = error;
      }
    }
  }
  if (cleanupError !== undefined) report = reportWithCleanupFailure(report, cleanupError);

  const paths = await writeReport(report);
  process.stdout.write(`FragForge MCP eval: ${report.summary.score}/100 (${report.summary.passed}/${report.summary.total})\n`);
  process.stdout.write(`JSON: ${paths.json}\nMarkdown: ${paths.markdown}\n`);
  if (report.summary.failed > 0) process.exitCode = 1;
}

function reportEnvironment(): JsonObject {
  return {
    database: 'memory',
    discovery_authentication: 'hmac-sha256',
    external_tools: 'inaccessible sentinel paths; no costly media operation is executed',
    mcp_entry: path.relative(repositoryRoot, mcpEntry).replace(/\\/g, '/'),
    node: process.version,
    orchestrator_binary: path.relative(repositoryRoot, orchestratorExecutable).replace(/\\/g, '/'),
    orchestrator_binary_present: existsSync(orchestratorExecutable),
    platform: `${process.platform}-${process.arch}`,
    scenario_isolation: 'fresh temporary directory and process pair per CLI run',
  };
}

function bootstrapFailureScenario(error: unknown): EvalScenario {
  return {
    id: 'bootstrap.real-processes',
    run: async (signal) => {
      signal.throwIfAborted();
      throw error;
    },
    title: 'Start isolated real orchestrator and MCP processes',
  };
}

function reportWithCleanupFailure(report: McpEvalReport, error: unknown): McpEvalReport {
  const scenarios = [
    ...report.scenarios,
    {
      duration_ms: 0,
      error: errorMessage(error),
      id: 'process.fallback-cleanup',
      status: 'failed' as const,
      title: 'Clean up evaluator processes and temporary files after scoring',
    },
  ];
  const passed = scenarios.filter((scenario) => scenario.status === 'passed').length;
  const total = scenarios.length;
  const finished = new Date();
  return {
    ...report,
    duration_ms: Math.max(report.duration_ms, finished.getTime() - Date.parse(report.started_at)),
    finished_at: finished.toISOString(),
    scenarios,
    summary: {
      failed: total - passed,
      passed,
      score: Math.round((passed / total) * 100),
      total,
    },
  };
}

async function startEvalContext(): Promise<EvalContext> {
  if (!existsSync(orchestratorExecutable)) {
    throw new Error(`real orchestrator is missing: ${orchestratorExecutable}; build it before running the MCP eval gate`);
  }
  if (!existsSync(mcpEntry)) throw new Error(`MCP stdio entry is missing: ${mcpEntry}`);

  const temporaryDirectory = await mkdtemp(path.join(os.tmpdir(), 'fragforge-mcp-eval-'));
  const port = await unusedPort();
  const baseUrl = `http://127.0.0.1:${port}`;
  const secret = randomBytes(32).toString('hex');
  const forbiddenToolsDirectory = path.join(temporaryDirectory, 'forbidden-tools');
  const orchestratorStderr: Buffer[] = [];
  let orchestrator: ChildProcess | undefined;
  let mcp: McpSession | undefined;
  try {
    orchestrator = spawn(orchestratorExecutable, [], {
      env: minimalOrchestratorEnvironment(temporaryDirectory, forbiddenToolsDirectory, port, secret),
      stdio: ['ignore', 'ignore', 'pipe'],
      windowsHide: true,
    });
    orchestrator.stderr?.on('data', (chunk: Buffer) => orchestratorStderr.push(chunk));
    await waitForHealth(baseUrl, orchestrator, orchestratorStderr);

    const portsFile = path.join(temporaryDirectory, 'ports.json');
    await writeFile(portsFile, JSON.stringify({ discovery_secret: secret, orchestrator: port }), 'utf8');
    const demoPath = path.join(temporaryDirectory, 'minimal.dem');
    const streamPath = path.join(temporaryDirectory, 'minimal.mp4');
    await Promise.all([
      writeFile(demoPath, Buffer.concat([Buffer.from('PBDEMS2\0'), Buffer.alloc(32)])),
      writeFile(streamPath, Buffer.from([0, 0, 0, 0])),
    ]);
    mcp = startMcp(portsFile);
    return {
      baseUrl,
      demoPath,
      mcp,
      orchestrator,
      orchestratorStderr,
      port,
      portsFile,
      secret,
      streamPath,
      temporaryDirectory,
    };
  } catch (error: unknown) {
    const cleanupErrors: string[] = [];
    if (mcp !== undefined) {
      try {
        await stopMcp(mcp);
      } catch (cleanupError: unknown) {
        cleanupErrors.push(`MCP: ${errorMessage(cleanupError)}`);
      }
    }
    if (orchestrator !== undefined && processIsRunning(orchestrator)) {
      try {
        if (!orchestrator.kill()) cleanupErrors.push('orchestrator rejected the termination signal');
        await waitForExit(orchestrator, PROCESS_EXIT_TIMEOUT_MS);
      } catch (cleanupError: unknown) {
        cleanupErrors.push(`orchestrator: ${errorMessage(cleanupError)}`);
      }
    }
    try {
      await rm(temporaryDirectory, { force: true, recursive: true });
    } catch (cleanupError: unknown) {
      cleanupErrors.push(`temporary directory: ${errorMessage(cleanupError)}`);
    }
    if (cleanupErrors.length > 0) {
      throw new Error(`${errorMessage(error)}; bootstrap cleanup failures: ${cleanupErrors.join('; ')}`);
    }
    throw error;
  }
}

function evalScenarios(context: EvalContext): EvalScenario[] {
  return [
    lifecycleScenario(context),
    catalogScenario(context),
    studioScenario(context),
    spanishSearchScenario(context),
    exactFilterScenario(context),
    writeSafetyScenario(context),
    dynamicDiscoveryScenario(context),
    streamHappyPathScenario(context),
    streamCancellationRecoveryScenario(context),
    streamDiscoverySchemaScenario(context),
    strictSchemaScenario(context),
    conditionalElicitationScenario(context),
    artifactAvailabilityScenario(context),
    staleHmacScenario(context),
    oversizedFrameScenario(context),
    processTeardownScenario(context),
  ];
}

function lifecycleScenario(context: EvalContext): EvalScenario {
  return {
    id: 'protocol.lifecycle',
    run: async (signal) => {
      const initialized = jsonObject(await sendMcpRequest(context.mcp, 'initialize', {
        capabilities: {},
        clientInfo: { name: 'fragforge-mcp-eval', version: '1' },
        protocolVersion: PROTOCOL_VERSION,
      }, signal), 'initialize result');
      requireEval(initialized.protocolVersion === PROTOCOL_VERSION, 'server did not negotiate the current MCP protocol');
      signal.throwIfAborted();
      await context.mcp.connection.sendNotification('notifications/initialized');
      signal.throwIfAborted();
      const ping = jsonObject(await sendMcpRequest(context.mcp, 'ping', undefined, signal), 'ping result');
      requireEval(Object.keys(ping).length === 0, 'ping must return an empty object');
      const listed = jsonObject(await sendMcpRequest(context.mcp, 'tools/list', undefined, signal), 'tools/list result');
      const tools = jsonObjectArray(listed.tools, 'tools/list tools');
      const names = tools.map((tool) => requireEvalString(tool.name, 'tool name'));
      requireEval(names.length === 2, `expected two progressive-disclosure tools, got ${names.length}`);
      requireEval(names.includes('search') && names.includes('execute'), 'search and execute tools must both be advertised');
      return { protocol: PROTOCOL_VERSION, tools: names };
    },
    title: 'Negotiate MCP lifecycle and expose only search/execute',
  };
}

function catalogScenario(context: EvalContext): EvalScenario {
  return {
    id: 'catalog.surface',
    run: async (signal) => {
      const resources = jsonObject(await sendMcpRequest(context.mcp, 'resources/list', undefined, signal), 'resources/list result');
      requireEval(jsonObjectArray(resources.resources, 'resources').length === 2, 'expected catalog and status resources');
      const read = jsonObject(await sendMcpRequest(context.mcp, 'resources/read', {
        uri: 'fragforge://catalog',
      }, signal), 'catalog resource');
      const contents = jsonObjectArray(read.contents, 'catalog contents');
      const first = contents[0];
      requireEval(first !== undefined, 'catalog resource returned no content');
      const text = requireEvalString(first.text, 'catalog text');
      const catalog = jsonObject(JSON.parse(text) as unknown, 'parsed catalog');
      const operations = jsonObjectArray(catalog.operations, 'catalog operations');
      const sourceCatalog = operationCatalogResource();
      const sourceOperations = jsonObjectArray(sourceCatalog.operations, 'source catalog operations');
      const names = operations.map((operation) => requireEvalString(operation.name, 'operation name'));
      const sourceNames = sourceOperations.map((operation) => requireEvalString(operation.name, 'source operation name'));
      requireEval(
        JSON.stringify(operations) === JSON.stringify(sourceOperations),
        'served operation descriptors differ from the current source catalog',
      );
      requireEval(
        JSON.stringify(names) === JSON.stringify(sourceNames),
        'served operation names differ from the current source catalog',
      );
      return { operation_count: operations.length };
    },
    title: 'Expose the complete current allowlisted operation catalog',
  };
}

function studioScenario(context: EvalContext): EvalScenario {
  return {
    id: 'orchestrator.hmac-and-status',
    run: async (signal) => {
      const status = await callToolSuccess(context.mcp, 'execute', { operation: 'studio.status' }, signal);
      requireEval(status.status === 'completed', 'studio.status did not complete through authenticated discovery');
      const serialized = JSON.stringify(status);
      requireEval(!serialized.includes(context.secret), 'discovery secret leaked through studio.status');
      requireEval(!serialized.includes(context.temporaryDirectory), 'local capability paths leaked through studio.status');
      const metrics = await callToolSuccess(context.mcp, 'execute', { operation: 'studio.metrics' }, signal);
      const result = operationResult(metrics);
      requireEval(result.format === 'prometheus', 'studio.metrics did not identify Prometheus text');
      requireEval(typeof result.text === 'string', 'studio.metrics returned no text');
      return { metrics_format: result.format, status: status.status };
    },
    title: 'Authenticate discovered Studio and read status plus metrics',
  };
}

function spanishSearchScenario(context: EvalContext): EvalScenario {
  const intents: ReadonlyArray<readonly [string, string]> = [
    ['estado del estudio', 'studio.status'],
    ['subir una demo', 'jobs.create'],
    ['ver trabajos recientes', 'jobs.list'],
    ['grabar kills', 'jobs.record'],
    ['descargar video', 'artifacts.get_url'],
    ['lista de canciones', 'catalog.songs'],
    ['subtitulos con grok', 'streams.configure_captions'],
  ];
  return {
    id: 'search.spanish-intents',
    run: async (signal) => {
      const matches = await Promise.all(intents.map(async ([query, expected]) => {
        const result = await callToolSuccess(context.mcp, 'search', {
          include_dynamic_inputs: false,
          limit: 5,
          query,
        }, signal);
        return { expected, query, top: firstOperationName(result) };
      }));
      for (const match of matches) {
        requireEval(match.top === match.expected, `Spanish intent "${match.query}" ranked ${match.top ?? 'nothing'} above ${match.expected}`);
      }
      const evidence: JsonObject = {};
      for (const match of matches) evidence[match.query] = match.top ?? '';
      return evidence;
    },
    title: 'Rank common Spanish Studio intents correctly at top one',
  };
}

function exactFilterScenario(context: EvalContext): EvalScenario {
  return {
    id: 'search.exact-filter-consistency',
    run: async (signal) => {
      const [wrongRisk, wrongCategory, matching] = await Promise.all([
        callToolSuccess(context.mcp, 'search', {
          include_dynamic_inputs: false,
          operation: 'jobs.create',
          risk: 'read',
        }, signal),
        callToolSuccess(context.mcp, 'search', {
          category: 'studio',
          include_dynamic_inputs: false,
          operation: 'jobs.create',
        }, signal),
        callToolSuccess(context.mcp, 'search', {
          category: 'jobs',
          include_dynamic_inputs: false,
          operation: 'jobs.create',
          risk: 'write',
        }, signal),
      ]);
      requireEval(wrongRisk.count === 0, 'an exact operation ignored a contradictory risk filter');
      requireEval(wrongCategory.count === 0, 'an exact operation ignored a contradictory category filter');
      requireEval(matching.count === 1, 'a matching exact operation was filtered out');
      return { matching: matching.count, wrong_category: wrongCategory.count, wrong_risk: wrongRisk.count };
    },
    title: 'Honor category and risk filters for exact operation lookup',
  };
}

function writeSafetyScenario(context: EvalContext): EvalScenario {
  return {
    id: 'execute.preview-confirmation',
    run: async (signal) => {
      requireEval((await listJobs(context.mcp, signal)).length === 0, 'isolated evaluator did not start with an empty job store');
      const preview = await callToolSuccess(context.mcp, 'execute', {
        arguments: { demo_path: context.demoPath },
        operation: 'jobs.create',
      }, signal);
      requireEval(preview.status === 'preview', 'write operation did not default to preview');
      requireEval((await listJobs(context.mcp, signal)).length === 0, 'preview created a real demo job');

      const confirmationError = await callToolError(context.mcp, 'execute', {
        arguments: { demo_path: context.demoPath },
        mode: 'apply',
        operation: 'jobs.create',
      }, signal);
      requireEval(/confirmed=true|approval/i.test(confirmationError), 'apply without confirmation returned an unrelated error');
      requireEval((await listJobs(context.mcp, signal)).length === 0, 'unconfirmed apply created a real demo job');

      const applied = await callToolSuccess(context.mcp, 'execute', {
        arguments: { demo_path: context.demoPath },
        confirmed: true,
        mode: 'apply',
        operation: 'jobs.create',
      }, signal);
      requireEval(applied.status === 'completed', 'confirmed demo upload did not complete');
      context.createdJobID = requireEvalString(operationResult(applied).id, 'created demo job ID');
      const jobs = await listJobs(context.mcp, signal);
      requireEval(jobs.some((job) => job.id === context.createdJobID), 'created demo job is absent from jobs.list');
      return { created_job_id: context.createdJobID, preview_status: preview.status };
    },
    title: 'Keep writes side-effect-free until apply plus confirmation',
  };
}

function dynamicDiscoveryScenario(context: EvalContext): EvalScenario {
  return {
    id: 'search.dynamic-partial-survival',
    run: async (signal) => {
      const jobID = requireEvalString(context.createdJobID, 'created demo job ID');
      const exact = await callToolSuccess(context.mcp, 'search', { operation: 'jobs.get' }, signal);
      const exactDescriptor = firstDescriptor(exact);
      const jobField = dynamicInputs(exactDescriptor).find((field) => field.field === 'job_id');
      requireEval(jobField !== undefined, 'jobs.get did not expose live job_id candidates');
      requireEval(candidateValues(jobField).includes(jobID), 'live job candidates did not include the created job');

      const partial = await callToolSuccess(context.mcp, 'search', {
        arguments: { job_id: INVALID_UUID },
        operation: 'jobs.generate',
      }, signal);
      const fields = dynamicInputs(firstDescriptor(partial)).map((field) => field.field).filter((field): field is string => typeof field === 'string');
      requireEval(fields.includes('job_id'), 'one failing dependent source erased valid job candidates');
      requireEval(fields.includes('preset'), 'one failing dependent source erased valid preset candidates');
      return { candidate_job_id: jobID, partial_fields: fields };
    },
    title: 'Preserve successful live inputs when one dependent source fails',
  };
}

function streamHappyPathScenario(context: EvalContext): EvalScenario {
  return {
    id: 'streams.local-file-happy-path',
    run: async (signal) => {
      const preview = await callToolSuccess(context.mcp, 'execute', {
        arguments: { title: 'MCP deterministic eval', video_path: context.streamPath },
        operation: 'streams.create_from_file',
      }, signal);
      requireEval(preview.status === 'preview', 'stream upload did not default to preview');
      const created = await callToolSuccess(context.mcp, 'execute', {
        arguments: { title: 'MCP deterministic eval', video_path: context.streamPath },
        confirmed: true,
        mode: 'apply',
        operation: 'streams.create_from_file',
      }, signal);
      requireEval(created.status === 'completed', `stream creation did not complete: ${JSON.stringify(created)}`);
      const job = operationResult(created).job;
      requireEval(isJsonObject(job), 'stream creation returned no job object');
      context.createdStreamJobID = requireEvalString(job.id, 'created stream job ID');
      requireEval(job.status === 'ready', `created stream job is ${String(job.status)} instead of ready`);

      const before = await callToolSuccess(context.mcp, 'execute', {
        arguments: { stream_job_id: context.createdStreamJobID },
        operation: 'streams.get_edit_plan',
      }, signal);
      const captionsPreview = await callToolSuccess(context.mcp, 'execute', {
        arguments: { enabled: true, language: 'es', stream_job_id: context.createdStreamJobID },
        operation: 'streams.configure_captions',
      }, signal);
      requireEval(captionsPreview.status === 'preview', 'caption configuration preview did not stay a preview');
      const afterPreview = await callToolSuccess(context.mcp, 'execute', {
        arguments: { stream_job_id: context.createdStreamJobID },
        operation: 'streams.get_edit_plan',
      }, signal);
      requireEval(JSON.stringify(operationResult(before)) === JSON.stringify(operationResult(afterPreview)), 'caption preview mutated the saved edit plan');

      const captionsApplied = await callToolSuccess(context.mcp, 'execute', {
        arguments: { enabled: true, language: 'es', stream_job_id: context.createdStreamJobID },
        confirmed: true,
        mode: 'apply',
        operation: 'streams.configure_captions',
      }, signal);
      requireEval(captionsApplied.status === 'completed', 'caption configuration did not apply');

      const source = await callToolSuccess(context.mcp, 'execute', {
        arguments: { kind: 'source', stream_job_id: context.createdStreamJobID },
        operation: 'artifacts.get_stream_url',
      }, signal);
      const sourceURL = requireEvalString(operationResult(source).url, 'stream source URL');
      const response = await fetch(sourceURL, { signal: boundedSignal(signal, MCP_TIMEOUT_MS) });
      requireEval(response.status === 200, `stream source URL returned HTTP ${response.status}`);
      const bytes = Buffer.from(await response.arrayBuffer());
      requireEval(bytes.equals(Buffer.from([0, 0, 0, 0])), 'stream source bytes changed in transit');
      return { source_status: response.status, stream_job_id: context.createdStreamJobID };
    },
    title: 'Upload, initialize, edit, and fetch a local stream source',
  };
}

function streamCancellationRecoveryScenario(context: EvalContext): EvalScenario {
  return {
    id: 'streams.post-upload-cancellation-recovery',
    run: async (signal) => {
      const maxProxyBodyBytes = 1_000_000;
      let durableStreamJobID: string | undefined;
      let uploadCount = 0;
      let recoveryEnabled = false;
      let markEditPlanRequestStarted: (() => void) | undefined;
      const editPlanRequestStarted = new Promise<void>((resolve) => {
        markEditPlanRequestStarted = resolve;
      });
      let markEditPlanRequestClosed: (() => void) | undefined;
      const editPlanRequestClosed = new Promise<void>((resolve) => {
        markEditPlanRequestClosed = resolve;
      });
      const blockedResponses = new Set<ServerResponse>();
      const httpServer = createHttpServer((request, response) => {
        const proxyRequest = async (): Promise<void> => {
          const requestUrl = request.url ?? '/';
          const pathname = new URL(requestUrl, 'http://127.0.0.1').pathname;
          const editPlanPath = durableStreamJobID === undefined
            ? undefined
            : `/api/stream-jobs/${durableStreamJobID}/edit-plan`;
          if (request.method === 'GET' && pathname === editPlanPath && !recoveryEnabled) {
            blockedResponses.add(response);
            response.once('close', () => {
              blockedResponses.delete(response);
              markEditPlanRequestClosed?.();
            });
            markEditPlanRequestStarted?.();
            return;
          }

          const method = request.method ?? 'GET';
          let body: Buffer | undefined;
          if (method !== 'GET' && method !== 'HEAD') {
            const chunks: Buffer[] = [];
            let totalBytes = 0;
            for await (const chunk of request) {
              const bytes = Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk);
              totalBytes += bytes.length;
              requireEval(totalBytes <= maxProxyBodyBytes, 'cancellation proxy request body exceeded its evaluation bound');
              chunks.push(bytes);
            }
            body = Buffer.concat(chunks);
          }
          const headers: Record<string, string> = {};
          const contentType = request.headers['content-type'];
          if (typeof contentType === 'string') headers['content-type'] = contentType;
          const upstream = await fetch(`${context.baseUrl}${requestUrl}`, {
            body,
            headers,
            method,
            redirect: 'manual',
            signal: boundedSignal(signal, MCP_TIMEOUT_MS),
          });
          const responseBytes = Buffer.from(await upstream.arrayBuffer());
          if (method === 'POST' && pathname === '/api/stream-jobs') {
            uploadCount += 1;
            requireEval(upstream.ok, `real orchestrator rejected cancellation-eval upload with HTTP ${upstream.status}`);
            const parsed: unknown = JSON.parse(responseBytes.toString('utf8'));
            requireEval(isJsonObject(parsed), 'real orchestrator upload response was not an object');
            durableStreamJobID = requireEvalString(parsed.id, 'real durable stream job ID');
          }
          response.statusCode = upstream.status;
          const upstreamContentType = upstream.headers.get('content-type');
          if (upstreamContentType !== null) response.setHeader('Content-Type', upstreamContentType);
          response.end(responseBytes);
        };
        void proxyRequest().catch((error: unknown) => {
          if (response.headersSent) {
            response.destroy(error instanceof Error ? error : new Error(String(error)));
            return;
          }
          response.statusCode = 502;
          response.setHeader('Content-Type', 'application/json');
          response.end(JSON.stringify({ error: errorMessage(error) }));
        });
      });

      let session: McpSession | undefined;
      try {
        const listen = new Promise<void>((resolve, reject) => {
          httpServer.once('error', reject);
          httpServer.listen(0, '127.0.0.1', resolve);
        });
        await withTimeout(listen, MCP_TIMEOUT_MS, 'cancellation evaluator server did not bind TCP', signal);
        const address = httpServer.address();
        requireEval(address !== null && typeof address !== 'string', 'cancellation evaluator server did not bind TCP');
        session = startMcp(context.portsFile, {
          orchestratorUrl: `http://127.0.0.1:${address.port}`,
        });
        await initializeSession(session, false, signal);
        const controller = new AbortController();
        const createOutcome = session.connection.sendRequest('tools/call', {
          arguments: {
            arguments: { title: 'Cancellation eval', video_path: context.streamPath },
            confirmed: true,
            mode: 'apply',
            operation: 'streams.create_from_file',
          },
          name: 'execute',
        }, AbortSignal.any([signal, controller.signal, AbortSignal.timeout(MCP_TIMEOUT_MS)])).then(
          () => 'unexpected success',
          (error: unknown) => errorMessage(error),
        );
        await withTimeout(editPlanRequestStarted, MCP_TIMEOUT_MS, 'stream edit-plan request never started', signal);
        controller.abort();
        const cancellationError = await createOutcome;
        requireEval(/cancel/i.test(cancellationError), `tool request did not cancel cleanly: ${cancellationError}`);
        await withTimeout(editPlanRequestClosed, MCP_TIMEOUT_MS, 'cancellation did not abort the pending orchestrator request', signal);
        requireEval(uploadCount === 1, `cancellation created ${uploadCount} uploads before recovery`);

        recoveryEnabled = true;
        const expectedStreamJobID = requireEvalString(durableStreamJobID, 'durable cancellation-eval stream job ID');
        const listed = await callToolSuccess(session, 'execute', {
          arguments: { limit: 50 },
          operation: 'streams.list',
        }, signal);
        const jobs = jsonObjectArray(operationResult(listed).jobs, 'streams.list jobs');
        const rediscovered = jobs.find((job) => job.id === expectedStreamJobID);
        requireEval(rediscovered !== undefined, 'the real durable job could not be rediscovered after cancellation');
        await callToolSuccess(session, 'execute', {
          arguments: { stream_job_id: expectedStreamJobID },
          confirmed: true,
          mode: 'apply',
          operation: 'streams.resume_initialization',
        }, signal);
        const ready = await callToolSuccess(session, 'execute', {
          arguments: { stream_job_id: expectedStreamJobID },
          operation: 'streams.get',
        }, signal);
        requireEval(operationResult(ready).status === 'ready', 'same-ID recovery did not reach ready state');
        requireEval(uploadCount === 1, `same-ID recovery created ${uploadCount} uploads instead of exactly one`);
        return {
          cancellation: cancellationError,
          orchestrator: 'real Go state behind deterministic fault proxy',
          rediscovered: true,
          recovery_status: 'ready',
          stream_job_id: expectedStreamJobID,
          uploads: uploadCount,
        };
      } finally {
        recoveryEnabled = true;
        for (const response of blockedResponses) response.destroy();
        const cleanupErrors: string[] = [];
        if (session !== undefined) {
          try {
            await stopMcp(session);
          } catch (error: unknown) {
            cleanupErrors.push(`secondary MCP: ${errorMessage(error)}`);
          }
        }
        try {
          const closeProxy = new Promise<void>((resolve, reject) => {
            httpServer.close((error) => error === undefined ? resolve() : reject(error));
          });
          httpServer.closeAllConnections();
          await withTimeout(closeProxy, PROCESS_EXIT_TIMEOUT_MS, 'cancellation fault proxy did not close');
        } catch (error: unknown) {
          cleanupErrors.push(`fault proxy: ${errorMessage(error)}`);
        }
        if (cleanupErrors.length > 0) {
          throw new Error(`cancellation scenario cleanup failures: ${cleanupErrors.join('; ')}`);
        }
      }
    },
    title: 'Recover a real Go stream job after a proxied post-upload cancellation without uploading twice',
  };
}

function streamDiscoverySchemaScenario(context: EvalContext): EvalScenario {
  return {
    id: 'search.dynamic-schema-alignment',
    run: async (signal) => {
      const streamJobID = requireEvalString(context.createdStreamJobID, 'created stream job ID');
      const result = await callToolSuccess(context.mcp, 'search', {
        arguments: { stream_job_id: streamJobID },
        operation: 'streams.configure_captions',
      }, signal);
      const descriptor = firstDescriptor(result);
      const schema = jsonObject(descriptor.input_schema, 'configure captions schema');
      const properties = jsonObject(schema.properties, 'configure captions schema properties');
      const fields = dynamicInputs(descriptor)
        .map((field) => field.field)
        .filter((field): field is string => typeof field === 'string');
      for (const field of fields) {
        requireEval(Object.hasOwn(properties, field), `dynamic input ${field} is not accepted by streams.configure_captions`);
      }
      requireEval(fields.includes('stream_job_id'), 'caption discovery did not expose stream_job_id candidates');
      return { dynamic_fields: fields };
    },
    title: 'Keep dynamic inputs aligned with the operation input schema',
  };
}

function strictSchemaScenario(context: EvalContext): EvalScenario {
  return {
    id: 'execute.strict-boundary-validation',
    run: async (signal) => {
      const jobID = requireEvalString(context.createdJobID, 'created demo job ID');
      const streamJobID = requireEvalString(context.createdStreamJobID, 'created stream job ID');
      const invalidUUID = await callToolError(context.mcp, 'execute', {
        arguments: { job_id: 'x' },
        operation: 'jobs.get',
      }, signal);
      const invalidSource = await callToolError(context.mcp, 'execute', {
        arguments: { source_url: 'file:///C:/Windows/win.ini' },
        operation: 'streams.create_from_url',
      }, signal);
      const emptyRecordPreset = await callToolError(context.mcp, 'execute', {
        arguments: { job_id: jobID, preset: '' },
        operation: 'jobs.record',
      }, signal);
      const unknownGeneratePreset = await callToolError(context.mcp, 'execute', {
        arguments: { job_id: jobID, preset: 'unknown-render-preset' },
        confirmed: true,
        mode: 'apply',
        operation: 'jobs.generate',
      }, signal);
      const inheritedName = await callToolError(context.mcp, 'execute', {
        arguments: { constructor: 'schema-bypass', stream_job_id: streamJobID },
        operation: 'streams.get',
      }, signal);
      const emptyClip = await callToolError(context.mcp, 'execute', {
        arguments: {
          clip_id: '',
          kind: 'video',
          stream_job_id: streamJobID,
          variant: 'streamer-vertical-stack-40-60',
        },
        operation: 'artifacts.get_stream_url',
      }, signal);
      const unknownStreamVariant = await callToolError(context.mcp, 'execute', {
        arguments: { stream_job_id: streamJobID, variant: 'unknown-stream-variant' },
        operation: 'streams.get_render',
      }, signal);
      const missingFaceCrop = await callToolError(context.mcp, 'execute', {
        arguments: {
          plan: {
            gameplay_crop: { height: 1, width: 1, x: 0, y: 0 },
            variant: 'streamer-vertical-stack-40-60',
          },
          stream_job_id: streamJobID,
        },
        confirmed: true,
        mode: 'apply',
        operation: 'streams.update_edit_plan',
      }, signal);
      requireEval(/invalid format|uuid/i.test(invalidUUID), `short job ID reached HTTP instead of schema rejection: ${invalidUUID}`);
      requireEval(/https?|scheme|invalid format/i.test(invalidSource), `non-HTTP source URL passed schema validation: ${invalidSource}`);
      requireEval(/preset[\s\S]*invalid format/i.test(emptyRecordPreset), `empty record preset passed schema validation: ${emptyRecordPreset}`);
      requireEval(
        /unknown-render-preset[\s\S]*live render[\s\S]*viral-60-clean/i.test(unknownGeneratePreset),
        `unknown generate preset was not rejected against the live registry: ${unknownGeneratePreset}`,
      );
      requireEval(/constructor.*not allowed/i.test(inheritedName), `prototype property bypassed additionalProperties: ${inheritedName}`);
      requireEval(/clip_id|too short|invalid format/i.test(emptyClip), `empty stream clip ID passed validation: ${emptyClip}`);
      requireEval(
        /unknown-stream-variant[\s\S]*live stream[\s\S]*streamer-/i.test(unknownStreamVariant),
        `unknown stream variant was not rejected against the live registry: ${unknownStreamVariant}`,
      );
      requireEval(/face_crop/i.test(missingFaceCrop), `vertical stream plan without face_crop passed validation: ${missingFaceCrop}`);
      return {
        empty_clip: emptyClip,
        empty_record_preset: emptyRecordPreset,
        inherited_property: inheritedName,
        invalid_source: invalidSource,
        invalid_uuid: invalidUUID,
        missing_face_crop: missingFaceCrop,
        unknown_stream_variant: unknownStreamVariant,
        unknown_generate_preset: unknownGeneratePreset,
      };
    },
    title: 'Reject malformed identifiers, URLs, keys, and stream plans before unsafe target requests',
  };
}

function conditionalElicitationScenario(context: EvalContext): EvalScenario {
  return {
    id: 'execute.conditional-elicitation',
    run: async (signal) => {
      const streamJobID = requireEvalString(context.createdStreamJobID, 'created stream job ID');
      const session = startMcp(context.portsFile, {
        elicitationAnswers: {
          clip_id: 'missing-clip',
          kind: 'video',
          stream_job_id: streamJobID,
          variant: 'streamer-vertical-stack-40-60',
        },
      });
      try {
        await initializeSession(session, true, signal);
        const result = await callTool(session, 'execute', { operation: 'artifacts.get_stream_url' }, signal);
        const structured = structuredContent(result);
        for (const field of ['stream_job_id', 'kind', 'variant', 'clip_id']) {
          requireEval(session.elicitedFields.includes(field), `conditional elicitation never requested ${field}`);
        }
        requireEval(result.isError === true, `elicited nonexistent artifact unexpectedly succeeded: ${JSON.stringify(structured)}`);
        const error = requireEvalString(structured.error, 'elicited nonexistent artifact error');
        requireEval(!/required|too many missing inputs/i.test(error), `elicitation stopped before completing conditional fields: ${error}`);
        requireEval(
          isConcreteArtifactUnavailableError(error),
          `elicited nonexistent artifact failed for an unrelated reason: ${error}`,
        );
        return { elicited_fields: session.elicitedFields, error, is_error: true };
      } finally {
        await stopMcp(session);
      }
    },
    title: 'Complete conditionally required primitive inputs through MCP forms',
  };
}

function artifactAvailabilityScenario(context: EvalContext): EvalScenario {
  return {
    id: 'artifacts.availability-honesty',
    run: async (signal) => {
      const result = await callTool(context.mcp, 'execute', {
        arguments: { job_id: INVALID_UUID, kind: 'final' },
        operation: 'artifacts.get_url',
      }, signal);
      requireEval(result.isError === true, 'nonexistent artifact URL was not rejected');
      const structured = structuredContent(result);
      const error = requireEvalString(structured.error, 'nonexistent artifact error');
      requireEval(
        isConcreteArtifactUnavailableError(error),
        `nonexistent artifact failed for an unrelated reason: ${error}`,
      );
      return { error, outcome: 'concrete nonexistent-artifact rejection' };
    },
    title: 'Never claim a nonexistent artifact URL is completed and available',
  };
}

function staleHmacScenario(context: EvalContext): EvalScenario {
  return {
    id: 'discovery.stale-secret-rejected',
    run: async (signal) => {
      signal.throwIfAborted();
      const staleSecret = randomBytes(32).toString('hex');
      const stalePorts = path.join(context.temporaryDirectory, 'ports-stale.json');
      await writeFile(stalePorts, JSON.stringify({ discovery_secret: staleSecret, orchestrator: context.port }), 'utf8');
      signal.throwIfAborted();
      const session = startMcp(stalePorts);
      try {
        await initializeSession(session, false, signal);
        const message = await callToolError(session, 'execute', { operation: 'studio.status' }, signal);
        requireEval(/authenticate|refusing discovered orchestrator/i.test(message), `stale discovery secret was not rejected as an authentication failure: ${message}`);
        const serialized = JSON.stringify(await callTool(session, 'execute', { operation: 'studio.status' }, signal));
        requireEval(!serialized.includes(staleSecret) && !serialized.includes(context.secret), 'discovery secret leaked in an authentication failure');
        return { rejected: true };
      } finally {
        await stopMcp(session);
      }
    },
    title: 'Reject stale ports-file discovery secrets without leaking them',
  };
}

function oversizedFrameScenario(context: EvalContext): EvalScenario {
  return {
    id: 'protocol.oversized-frame-lifecycle',
    run: async (signal) => {
      const session = startMcp(context.portsFile);
      try {
        const oversized = sendMcpRequest(session, 'initialize', {
          capabilities: {},
          clientInfo: { name: 'x'.repeat(1_100_000), version: '1' },
          protocolVersion: PROTOCOL_VERSION,
        }, signal);
        await withTimeout(oversized.then(() => undefined, () => undefined), MCP_TIMEOUT_MS, 'oversized frame response timed out', signal);
        await waitForExit(session.child, 1_500, signal);
        requireEval(session.child.signalCode === null, `oversized-frame MCP was killed by ${String(session.child.signalCode)}`);
        requireEval(session.child.exitCode === 1, `oversized-frame MCP exited with ${String(session.child.exitCode)} instead of 1`);
        return { exit_code: session.child.exitCode };
      } finally {
        if (session.child.exitCode === null) session.child.kill();
        session.connection.close();
      }
    },
    title: 'Terminate the MCP process after a fatal oversized input frame',
  };
}

function processTeardownScenario(context: EvalContext): EvalScenario {
  return {
    id: 'process.teardown',
    run: async (signal) => {
      const teardownErrors: string[] = [];
      let exitCode: number | null = null;
      try {
        exitCode = await closeMcp(context.mcp, signal);
      } catch (error: unknown) {
        teardownErrors.push(`MCP teardown: ${errorMessage(error)}`);
        context.mcp.connection.close();
        if (processIsRunning(context.mcp.child)) context.mcp.child.kill();
        try {
          await waitForExit(context.mcp.child, PROCESS_EXIT_TIMEOUT_MS);
        } catch (waitError: unknown) {
          teardownErrors.push(`forced MCP teardown: ${errorMessage(waitError)}`);
        }
      }

      if (processIsRunning(context.orchestrator)) {
        const accepted = context.orchestrator.kill();
        if (!accepted) teardownErrors.push('orchestrator rejected the termination signal');
      }
      try {
        await waitForExit(context.orchestrator, PROCESS_EXIT_TIMEOUT_MS, signal);
      } catch (error: unknown) {
        teardownErrors.push(`orchestrator teardown: ${errorMessage(error)}`);
      }

      const mcpDead = !processIsRunning(context.mcp.child);
      const orchestratorDead = !processIsRunning(context.orchestrator);
      requireEval(mcpDead, 'MCP process remained alive after teardown');
      requireEval(orchestratorDead, 'orchestrator process remained alive after teardown');
      requireEval(teardownErrors.length === 0, teardownErrors.join('; '));
      requireEval(exitCode === 0, `MCP exited with code ${String(exitCode)}`);
      requireEval(context.mcp.protocolErrors.length === 0, `client observed protocol errors: ${context.mcp.protocolErrors.map((error) => error.message).join('; ')}`);
      const stderr = Buffer.concat(context.mcp.stderr).toString('utf8');
      requireEval(stderr === '', `MCP contaminated diagnostics during a healthy session: ${stderr}`);
      return {
        mcp_dead: mcpDead,
        mcp_exit_code: exitCode,
        mcp_stderr_bytes: 0,
        orchestrator_dead: orchestratorDead,
        orchestrator_exit_code: context.orchestrator.exitCode ?? -1,
        orchestrator_signal: context.orchestrator.signalCode ?? 'none',
      };
    },
    title: 'Stop and verify both isolated MCP and orchestrator processes',
  };
}

function startMcp(portsFile: string, options: StartMcpOptions = {}): McpSession {
  const child = spawn(
    process.execPath,
    ['--no-warnings', '--experimental-strip-types', mcpEntry],
    {
      env: {
        ...minimalWindowsEnvironment(false),
        FRAGFORGE_MCP_TIMEOUT_MS: String(MCP_TIMEOUT_MS),
        FRAGFORGE_PORTS_FILE: portsFile,
        ...(options.orchestratorUrl === undefined
          ? {}
          : { FRAGFORGE_ORCHESTRATOR_URL: options.orchestratorUrl }),
      },
      stdio: ['pipe', 'pipe', 'pipe'],
      windowsHide: true,
    },
  );
  const stderr: Buffer[] = [];
  const protocolErrors: Error[] = [];
  const elicitedFields: string[] = [];
  child.stderr.on('data', (chunk: Buffer) => stderr.push(chunk));
  const connection = new JsonRpcConnection({
    errorHandler: (error) => protocolErrors.push(error),
    input: child.stdout,
    output: child.stdin,
  });
  connection.setRequestHandler(async (request: JsonRpcRequest) => {
    if (request.method !== 'elicitation/create' || options.elicitationAnswers === undefined) {
      await connection.sendError(request.id, -32_601, 'Method not found');
      return;
    }
    const field = elicitationField(request.params);
    const value = field === undefined ? undefined : options.elicitationAnswers[field];
    if (field === undefined || value === undefined) {
      await connection.sendResult(request.id, { action: 'decline' });
      return;
    }
    elicitedFields.push(field);
    await connection.sendResult(request.id, { action: 'accept', content: { [field]: value } });
  });
  connection.start();
  return { child, connection, elicitedFields, protocolErrors, stderr };
}

async function initializeSession(session: McpSession, elicitation: boolean, signal: AbortSignal): Promise<void> {
  const capabilities = elicitation ? { elicitation: { form: {} } } : {};
  await sendMcpRequest(session, 'initialize', {
    capabilities,
    clientInfo: { name: 'fragforge-mcp-eval-secondary', version: '1' },
    protocolVersion: PROTOCOL_VERSION,
  }, signal);
  signal.throwIfAborted();
  await session.connection.sendNotification('notifications/initialized');
  signal.throwIfAborted();
}

function elicitationField(params: unknown): string | undefined {
  if (!isJsonObject(params) || !isJsonObject(params.requestedSchema)) return undefined;
  const properties = params.requestedSchema.properties;
  if (!isJsonObject(properties)) return undefined;
  return Object.keys(properties)[0];
}

async function callTool(
  session: McpSession,
  name: string,
  argumentsValue: JsonObject,
  signal: AbortSignal,
): Promise<JsonObject> {
  const result = await sendMcpRequest(session, 'tools/call', {
    arguments: argumentsValue,
    name,
  }, signal);
  return jsonObject(result, `${name} tool result`);
}

async function callToolSuccess(
  session: McpSession,
  name: string,
  argumentsValue: JsonObject,
  signal: AbortSignal,
): Promise<JsonObject> {
  const result = await callTool(session, name, argumentsValue, signal);
  const structured = structuredContent(result);
  requireEval(result.isError !== true, `${name} failed: ${typeof structured.error === 'string' ? structured.error : JSON.stringify(structured)}`);
  return structured;
}

async function callToolError(
  session: McpSession,
  name: string,
  argumentsValue: JsonObject,
  signal: AbortSignal,
): Promise<string> {
  const result = await callTool(session, name, argumentsValue, signal);
  const structured = structuredContent(result);
  requireEval(result.isError === true, `${name} unexpectedly succeeded: ${JSON.stringify(structured)}`);
  return requireEvalString(structured.error, `${name} error`);
}

function structuredContent(result: JsonObject): JsonObject {
  return jsonObject(result.structuredContent, 'tool structuredContent');
}

function operationResult(structured: JsonObject): JsonObject {
  return jsonObject(structured.result, 'operation result');
}

function firstDescriptor(search: JsonObject): JsonObject {
  const descriptor = jsonObjectArray(search.operations, 'search operations')[0];
  requireEval(descriptor !== undefined, 'search returned no operation descriptor');
  return descriptor;
}

function firstOperationName(search: JsonObject): string | undefined {
  const descriptor = jsonObjectArray(search.operations, 'search operations')[0];
  return descriptor === undefined || typeof descriptor.name !== 'string' ? undefined : descriptor.name;
}

function dynamicInputs(descriptor: JsonObject): JsonObject[] {
  return jsonObjectArray(descriptor.dynamic_inputs, 'dynamic inputs');
}

function candidateValues(field: JsonObject): string[] {
  return jsonObjectArray(field.candidates, 'dynamic candidates')
    .map((candidate) => candidate.value)
    .filter((value): value is string => typeof value === 'string');
}

async function listJobs(session: McpSession, signal: AbortSignal): Promise<JsonObject[]> {
  const structured = await callToolSuccess(session, 'execute', {
    arguments: { limit: 100 },
    operation: 'jobs.list',
  }, signal);
  return jsonObjectArray(operationResult(structured).jobs, 'jobs.list jobs');
}

function jsonObject(value: unknown, label: string): JsonObject {
  requireEval(isJsonObject(value), `${label} must be a JSON object`);
  return value;
}

function jsonObjectArray(value: unknown, label: string): JsonObject[] {
  requireEval(Array.isArray(value), `${label} must be an array`);
  const objects = value.filter(isJsonObject);
  requireEval(objects.length === value.length, `${label} contains a non-object value`);
  return objects;
}

function minimalOrchestratorEnvironment(
  dataDirectory: string,
  forbiddenToolsDirectory: string,
  port: number,
  secret: string,
): NodeJS.ProcessEnv {
  const unavailable = (name: string): string => path.join(forbiddenToolsDirectory, name);
  return {
    ...minimalWindowsEnvironment(true),
    ZV_CODEX_PATH: unavailable('codex.exe'),
    ZV_COMPOSER_PATH: unavailable('zv-composer.exe'),
    ZV_CS2_PATH: unavailable('cs2.exe'),
    ZV_DATABASE_URL: 'memory',
    ZV_DATA_DIR: path.join(dataDirectory, 'data'),
    ZV_DISCOVERY_SECRET: secret,
    ZV_EDITOR_PATH: unavailable('zv-editor.exe'),
    ZV_FFMPEG_PATH: unavailable('ffmpeg.exe'),
    ZV_HLAE_PATH: unavailable('hlae.exe'),
    ZV_HTTP_ADDR: `127.0.0.1:${port}`,
    ZV_RECORDER_PATH: unavailable('zv-recorder.exe'),
    ZV_WHISPER_MODEL: unavailable('model.bin'),
    ZV_WHISPER_PATH: unavailable('whisper.exe'),
    ZV_YTDLP_PATH: unavailable('yt-dlp.exe'),
  };
}

function minimalWindowsEnvironment(clearPath: boolean): NodeJS.ProcessEnv {
  const environment: NodeJS.ProcessEnv = { Path: clearPath ? '' : process.env.Path };
  for (const name of ['APPDATA', 'SystemRoot', 'TEMP', 'TMP', 'WINDIR']) {
    const value = process.env[name];
    if (value !== undefined) environment[name] = value;
  }
  return environment;
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

async function waitForHealth(baseUrl: string, child: ChildProcess, stderr: Buffer[]): Promise<void> {
  const deadline = Date.now() + 10_000;
  while (Date.now() < deadline) {
    if (child.exitCode !== null) {
      throw new Error(`orchestrator exited before health check with code ${child.exitCode}: ${Buffer.concat(stderr).toString('utf8')}`);
    }
    try {
      const response = await fetch(`${baseUrl}/healthz`, { signal: AbortSignal.timeout(500) });
      if (response.ok) return;
    } catch {
      // The isolated orchestrator is still starting.
    }
    await pause(50);
  }
  throw new Error(`orchestrator did not become healthy: ${Buffer.concat(stderr).toString('utf8')}`);
}

async function closeMcp(session: McpSession, signal?: AbortSignal): Promise<number | null> {
  if (session.child.exitCode === null && !session.child.stdin.destroyed) session.child.stdin.end();
  await waitForExit(session.child, PROCESS_EXIT_TIMEOUT_MS, signal);
  session.connection.close();
  return session.child.exitCode;
}

async function stopMcp(session: McpSession): Promise<void> {
  try {
    const exitCode = await closeMcp(session);
    requireEval(session.child.signalCode === null, `secondary MCP was killed by ${String(session.child.signalCode)}`);
    requireEval(exitCode === 0, `secondary MCP exited with code ${String(exitCode)}`);
    requireEval(
      session.protocolErrors.length === 0,
      `secondary MCP client observed protocol errors: ${session.protocolErrors.map((error) => error.message).join('; ')}`,
    );
    const stderr = Buffer.concat(session.stderr).toString('utf8');
    requireEval(stderr === '', `secondary MCP contaminated diagnostics during a healthy session: ${stderr}`);
  } catch (error: unknown) {
    session.connection.close();
    if (processIsRunning(session.child)) session.child.kill();
    await waitForExit(session.child, PROCESS_EXIT_TIMEOUT_MS);
    throw error;
  }
}

async function waitForExit(child: ChildProcess, timeoutMs: number, signal?: AbortSignal): Promise<void> {
  if (child.exitCode !== null || child.signalCode !== null) return;
  signal?.throwIfAborted();
  await new Promise<void>((resolve, reject) => {
    const cleanup = (): void => {
      clearTimeout(timeout);
      child.off('close', handleClose);
      signal?.removeEventListener('abort', handleAbort);
    };
    const handleAbort = (): void => {
      cleanup();
      reject(signal?.reason ?? new Error('operation aborted'));
    };
    const handleClose = (): void => {
      cleanup();
      resolve();
    };
    const timeout = setTimeout(() => {
      cleanup();
      reject(new Error(`process did not exit within ${timeoutMs}ms`));
    }, timeoutMs);
    child.once('close', handleClose);
    signal?.addEventListener('abort', handleAbort, { once: true });
  });
}

function processIsRunning(child: ChildProcess): boolean {
  return child.exitCode === null && child.signalCode === null;
}

function sendMcpRequest(
  session: McpSession,
  method: string,
  params: unknown,
  signal: AbortSignal,
): Promise<unknown> {
  return session.connection.sendRequest(method, params, boundedSignal(signal, MCP_TIMEOUT_MS));
}

async function cleanupEvalContext(context: EvalContext): Promise<void> {
  const errors: string[] = [];
  try {
    await stopMcp(context.mcp);
  } catch (error: unknown) {
    errors.push(`MCP: ${errorMessage(error)}`);
  }
  if (processIsRunning(context.orchestrator)) {
    try {
      if (!context.orchestrator.kill()) errors.push('orchestrator rejected the termination signal');
      await waitForExit(context.orchestrator, PROCESS_EXIT_TIMEOUT_MS);
    } catch (error: unknown) {
      errors.push(`orchestrator: ${errorMessage(error)}`);
    }
  }
  try {
    await rm(context.temporaryDirectory, { force: true, recursive: true });
  } catch (error: unknown) {
    errors.push(`temporary directory: ${errorMessage(error)}`);
  }
  if (errors.length > 0) throw new Error(`fallback cleanup failures: ${errors.join('; ')}`);
}

async function withTimeout<T>(
  promise: Promise<T>,
  timeoutMs: number,
  message: string,
  signal?: AbortSignal,
): Promise<T> {
  signal?.throwIfAborted();
  return new Promise<T>((resolve, reject) => {
    let settled = false;
    const cleanup = (): void => {
      clearTimeout(timeout);
      signal?.removeEventListener('abort', handleAbort);
    };
    const rejectOnce = (error: unknown): void => {
      if (settled) return;
      settled = true;
      cleanup();
      reject(error);
    };
    const resolveOnce = (value: T): void => {
      if (settled) return;
      settled = true;
      cleanup();
      resolve(value);
    };
    const handleAbort = (): void => rejectOnce(signal?.reason ?? new Error('operation aborted'));
    const timeout = setTimeout(() => rejectOnce(new Error(message)), timeoutMs);
    signal?.addEventListener('abort', handleAbort, { once: true });
    void promise.then(
      resolveOnce,
      rejectOnce,
    );
  });
}

function boundedSignal(signal: AbortSignal, timeoutMs: number): AbortSignal {
  return AbortSignal.any([signal, AbortSignal.timeout(timeoutMs)]);
}

function pause(milliseconds: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, milliseconds));
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

async function writeReport(report: McpEvalReport): Promise<{ json: string; markdown: string }> {
  await mkdir(reportDirectory, { recursive: true });
  const stem = `mcp-eval-${report.run_id}`;
  const jsonPath = path.join(reportDirectory, `${stem}.json`);
  const markdownPath = path.join(reportDirectory, `${stem}.md`);
  const json = `${JSON.stringify(report, null, 2)}\n`;
  const markdown = renderEvalMarkdown(report);
  await Promise.all([
    writeFile(jsonPath, json, 'utf8'),
    writeFile(markdownPath, markdown, 'utf8'),
    writeFile(path.join(reportDirectory, 'latest.json'), json, 'utf8'),
    writeFile(path.join(reportDirectory, 'latest.md'), markdown, 'utf8'),
  ]);
  return { json: jsonPath, markdown: markdownPath };
}

void main().catch((error: unknown) => {
  process.stderr.write(`FragForge MCP eval could not write its report: ${error instanceof Error ? error.message : String(error)}\n`);
  process.exitCode = 1;
});
