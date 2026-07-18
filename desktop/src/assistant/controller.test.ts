import assert from 'node:assert/strict';
import { mkdtemp, rm } from 'node:fs/promises';
import * as os from 'node:os';
import * as path from 'node:path';
import test from 'node:test';
import { McpOperationGateway } from '../mcp/operation-gateway.ts';
import { OrchestratorClient } from '../mcp/orchestrator-client.ts';
import {
  type AppServerAgentMessageDelta,
  type AppServerDynamicToolCall,
  type AppServerStartThreadOptions,
  type AppServerStartTurnOptions,
  type AppServerThread,
  type AppServerTurn,
  type CodexAppServer,
  type CodexAppServerClientOptions,
} from './app-server-client.ts';
import { AssistantController } from './controller.ts';
import { AssistantHistoryStore } from './history.ts';

class FakeAppServer implements CodexAppServer {
  closed = false;
  readonly status = 'ready' as const;
  startThreadOptions: AppServerStartThreadOptions | undefined;
  turnText: string | undefined;

  close(): void {
    this.closed = true;
  }

  async initialize(): Promise<void> {}

  async interruptTurn(): Promise<void> {}

  async resumeThread(threadId: string): Promise<AppServerThread> {
    return { id: threadId, sessionId: 'session-1' };
  }

  async startThread(options?: AppServerStartThreadOptions): Promise<AppServerThread> {
    this.startThreadOptions = options;
    return { id: 'thread-1', sessionId: 'session-1' };
  }

  async startTurn(_threadId: string, text: string, _options?: AppServerStartTurnOptions): Promise<AppServerTurn> {
    this.turnText = text;
    return { id: 'turn-1', status: 'inProgress' };
  }
}

async function controllerFixture(t: test.TestContext): Promise<{
  appServer: FakeAppServer;
  calls: Array<{ arguments?: unknown; privileged?: boolean }>;
  controller: AssistantController;
  options(): CodexAppServerClientOptions;
}> {
  const directory = await mkdtemp(path.join(os.tmpdir(), 'fragforge-assistant-controller-'));
  t.after(async () => removeTemporaryDirectory(directory));
  const appServer = new FakeAppServer();
  let captured: CodexAppServerClientOptions | undefined;
  const calls: Array<{ arguments?: unknown; privileged?: boolean }> = [];
  const gateway = {
    execute: async (request: { arguments?: unknown; operation?: string }, options: { privileged?: boolean } = {}) => {
      calls.push({ arguments: request.arguments, privileged: options.privileged });
      if (request.operation === 'jobs.get') {
        return {
          arguments: request.arguments ?? {},
          kind: 'executed' as const,
          operation: 'jobs.get',
          partialFailure: false,
          result: {
            error: 'C:\\Users\\reche\\secret.mp4',
            safe_status: 'parsed',
            video_url: 'http://127.0.0.1:8080/api/jobs/job-123/video',
          },
          status: 'completed' as const,
        };
      }
      if (options.privileged) {
        return {
          arguments: request.arguments ?? {},
          kind: 'executed' as const,
          operation: 'jobs.record',
          partialFailure: false,
          result: { accepted: true },
          status: 'completed' as const,
        };
      }
      return {
        arguments: request.arguments ?? {},
        kind: 'preview' as const,
        operation: 'jobs.record',
        preview: { method: 'POST', path: '/api/jobs/job-123/record' },
        requiresConfirmation: true as const,
        risk: 'costly' as const,
      };
    },
  } as unknown as McpOperationGateway;
  const controller = new AssistantController({
    createAppServer: (options) => {
      captured = options;
      return appServer;
    },
    cwd: path.join(directory, 'workspace'),
    gateway,
    history: new AssistantHistoryStore(path.join(directory, 'history.json')),
    orchestratorClient: new OrchestratorClient({ baseUrl: 'http://127.0.0.1:1' }),
  });
  return {
    appServer,
    calls,
    controller,
    options: () => {
      const options = captured;
      if (options === undefined) throw new Error('expected app-server options');
      return options;
    },
  };
}

function completeTurn(options: CodexAppServerClientOptions, delta = 'Hola desde Codex.'): void {
  options.onAgentMessageDelta?.({
    delta,
    itemId: 'item-1',
    threadId: 'thread-1',
    turnId: 'turn-1',
  } satisfies AppServerAgentMessageDelta);
  options.onTurnCompleted?.({
    threadId: 'thread-1',
    turn: { id: 'turn-1', status: 'completed' },
  });
}

function dynamicCall(tool: string, argumentsValue: AppServerDynamicToolCall['arguments']): AppServerDynamicToolCall {
  return {
    arguments: argumentsValue,
    callId: `call-${tool}`,
    namespace: 'fragforge',
    requestId: `request-${tool}`,
    threadId: 'thread-1',
    tool,
    turnId: 'turn-1',
  };
}

