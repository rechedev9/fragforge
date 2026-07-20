import assert from 'node:assert/strict';
import { mkdtemp, rm } from 'node:fs/promises';
import * as os from 'node:os';
import * as path from 'node:path';
import test from 'node:test';
import { OperationGateway } from '../studio-operations/operation-gateway.ts';
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
  account: Awaited<ReturnType<CodexAppServer['readAccount']>> = {
    account: { email: 'creator@example.com', planType: 'plus', type: 'chatgpt' },
    requiresOpenaiAuth: true,
  };
  closed = false;
  readonly status = 'ready' as const;
  resumeThreadCalls = 0;
  startThreadCalls = 0;
  startThreadOptions: AppServerStartThreadOptions | undefined;
  turnCount = 0;
  turnText: string | undefined;
  startTurnImplementation: ((turnID: string, text: string) => Promise<AppServerTurn>) | undefined;
  interruptTurnImplementation: (() => Promise<void>) | undefined;

  close(): void {
    this.closed = true;
  }

  async cancelLogin(): Promise<void> {}

  async initialize(): Promise<void> {}

  async interruptTurn(): Promise<void> {
    await this.interruptTurnImplementation?.();
  }

  async loginChatGPT(): Promise<Awaited<ReturnType<CodexAppServer['loginChatGPT']>>> {
    return { authUrl: 'https://chatgpt.com/auth/login', loginId: 'login-1', type: 'chatgpt' };
  }

  async logoutAccount(): Promise<void> {
    this.account = { account: null, requiresOpenaiAuth: true };
  }

  async readAccount(): Promise<Awaited<ReturnType<CodexAppServer['readAccount']>>> {
    return this.account;
  }

  async resumeThread(threadId: string): Promise<AppServerThread> {
    this.resumeThreadCalls += 1;
    return { id: threadId, sessionId: 'session-1' };
  }

  async startThread(options?: AppServerStartThreadOptions): Promise<AppServerThread> {
    this.startThreadCalls += 1;
    this.startThreadOptions = options;
    return { id: 'thread-1', sessionId: 'session-1' };
  }

  async startTurn(_threadId: string, text: string, _options?: AppServerStartTurnOptions): Promise<AppServerTurn> {
    this.turnCount += 1;
    this.turnText = text;
    const turnID = `turn-${this.turnCount}`;
    return this.startTurnImplementation?.(turnID, text) ?? { id: turnID, status: 'inProgress' };
  }
}