async function removeTemporaryDirectory(directory: string): Promise<void> {
  let lastError: unknown;
  for (let attempt = 0; attempt < 4; attempt += 1) {
    try {
      await rm(directory, { force: true, recursive: true });
      return;
    } catch (error) {
      lastError = error;
      await new Promise<void>((resolve) => setTimeout(resolve, 20));
    }
  }
  throw lastError;
}

test('starts Codex in the isolated safe profile and streams its reply', async (t) => {
  const fixture = await controllerFixture(t);

  const ready = await fixture.controller.status();
  assert.equal(ready.availability, 'ready');
  const options = fixture.options();
  assert.ok(options.args?.includes('--strict-config'));
  assert.ok(options.args?.includes('mcp_servers={}'));
  assert.ok(options.args?.includes("web_search='disabled'"));
  assert.ok(options.args?.includes('hooks'));
  assert.ok(options.args?.includes('remote_plugin'));
  assert.equal(options.cwd?.endsWith('workspace'), true);
  assert.equal(options.dynamicTools?.[0]?.type, 'namespace');

  await fixture.controller.send('Resume este demo.', {
    jobId: 'job-123',
    kind: 'demo',
    label: 'Demo actual',
    pathname: '/matches/job-123',
  });
  assert.equal(fixture.appServer.startThreadOptions?.sandbox, 'read-only');
  assert.equal(fixture.appServer.startThreadOptions?.approvalPolicy, 'never');
  assert.match(fixture.appServer.turnText ?? '', /Demo actual/);

  completeTurn(options);
  const snapshot = fixture.controller.snapshot();
  assert.equal(snapshot.busy, false);
  assert.equal(snapshot.messages.at(-1)?.content, 'Hola desde Codex.');
  assert.equal(snapshot.messages.at(-1)?.streaming, false);
});

test('requires an approved structured creative brief before exact costly execution', async (t) => {
  const fixture = await controllerFixture(t);
  await fixture.controller.status();
  await fixture.controller.send('Prepara una grabación.', {
    jobId: 'job-123',
    kind: 'demo',
    label: 'Demo actual',
    pathname: '/matches/job-123',
  });
  const options = fixture.options();
  completeTurn(options, 'Necesito confirmar el brief.');

  const previewWithoutBrief = await options.onDynamicToolCall?.(dynamicCall('preview', {
    arguments: { job_id: 'job-123' },
    operation: 'jobs.record',
  }));
  assert.equal(previewWithoutBrief?.success, false);
  assert.match(previewWithoutBrief?.contentItems[0]?.text ?? '', /creative brief/i);

  const brief = await options.onDynamicToolCall?.(dynamicCall('creative_brief', {
    counter: 'on',
    cover: 'generated-gameplay-candidates',
    effect: 'punch-in',
    format: 'short-9x16',
    hud: 'full-game-ui',
    intro: 'hook',
    job_id: 'job-123',
    killfeed: 'preserve',
    music: 'none',
    operation: 'jobs.record',
    outro: 'loop',
    transition: 'cut',
  }));
  assert.equal(brief?.success, true);
  const briefID = fixture.controller.snapshot().pendingActions[0]?.id;
  assert.notEqual(briefID, undefined);
  await fixture.controller.approve(briefID as string);

  const preview = await options.onDynamicToolCall?.(dynamicCall('preview', {
    arguments: { job_id: 'job-123', segment_ids: [] },
    operation: 'jobs.record',
  }));
  assert.equal(preview?.success, true);
  const action = fixture.controller.snapshot().pendingActions.find((item) => item.operation === 'jobs.record');
  assert.notEqual(action, undefined);
  assert.equal(action?.preview?.fields?.some((field) => field.label === 'Job id' && field.value === 'job-123'), true);
  assert.equal(action?.preview?.fields?.some((field) => field.label === 'Segment ids' && field.value === '[]'), true);

  await fixture.controller.approve(action?.id as string);
  assert.deepEqual(fixture.calls.map((call) => call.privileged), [undefined, true]);
  assert.deepEqual(fixture.calls.at(-1)?.arguments, { job_id: 'job-123', segment_ids: [] });
  await assert.rejects(fixture.controller.approve(action?.id as string), /no longer available/);
});

test('redacts raw path, URL, and error details before a read reaches Codex', async (t) => {
  const fixture = await controllerFixture(t);
  await fixture.controller.status();
  await fixture.controller.send('Estado del demo.', {
    jobId: 'job-123',
    kind: 'demo',
    label: 'Demo actual',
    pathname: '/matches/job-123',
  });
  const options = fixture.options();
  completeTurn(options);

  const result = await options.onDynamicToolCall?.(dynamicCall('read', {
    arguments: { job_id: 'job-123' },
    operation: 'jobs.get',
  }));
  const text = result?.contentItems[0]?.text ?? '';
  assert.equal(result?.success, true);
  assert.doesNotMatch(text, /C:\\Users/i);
  assert.doesNotMatch(text, /http:\/\//i);
  assert.doesNotMatch(text, /"error"/i);
  assert.match(text, /parsed/);
});