async function controllerFixture(
  t: test.TestContext,
  controllerOptions: { interruptTimeoutMs?: number; turnTimeoutMs?: number } = {},
): Promise<{
  appServer: FakeAppServer;
  appServers: FakeAppServer[];
  calls: Array<{ arguments?: unknown; privileged?: boolean }>;
  controller: AssistantController;
  openedAuthURLs: string[];
  options(): CodexAppServerClientOptions;
  setExecuteImplementation(
    implementation: ((request: { arguments?: unknown; operation?: string }, options: { privileged?: boolean }) => Promise<unknown>) | undefined,
  ): void;
}> {
  const directory = await mkdtemp(path.join(os.tmpdir(), 'fragforge-assistant-controller-'));
  t.after(async () => removeTemporaryDirectory(directory));
  const appServer = new FakeAppServer();
  const appServers: FakeAppServer[] = [];
  let captured: CodexAppServerClientOptions | undefined;
  let executeImplementation: ((request: { arguments?: unknown; operation?: string }, options: { privileged?: boolean }) => Promise<unknown>) | undefined;
  const calls: Array<{ arguments?: unknown; privileged?: boolean }> = [];
  const openedAuthURLs: string[] = [];
  const gateway = {
    execute: async (request: { arguments?: unknown; operation?: string }, options: { privileged?: boolean } = {}) => {
      calls.push({ arguments: request.arguments, privileged: options.privileged });
      if (executeImplementation !== undefined) return executeImplementation(request, options);
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
  } as unknown as OperationGateway;
  const controller = new AssistantController({
    createAppServer: (options) => {
      captured = options;
      const created = appServers.length === 0 ? appServer : new FakeAppServer();
      appServers.push(created);
      return created;
    },
    cwd: path.join(directory, 'workspace'),
    gateway,
    history: new AssistantHistoryStore(path.join(directory, 'history.json')),
    interruptTimeoutMs: controllerOptions.interruptTimeoutMs,
    openAuthURL: async (url) => {
      openedAuthURLs.push(url);
    },
    orchestratorClient: new OrchestratorClient({ baseUrl: 'http://127.0.0.1:1' }),
    turnTimeoutMs: controllerOptions.turnTimeoutMs,
  });
  return {
    appServer,
    appServers,
    calls,
    controller,
    openedAuthURLs,
    options: () => {
      const options = captured;
      if (options === undefined) throw new Error('expected app-server options');
      return options;
    },
    setExecuteImplementation: (implementation) => {
      executeImplementation = implementation;
    },
  };
}

function completeTurn(options: CodexAppServerClientOptions, delta = 'Hola desde Codex.', turnID = 'turn-1'): void {
  options.onAgentMessageDelta?.({
    delta,
    itemId: 'item-1',
    threadId: 'thread-1',
    turnId: turnID,
  } satisfies AppServerAgentMessageDelta);
  options.onTurnCompleted?.({
    threadId: 'thread-1',
    turn: { id: turnID, status: 'completed' },
  });
}

function dynamicCall(
  tool: string,
  argumentsValue: AppServerDynamicToolCall['arguments'],
  turnID = 'turn-1',
): AppServerDynamicToolCall {
  return {
    arguments: argumentsValue,
    callId: `call-${tool}`,
    namespace: 'fragforge',
    requestId: `request-${tool}`,
    threadId: 'thread-1',
    tool,
    turnId: turnID,
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

async function waitFor(predicate: () => boolean): Promise<void> {
  for (let attempt = 0; attempt < 50; attempt += 1) {
    if (predicate()) return;
    await new Promise<void>((resolve) => setImmediate(resolve));
  }
  throw new Error('timed out waiting for test condition');
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

test('requires and manages a personal Codex OAuth account before agent turns', async (t) => {
  const fixture = await controllerFixture(t);
  fixture.appServer.account = { account: null, requiresOpenaiAuth: true };

  const signedOut = await fixture.controller.status();
  assert.equal(signedOut.availability, 'ready');
  assert.deepEqual(signedOut.account, { status: 'signed-out' });
  await assert.rejects(
    fixture.controller.send('Haz el reel.', { kind: 'none', label: 'Studio', pathname: '/' }),
    /not connected/,
  );

  await fixture.controller.login();
  assert.deepEqual(fixture.openedAuthURLs, ['https://chatgpt.com/auth/login']);
  assert.deepEqual(fixture.controller.snapshot().account, { status: 'signing-in' });

  fixture.options().onNotification?.({
    method: 'account/updated',
    params: { authMode: 'chatgpt', planType: 'pro' },
  });
  assert.deepEqual(fixture.controller.snapshot().account, { planType: 'pro', status: 'signed-in' });

  await fixture.controller.logout();
  assert.deepEqual(fixture.controller.snapshot().account, { status: 'signed-out' });
});

test('keeps a live thread attached across consecutive turns', async (t) => {
  const fixture = await controllerFixture(t);
  await fixture.controller.status();
  const options = fixture.options();

  await fixture.controller.send('Primer turno.', {
    kind: 'none',
    label: 'Studio',
    pathname: '/settings',
  });
  completeTurn(options, 'Primera respuesta.', 'turn-1');

  await fixture.controller.send('Segundo turno.', {
    kind: 'none',
    label: 'Studio',
    pathname: '/settings',
  });
  completeTurn(options, 'Segunda respuesta.', 'turn-2');

  assert.equal(fixture.appServer.startThreadCalls, 1);
  assert.equal(fixture.appServer.resumeThreadCalls, 0);
  assert.equal(fixture.appServer.turnCount, 2);
  assert.equal(fixture.controller.snapshot().busy, false);
  assert.equal(fixture.controller.snapshot().messages.at(-1)?.content, 'Segunda respuesta.');
});

test('does not let a late start response from a completed turn capture the next turn', async (t) => {
  const fixture = await controllerFixture(t);
  await fixture.controller.status();
  const options = fixture.options();
  let resolveFirst: ((turn: AppServerTurn) => void) | undefined;
  fixture.appServer.startTurnImplementation = async (turnID) => {
    if (turnID !== 'turn-1') return { id: turnID, status: 'inProgress' };
    return new Promise<AppServerTurn>((resolve) => {
      resolveFirst = resolve;
    });
  };

  const firstSend = fixture.controller.send('Primer turno.', {
    kind: 'none', label: 'Studio', pathname: '/settings',
  });
  await waitFor(() => fixture.appServer.turnCount === 1);
  completeTurn(options, 'Primera respuesta.', 'turn-1');

  await fixture.controller.send('Segundo turno.', {
    kind: 'none', label: 'Studio', pathname: '/settings',
  });
  resolveFirst?.({ id: 'turn-1', status: 'inProgress' });
  await firstSend;
  completeTurn(options, 'Segunda respuesta.', 'turn-2');

  const snapshot = fixture.controller.snapshot();
  assert.equal(snapshot.busy, false);
  assert.equal(snapshot.messages.at(-1)?.content, 'Segunda respuesta.');
});

test('cancels preparation even before Codex returns a turn id', async (t) => {
  const fixture = await controllerFixture(t);
  await fixture.controller.status();
  fixture.appServer.startTurnImplementation = async () => new Promise<AppServerTurn>(() => {});

  void fixture.controller.send('Turno que no arranca.', {
    kind: 'none', label: 'Studio', pathname: '/settings',
  });
  await waitFor(() => fixture.appServer.turnCount === 1);
  await fixture.controller.cancel();

  assert.equal(fixture.controller.snapshot().busy, false);
  assert.equal(fixture.appServer.closed, true);
});

test('rebuilds Codex when an interrupted turn never completes', async (t) => {
  const fixture = await controllerFixture(t, { interruptTimeoutMs: 10, turnTimeoutMs: 60_000 });
  await fixture.controller.status();
  await fixture.controller.send('Turno interrumpido.', {
    kind: 'none', label: 'Studio', pathname: '/settings',
  });
  fixture.appServer.interruptTurnImplementation = async () => new Promise<void>((_resolve, reject) => {
    setTimeout(() => reject(new Error('interrupt stayed pending')), 40);
  });

  const cancelling = fixture.controller.cancel();
  await new Promise<void>((resolve) => setTimeout(resolve, 25));
  await cancelling;

  assert.equal(fixture.controller.snapshot().busy, false);
  assert.equal(fixture.controller.snapshot().availability, 'ready');
  assert.equal(fixture.appServer.closed, true);
  assert.equal(fixture.appServers.length, 2);
});

test('reports a failed send when the transport dies before turn start resolves', async (t) => {
  const fixture = await controllerFixture(t);
  await fixture.controller.status();
  const options = fixture.options();
  fixture.appServer.startTurnImplementation = async () => {
    options.onError?.(new Error('transport failed'));
    throw new Error('transport failed');
  };

  await assert.rejects(fixture.controller.send('No se enviará.', {
    kind: 'none', label: 'Studio', pathname: '/settings',
  }), /No se pudo enviar/);
  assert.equal(fixture.controller.snapshot().availability, 'error');
  assert.equal(fixture.controller.snapshot().busy, false);
});

test('does not create an approval card after the originating tool turn completes', async (t) => {
  const fixture = await controllerFixture(t);
  await fixture.controller.status();
  const options = fixture.options();
  let resolvePreview: ((value: unknown) => void) | undefined;
  fixture.setExecuteImplementation(async () => new Promise((resolve) => {
    resolvePreview = resolve;
  }));
  await fixture.controller.send('Prepara borrar este trabajo.', {
    jobId: 'job-123', kind: 'demo', label: 'Demo actual', pathname: '/matches/job-123',
  });

  const toolResult = options.onDynamicToolCall?.(dynamicCall('preview', {
    arguments: { job_id: 'job-123' },
    operation: 'jobs.delete',
  }), new AbortController().signal);
  await waitFor(() => fixture.calls.length === 1);
  completeTurn(options, 'Turno terminado.');
  resolvePreview?.({
    arguments: { job_id: 'job-123' },
    kind: 'preview',
    operation: 'jobs.delete',
    preview: { method: 'DELETE', path: '/api/jobs/job-123' },
    requiresConfirmation: true,
    risk: 'destructive',
  });

  assert.equal((await toolResult)?.success, false);
  assert.equal(fixture.controller.snapshot().pendingActions.some((action) => action.operation === 'jobs.delete'), false);
});

test('rejects a completed turn tool call during the next turn startup window', async (t) => {
  const fixture = await controllerFixture(t);
  await fixture.controller.status();
  const options = fixture.options();
  await fixture.controller.send('Primer turno.', {
    kind: 'none', label: 'Studio', pathname: '/settings',
  });
  completeTurn(options, 'Primer turno completo.', 'turn-1');

  let resolveSecond: ((turn: AppServerTurn) => void) | undefined;
  fixture.appServer.startTurnImplementation = async (turnID) => new Promise<AppServerTurn>((resolve) => {
    assert.equal(turnID, 'turn-2');
    resolveSecond = resolve;
  });
  const secondSend = fixture.controller.send('Segundo turno.', {
    kind: 'none', label: 'Studio', pathname: '/settings',
  });
  await waitFor(() => fixture.appServer.turnCount === 2);

  const stale = await options.onDynamicToolCall?.(dynamicCall('preview', {
    arguments: { job_id: 'job-123' },
    operation: 'jobs.delete',
  }, 'turn-1'), new AbortController().signal);
  assert.equal(stale?.success, false);

  resolveSecond?.({ id: 'turn-2', status: 'inProgress' });
  await secondSend;
  completeTurn(options, 'Segundo turno completo.', 'turn-2');
  assert.equal(fixture.controller.snapshot().busy, false);
});

test('fails and rebuilds a turn that produces no terminal event', async (t) => {
  const fixture = await controllerFixture(t, { turnTimeoutMs: 10 });
  await fixture.controller.status();
  await fixture.controller.send('Turno sin final.', {
    kind: 'none', label: 'Studio', pathname: '/settings',
  });

  await new Promise<void>((resolve) => setTimeout(resolve, 25));

  const snapshot = fixture.controller.snapshot();
  assert.equal(snapshot.busy, false);
  assert.equal(snapshot.availability, 'ready');
  assert.equal(fixture.appServer.closed, true);
  assert.equal(fixture.appServers.length, 2);
  assert.match(snapshot.messages.at(-1)?.content ?? '', /no pudo completar/i);
});

test('starts a fresh app-server when a new conversation follows a connection failure', async (t) => {
  const fixture = await controllerFixture(t);
  await fixture.controller.status();
  fixture.options().onError?.(new Error('connection lost'));
  assert.equal(fixture.controller.snapshot().availability, 'error');

  await fixture.controller.newConversation();

  const snapshot = fixture.controller.snapshot();
  assert.equal(snapshot.availability, 'ready');
  assert.equal(snapshot.busy, false);
  assert.equal(snapshot.messages.length, 0);
  assert.equal(fixture.appServers.length, 2);
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

  const previewWithoutBrief = await options.onDynamicToolCall?.(dynamicCall('preview', {
    arguments: { job_id: 'job-123' },
    operation: 'jobs.record',
  }), new AbortController().signal);
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
  }), new AbortController().signal);
  assert.equal(brief?.success, true);
  completeTurn(options, 'Necesito confirmar el brief.');
  const briefID = fixture.controller.snapshot().pendingActions[0]?.id;
  assert.notEqual(briefID, undefined);
  await fixture.controller.approve(briefID as string);

  await fixture.controller.send('Prepara la acción aprobada.', {
    jobId: 'job-123',
    kind: 'demo',
    label: 'Demo actual',
    pathname: '/matches/job-123',
  });

  const preview = await options.onDynamicToolCall?.(dynamicCall('preview', {
    arguments: { job_id: 'job-123', segment_ids: [] },
    operation: 'jobs.record',
  }, 'turn-2'), new AbortController().signal);
  assert.equal(preview?.success, true);
  completeTurn(options, 'Acción preparada.', 'turn-2');
  const action = fixture.controller.snapshot().pendingActions.find((item) => item.operation === 'jobs.record');
  assert.notEqual(action, undefined);
  assert.equal(action?.preview?.fields?.some((field) => field.label === 'Job id' && field.value === 'job-123'), true);
  assert.equal(action?.preview?.fields?.some((field) => field.label === 'Segment ids' && field.value === '[]'), true);

  await fixture.controller.approve(action?.id as string);
  assert.deepEqual(fixture.calls.map((call) => call.privileged), [undefined, true]);
  assert.deepEqual(fixture.calls.at(-1)?.arguments, { job_id: 'job-123', segment_ids: [] });
  await assert.rejects(fixture.controller.approve(action?.id as string), /no longer available/);
});

test('exposes every typed operation except local file-picker intake', async (t) => {
  const fixture = await controllerFixture(t);
  await fixture.controller.status();
  await fixture.controller.send('Busca operaciones del agente.', {
    kind: 'none', label: 'Studio', pathname: '/settings',
  });
  const options = fixture.options();

  const captions = await options.onDynamicToolCall?.(dynamicCall('search', {
    include_dynamic_inputs: false,
    operation: 'streams.start_caption_candidates',
  }), new AbortController().signal);
  assert.equal(captions?.success, true);
  assert.match(captions?.contentItems[0]?.text ?? '', /streams\.start_caption_candidates/);

  const Twitch = await options.onDynamicToolCall?.(dynamicCall('search', {
    include_dynamic_inputs: false,
    operation: 'streams.create_from_url',
  }), new AbortController().signal);
  assert.equal(Twitch?.success, true);
  assert.match(Twitch?.contentItems[0]?.text ?? '', /streams\.create_from_url/);

  const localFile = await options.onDynamicToolCall?.(dynamicCall('search', {
    include_dynamic_inputs: false,
    operation: 'jobs.create',
  }), new AbortController().signal);
  assert.equal(localFile?.success, false);
  completeTurn(options);
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

  const result = await options.onDynamicToolCall?.(dynamicCall('read', {
    arguments: { job_id: 'job-123' },
    operation: 'jobs.get',
  }), new AbortController().signal);
  completeTurn(options);
  const text = result?.contentItems[0]?.text ?? '';
  assert.equal(result?.success, true);
  assert.doesNotMatch(text, /C:\\Users/i);
  assert.doesNotMatch(text, /http:\/\//i);
  assert.doesNotMatch(text, /"error"/i);
  assert.match(text, /parsed/);
});
