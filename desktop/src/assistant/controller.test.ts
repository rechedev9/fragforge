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
  controllerOptions: {
    interruptTimeoutMs?: number;
    backgroundHibernateMs?: number;
    foregroundHibernateMs?: number;
    startAwake?: boolean;
    selectLocalMedia?: (kind: 'demo' | 'stream') => Promise<string | null>;
    stateWatchIntervalMs?: number;
    stateWatchTimeoutMs?: number;
    turnTimeoutMs?: number;
  } = {},
): Promise<{
  appServer: FakeAppServer;
  appServers: FakeAppServer[];
  calls: Array<{ arguments?: unknown; operation?: string; privileged?: boolean }>;
  controller: AssistantController;
  openedAuthURLs: string[];
  options(): CodexAppServerClientOptions;
  setExecuteImplementation(
    implementation: ((request: { arguments?: unknown; operation?: string }, options: { privileged?: boolean; signal?: AbortSignal }) => Promise<unknown>) | undefined,
  ): void;
}> {
  const directory = await mkdtemp(path.join(os.tmpdir(), 'fragforge-assistant-controller-'));
  t.after(async () => removeTemporaryDirectory(directory));
  const appServer = new FakeAppServer();
  const appServers: FakeAppServer[] = [];
  let captured: CodexAppServerClientOptions | undefined;
  let executeImplementation: ((request: { arguments?: unknown; operation?: string }, options: { privileged?: boolean; signal?: AbortSignal }) => Promise<unknown>) | undefined;
  const calls: Array<{ arguments?: unknown; operation?: string; privileged?: boolean }> = [];
  const openedAuthURLs: string[] = [];
  const gateway = {
    execute: async (request: { arguments?: unknown; operation?: string }, options: { privileged?: boolean; signal?: AbortSignal } = {}) => {
      calls.push({ arguments: request.arguments, operation: request.operation, privileged: options.privileged });
      if (executeImplementation !== undefined) return executeImplementation(request, options);
      if (request.operation === 'streams.get_edit_plan') {
        return {
          arguments: request.arguments ?? {},
          kind: 'executed' as const,
          operation: 'streams.get_edit_plan',
          partialFailure: false,
          result: streamEditPlan(),
          status: 'completed' as const,
        };
      }
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
          operation: request.operation ?? 'jobs.record',
          partialFailure: false,
          result: { accepted: true },
          status: 'completed' as const,
        };
      }
      return {
        arguments: request.arguments ?? {},
        kind: 'preview' as const,
        operation: request.operation ?? 'jobs.record',
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
    backgroundHibernateMs: controllerOptions.backgroundHibernateMs,
    foregroundHibernateMs: controllerOptions.foregroundHibernateMs,
    openAuthURL: async (url) => {
      openedAuthURLs.push(url);
    },
    orchestratorClient: new OrchestratorClient({ baseUrl: 'http://127.0.0.1:1' }),
    selectLocalMedia: controllerOptions.selectLocalMedia,
    stateWatchIntervalMs: controllerOptions.stateWatchIntervalMs,
    stateWatchTimeoutMs: controllerOptions.stateWatchTimeoutMs,
    turnTimeoutMs: controllerOptions.turnTimeoutMs,
  });
  if (controllerOptions.startAwake !== false) await controller.wake();
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

function creativeBriefArguments(operation = 'jobs.generate', jobID = 'job-123') {
  return {
    counter: 'on',
    cover: 'generated-gameplay-candidates',
    effect: 'punch-in',
    format: 'short-9x16',
    hud: 'full-game-ui',
    intro: 'hook',
    job_id: jobID,
    killfeed: 'preserve',
    music: 'none',
    operation,
    outro: 'loop',
    transition: 'cut',
  };
}

function streamCreativeBriefArguments(streamJobID = 'stream-123') {
  return {
    captions: 'spanish-reviewed',
    clip_selection: 'saved-edit-plan',
    cover: 'generated-gameplay-candidates',
    format: 'short-9x16',
    framing: 'clean-crop',
    killfeed: 'preserve',
    layout: 'streamer-vertical-stack-40-60',
    music: 'none',
    operation: 'streams.start_render',
    stream_job_id: streamJobID,
    title: 'La ronda imposible',
  };
}

function streamEditPlan(updatedAt = '2026-07-20T20:00:00Z') {
  return {
    captions: { enabled: true, language: 'es' },
    clips: [{
      caption_reviewed: true,
      end_seconds: 20,
      id: 'clip-1',
      killfeed_seconds: [15],
      start_seconds: 10,
      title: 'La ronda imposible',
    }],
    face_crop: { height: 0.4, width: 0.4, x: 0.1, y: 0.1 },
    gameplay_crop: { height: 1, width: 1, x: 0, y: 0 },
    music: {},
    updated_at: updatedAt,
    variant: 'streamer-vertical-stack-40-60',
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

test('status stays asleep until an explicit wake and background idle hibernates safely', async (t) => {
  const fixture = await controllerFixture(t, {
    backgroundHibernateMs: 5,
    foregroundHibernateMs: 60_000,
    startAwake: false,
  });

  const sleeping = await fixture.controller.status();
  assert.equal(sleeping.availability, 'sleeping');
  assert.equal(fixture.appServers.length, 0);

  await fixture.controller.wake();
  assert.equal(fixture.controller.snapshot().availability, 'ready');
  assert.equal(fixture.appServers.length, 1);
  await fixture.controller.send('Conserva este mensaje.', {
    kind: 'none',
    label: 'Studio',
    pathname: '/settings',
  });
  completeTurn(fixture.options(), 'Historial conservado.');
  const messageCount = fixture.controller.snapshot().messages.length;

  fixture.controller.setWindowActive(false);
  await new Promise<void>((resolve) => setTimeout(resolve, 20));
  await waitFor(() => fixture.controller.snapshot().availability === 'sleeping');
  assert.equal(fixture.appServer.closed, true);
  assert.equal(fixture.controller.snapshot().messages.length, messageCount);
});

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
  const fixture = await controllerFixture(t, { startAwake: false });
  fixture.appServer.account = { account: null, requiresOpenaiAuth: true };
  await fixture.controller.wake();

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
  await fixture.controller.wake();

  const snapshot = fixture.controller.snapshot();
  assert.equal(snapshot.availability, 'ready');
  assert.equal(snapshot.busy, false);
  assert.equal(snapshot.messages.length, 0);
  assert.equal(fixture.appServers.length, 2);
});

test('resumes jobs.generate after creative brief approval and still requires exact costly approval', async (t) => {
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
    arguments: { job_id: 'job-123', preset: 'viral-60-clean' },
    operation: 'jobs.generate',
  }), new AbortController().signal);
  assert.equal(previewWithoutBrief?.success, false);
  assert.match(previewWithoutBrief?.contentItems[0]?.text ?? '', /creative brief/i);

  const brief = await options.onDynamicToolCall?.(dynamicCall(
    'creative_brief',
    creativeBriefArguments(),
  ), new AbortController().signal);
  assert.equal(brief?.success, true);
  completeTurn(options, 'Necesito confirmar el brief.');
  const briefID = fixture.controller.snapshot().pendingActions[0]?.id;
  assert.notEqual(briefID, undefined);

  await fixture.controller.send('Consulta otra demo sin tocar el brief pendiente.', {
    jobId: 'job-other',
    kind: 'demo',
    label: 'Otra demo',
    pathname: '/matches/job-other',
  });
  completeTurn(options, 'La otra demo está lista.', 'turn-2');
  await fixture.controller.approve(briefID as string);

  const resumed = fixture.controller.snapshot();
  assert.equal(fixture.appServer.turnCount, 3);
  assert.equal(resumed.busy, true);
  assert.equal(resumed.messages.filter((message) => message.role === 'user').length, 2);
  assert.match(fixture.appServer.turnText ?? '', /approved the creative brief/i);
  assert.match(fixture.appServer.turnText ?? '', /jobs\.generate/);
  assert.match(fixture.appServer.turnText ?? '', /job-123/);
  assert.doesNotMatch(fixture.appServer.turnText ?? '', /job-other/);
  assert.match(fixture.appServer.turnText ?? '', /counter=on/);

  const preview = await options.onDynamicToolCall?.(dynamicCall('preview', {
    arguments: { job_id: 'job-123', preset: 'viral-60-clean', segment_ids: ['segment-1'] },
    operation: 'jobs.generate',
  }, 'turn-3'), new AbortController().signal);
  assert.equal(preview?.success, true);
  completeTurn(options, 'Acción preparada.', 'turn-3');
  const action = fixture.controller.snapshot().pendingActions.find((item) => item.operation === 'jobs.generate');
  assert.notEqual(action, undefined);
  assert.equal(action?.preview?.fields?.some((field) => field.label === 'Job id' && field.value === 'job-123'), true);
  assert.equal(action?.preview?.fields?.some((field) => field.label === 'Segment ids · 1' && field.value === 'segment-1'), true);
  const nativePrompt = fixture.controller.approvalPrompt(action?.id as string);
  assert.equal(nativePrompt.operation, 'jobs.generate');
  assert.equal(nativePrompt.risk, 'costly');
  assert.deepEqual(nativePrompt.fields, action?.preview?.fields);

  await fixture.controller.approve(action?.id as string);
  assert.deepEqual(fixture.calls.map((call) => call.privileged), [undefined, true]);
  assert.deepEqual(fixture.calls.at(-1)?.arguments, { job_id: 'job-123', preset: 'viral-60-clean', segment_ids: ['segment-1'] });
  const afterApproval = fixture.controller.snapshot();
  assert.equal(afterApproval.busy, true);
  assert.equal(fixture.appServer.turnCount, 4);
  assert.equal(afterApproval.messages.some((message) => /inició jobs\.generate.*segundo plano/i.test(message.content)), true);
  assert.match(fixture.appServer.turnText ?? '', /continue owning the workflow/i);
  assert.match(fixture.appServer.turnText ?? '', /Current demo job: job-123/);
  completeTurn(options, 'Revisaré el estado sin repetir el render.', 'turn-4');
  await assert.rejects(fixture.controller.approve(action?.id as string), /no longer available/);
});

test('records a rejected creative brief so the agent does not treat it as approved', async (t) => {
  const fixture = await controllerFixture(t);
  await fixture.controller.status();
  await fixture.controller.send('Prepara un reel.', {
    jobId: 'job-123', kind: 'demo', label: 'Demo actual', pathname: '/matches/job-123',
  });
  const options = fixture.options();
  const result = await options.onDynamicToolCall?.(dynamicCall(
    'creative_brief',
    creativeBriefArguments(),
  ), new AbortController().signal);
  assert.equal(result?.success, true);
  completeTurn(options, 'Confirma el brief.', 'turn-1');

  const briefID = fixture.controller.snapshot().pendingActions[0]?.id;
  assert.notEqual(briefID, undefined);
  fixture.controller.reject(briefID as string);

  const rejected = fixture.controller.snapshot();
  assert.equal(rejected.pendingActions[0]?.status, 'rejected');
  assert.match(rejected.messages.at(-1)?.content ?? '', /rechazó el brief creativo.*no se ejecutó/i);

  await fixture.controller.send('¿Qué hago ahora?', {
    jobId: 'job-123', kind: 'demo', label: 'Demo actual', pathname: '/matches/job-123',
  });
  assert.match(fixture.appServer.turnText ?? '', /rechazó el brief creativo.*no se ejecutó/i);
  completeTurn(options, 'Propón los cambios.', 'turn-2');
});

test('uses a stream-specific creative brief and binds approval to its exact layout', async (t) => {
  const fixture = await controllerFixture(t);
  await fixture.controller.status();
  await fixture.controller.send('Prepara el render del stream.', {
    kind: 'stream', label: 'Stream actual', pathname: '/streams', streamJobId: 'stream-123',
  });
  const options = fixture.options();
  const brief = await options.onDynamicToolCall?.(dynamicCall(
    'creative_brief',
    streamCreativeBriefArguments(),
  ), new AbortController().signal);
  assert.equal(brief?.success, true);
  const card = fixture.controller.snapshot().pendingActions.find((item) => item.operation === 'studio.confirm_creative_brief');
  assert.equal(card?.preview?.fields?.some((field) => field.label === 'Layout' && /vertical-stack/.test(field.value)), true);
  assert.equal(card?.preview?.fields?.some((field) => field.label === 'Revisión del plan' && field.value.length === 16), true);
  assert.equal(card?.preview?.fields?.some((field) => field.label === 'Corte 1 de 1'
    && field.value === 'clip-1 · 10-20s · La ronda imposible'), true);
  assert.equal(card?.preview?.fields?.some((field) => field.label === 'Subtítulos' && field.value === 'spanish-reviewed'), true);
  assert.equal(card?.preview?.fields?.some((field) => field.label === 'HUD'), false);
  completeTurn(options, 'Confirma el brief de stream.', 'turn-1');

  await fixture.controller.approve(card?.id as string);

  assert.match(fixture.appServer.turnText ?? '', /layout=streamer-vertical-stack-40-60/);
  assert.match(fixture.appServer.turnText ?? '', /captions=spanish-reviewed/);
  const wrongLayout = await options.onDynamicToolCall?.(dynamicCall('preview', {
    arguments: { stream_job_id: 'stream-123', variant: 'streamer-fullframe-nocam' },
    operation: 'streams.start_render',
  }, 'turn-2'), new AbortController().signal);
  assert.equal(wrongLayout?.success, false);
  assert.match(wrongLayout?.contentItems[0]?.text ?? '', /creative brief/i);

  const exactLayout = await options.onDynamicToolCall?.(dynamicCall('preview', {
    arguments: { stream_job_id: 'stream-123', variant: 'streamer-vertical-stack-40-60' },
    operation: 'streams.start_render',
  }, 'turn-2'), new AbortController().signal);
  assert.equal(exactLayout?.success, true);
  const renderPreviewCall = fixture.calls.filter((call) => call.operation === 'streams.start_render').at(-1);
  assert.deepEqual(renderPreviewCall?.arguments, {
    expected_edit_plan_updated_at: '2026-07-20T20:00:00Z',
    stream_job_id: 'stream-123',
    variant: 'streamer-vertical-stack-40-60',
  });
  completeTurn(options, 'Render de stream preparado.', 'turn-2');
});

test('rejects stream briefs whose format or subtitle language contradicts the saved plan', async (t) => {
  for (const testCase of [
    {
      name: 'format',
      plan: streamEditPlan(),
      brief: { ...streamCreativeBriefArguments(), format: 'landscape-16x9' },
    },
    {
      name: 'caption language',
      plan: { ...streamEditPlan(), captions: { enabled: true, language: 'en' } },
      brief: streamCreativeBriefArguments(),
    },
  ]) {
    await t.test(testCase.name, async (t) => {
      const fixture = await controllerFixture(t);
      fixture.setExecuteImplementation(async (request) => ({
        arguments: request.arguments ?? {},
        kind: 'executed' as const,
        operation: request.operation ?? 'streams.get_edit_plan',
        partialFailure: false,
        result: testCase.plan,
        status: 'completed' as const,
      }));
      await fixture.controller.status();
      await fixture.controller.send('Prepara el stream.', {
        kind: 'stream', label: 'Stream actual', pathname: '/streams', streamJobId: 'stream-123',
      });
      const result = await fixture.options().onDynamicToolCall?.(dynamicCall(
        'creative_brief',
        testCase.brief,
      ), new AbortController().signal);

      assert.equal(result?.success, false);
      assert.equal(fixture.controller.snapshot().pendingActions.some((item) => item.operation === 'studio.confirm_creative_brief'), false);
    });
  }
});

test('derives landscape delivery and full-frame framing from the saved stream layout', async (t) => {
  const fixture = await controllerFixture(t);
  const plan = { ...streamEditPlan(), variant: 'streamer-landscape-16x9' };
  fixture.setExecuteImplementation(async (request) => ({
    arguments: request.arguments ?? {},
    kind: 'executed' as const,
    operation: request.operation ?? 'streams.get_edit_plan',
    partialFailure: false,
    result: plan,
    status: 'completed' as const,
  }));
  await fixture.controller.status();
  await fixture.controller.send('Prepara el stream horizontal.', {
    kind: 'stream', label: 'Stream actual', pathname: '/streams', streamJobId: 'stream-123',
  });
  const result = await fixture.options().onDynamicToolCall?.(dynamicCall('creative_brief', {
    ...streamCreativeBriefArguments(),
    format: 'landscape-16x9',
    framing: 'full-frame',
    layout: 'streamer-landscape-16x9',
  }), new AbortController().signal);

  assert.equal(result?.success, true);
  const card = fixture.controller.snapshot().pendingActions.find((item) => item.operation === 'studio.confirm_creative_brief');
  assert.equal(card?.preview?.fields?.some((field) => field.label === 'Formato' && field.value === 'landscape-16x9'), true);
  assert.equal(card?.preview?.fields?.some((field) => field.label === 'Encuadre' && field.value === 'full-frame'), true);
});

test('rejects stream plans that cannot be shown completely in one approval card', async (t) => {
  for (const testCase of [
    {
      brief: { ...streamCreativeBriefArguments(), killfeed: 'none', title: 'Sin título' },
      name: 'too many clips',
      plan: {
        ...streamEditPlan(),
        clips: Array.from({ length: 39 }, (_value, index) => ({
          caption_reviewed: true,
          end_seconds: index + 1,
          id: `clip-${index + 1}`,
          start_seconds: index,
        })),
      },
    },
    {
      brief: { ...streamCreativeBriefArguments(), title: 'x'.repeat(230) },
      name: 'clip field too long',
      plan: {
        ...streamEditPlan(),
        clips: [{ ...streamEditPlan().clips[0], title: 'x'.repeat(230) }],
      },
    },
  ]) {
    await t.test(testCase.name, async (t) => {
      const fixture = await controllerFixture(t);
      fixture.setExecuteImplementation(async (request) => ({
        arguments: request.arguments ?? {},
        kind: 'executed' as const,
        operation: request.operation ?? 'streams.get_edit_plan',
        partialFailure: false,
        result: testCase.plan,
        status: 'completed' as const,
      }));
      await fixture.controller.status();
      await fixture.controller.send('Prepara el brief exacto.', {
        kind: 'stream', label: 'Stream actual', pathname: '/streams', streamJobId: 'stream-123',
      });
      const result = await fixture.options().onDynamicToolCall?.(dynamicCall(
        'creative_brief',
        testCase.brief,
      ), new AbortController().signal);

      assert.equal(result?.success, false);
      assert.equal(fixture.controller.snapshot().pendingActions.some((item) => item.operation === 'studio.confirm_creative_brief'), false);
    });
  }
});

test('keeps explicitly unreviewed caption words in the auto-review brief state', async (t) => {
  const fixture = await controllerFixture(t);
  const currentPlan = streamEditPlan();
  const plan = {
    ...currentPlan,
    clips: [{
      ...currentPlan.clips[0],
      caption_reviewed: false,
      caption_words: [{ end_seconds: 0.5, start_seconds: 0, word: 'hola' }],
    }],
  };
  fixture.setExecuteImplementation(async (request) => ({
    arguments: request.arguments ?? {},
    kind: 'executed' as const,
    operation: request.operation ?? 'streams.get_edit_plan',
    partialFailure: false,
    result: plan,
    status: 'completed' as const,
  }));
  await fixture.controller.status();
  await fixture.controller.send('Prepara el stream.', {
    kind: 'stream', label: 'Stream actual', pathname: '/streams', streamJobId: 'stream-123',
  });
  const result = await fixture.options().onDynamicToolCall?.(dynamicCall('creative_brief', {
    ...streamCreativeBriefArguments(),
    captions: 'spanish-auto-review',
  }), new AbortController().signal);

  assert.equal(result?.success, true);
  const card = fixture.controller.snapshot().pendingActions.find((item) => item.operation === 'studio.confirm_creative_brief');
  assert.equal(card?.preview?.fields?.some((field) => field.label === 'Subtítulos' && field.value === 'spanish-auto-review'), true);
});

test('invalidates an approved stream brief when its saved edit plan changes', async (t) => {
  const fixture = await controllerFixture(t);
  await fixture.controller.status();
  await fixture.controller.send('Prepara el stream y deja el brief listo.', {
    kind: 'stream', label: 'Stream actual', pathname: '/streams', streamJobId: 'stream-123',
  });
  const options = fixture.options();
  await options.onDynamicToolCall?.(dynamicCall('creative_brief', streamCreativeBriefArguments()), new AbortController().signal);
  completeTurn(options, 'Confirma el brief.', 'turn-1');
  const brief = fixture.controller.snapshot().pendingActions.find((item) => item.operation === 'studio.confirm_creative_brief');
  await fixture.controller.approve(brief?.id as string);

  const renderPreview = await options.onDynamicToolCall?.(dynamicCall('preview', {
    arguments: { stream_job_id: 'stream-123', variant: 'streamer-vertical-stack-40-60' },
    operation: 'streams.start_render',
  }, 'turn-2'), new AbortController().signal);
  assert.equal(renderPreview?.success, true);
  completeTurn(options, 'Render preparado con el brief actual.', 'turn-2');
  const oldRenderAction = fixture.controller.snapshot().pendingActions.find((item) => item.operation === 'streams.start_render');

  await fixture.controller.send('Cambia el título antes de renderizar.', {
    kind: 'stream', label: 'Stream actual', pathname: '/streams', streamJobId: 'stream-123',
  });
  const editPreview = await options.onDynamicToolCall?.(dynamicCall('preview', {
    arguments: {
      plan: { clips: [{ end_seconds: 20, id: 'clip-1', start_seconds: 10 }], title: 'Nuevo título' },
      stream_job_id: 'stream-123',
    },
    operation: 'streams.update_edit_plan',
  }, 'turn-3'), new AbortController().signal);
  assert.equal(editPreview?.success, true);
  completeTurn(options, 'Cambio de edición preparado.', 'turn-3');
  const editAction = fixture.controller.snapshot().pendingActions.find((item) => item.operation === 'streams.update_edit_plan');
  await fixture.controller.approve(editAction?.id as string);

  const invalidated = fixture.controller.snapshot();
  assert.equal(invalidated.messages.some((message) => /brief creativo anterior quedó invalidado/i.test(message.content)), true);
  assert.equal(invalidated.messages.some((message) => /caducó la confirmación de render pendiente/i.test(message.content)), true);
  assert.equal(invalidated.pendingActions.find((item) => item.id === oldRenderAction?.id)?.status, 'expired');
  const staleBriefRender = await options.onDynamicToolCall?.(dynamicCall('preview', {
    arguments: { stream_job_id: 'stream-123', variant: 'streamer-vertical-stack-40-60' },
    operation: 'streams.start_render',
  }, 'turn-4'), new AbortController().signal);
  assert.equal(staleBriefRender?.success, false);
  assert.match(staleBriefRender?.contentItems[0]?.text ?? '', /creative brief/i);
  completeTurn(options, 'Pediré revisar el brief actualizado.', 'turn-4');
  await assert.rejects(fixture.controller.approve(oldRenderAction?.id as string), /no longer available/);
});

test('expires a pending stream brief when the agent changes its saved edit plan', async (t) => {
  const fixture = await controllerFixture(t);
  await fixture.controller.status();
  await fixture.controller.send('Prepara el brief del stream.', {
    kind: 'stream', label: 'Stream actual', pathname: '/streams', streamJobId: 'stream-123',
  });
  const options = fixture.options();
  await options.onDynamicToolCall?.(dynamicCall('creative_brief', streamCreativeBriefArguments()), new AbortController().signal);
  completeTurn(options, 'Brief pendiente.', 'turn-1');
  const pendingBrief = fixture.controller.snapshot().pendingActions.find((item) => item.operation === 'studio.confirm_creative_brief');

  await fixture.controller.send('Cambia el plan antes de aprobarlo.', {
    kind: 'stream', label: 'Stream actual', pathname: '/streams', streamJobId: 'stream-123',
  });
  await options.onDynamicToolCall?.(dynamicCall('preview', {
    arguments: {
      plan: { clips: [{ end_seconds: 20, id: 'clip-1', start_seconds: 10 }], variant: 'streamer-vertical-stack-40-60' },
      stream_job_id: 'stream-123',
    },
    operation: 'streams.update_edit_plan',
  }, 'turn-2'), new AbortController().signal);
  completeTurn(options, 'Cambio preparado.', 'turn-2');
  const editAction = fixture.controller.snapshot().pendingActions.find((item) => item.operation === 'streams.update_edit_plan');
  await fixture.controller.approve(editAction?.id as string);

  const snapshot = fixture.controller.snapshot();
  assert.equal(snapshot.pendingActions.find((item) => item.id === pendingBrief?.id)?.status, 'expired');
  assert.equal(snapshot.messages.some((message) => /caducó el brief pendiente/i.test(message.content)), true);
  completeTurn(options, 'Continuaré con el plan nuevo.', 'turn-3');
  await assert.rejects(fixture.controller.approve(pendingBrief?.id as string), /no longer available/);
});

test('revalidates a pending stream brief against external plan changes at approval time', async (t) => {
  const fixture = await controllerFixture(t);
  let planUpdatedAt = '2026-07-20T20:00:00Z';
  fixture.setExecuteImplementation(async (request) => ({
    arguments: request.arguments ?? {},
    kind: 'executed' as const,
    operation: request.operation ?? 'streams.get_edit_plan',
    partialFailure: false,
    result: streamEditPlan(planUpdatedAt),
    status: 'completed' as const,
  }));
  await fixture.controller.status();
  await fixture.controller.send('Prepara el brief del stream.', {
    kind: 'stream', label: 'Stream actual', pathname: '/streams', streamJobId: 'stream-123',
  });
  const options = fixture.options();
  await options.onDynamicToolCall?.(dynamicCall('creative_brief', streamCreativeBriefArguments()), new AbortController().signal);
  completeTurn(options, 'Brief pendiente.', 'turn-1');
  const pendingBrief = fixture.controller.snapshot().pendingActions.find((item) => item.operation === 'studio.confirm_creative_brief');

  planUpdatedAt = '2026-07-20T20:01:00Z';
  await assert.rejects(fixture.controller.approve(pendingBrief?.id as string), /plan del stream cambió/i);

  const snapshot = fixture.controller.snapshot();
  assert.equal(snapshot.pendingActions.find((item) => item.id === pendingBrief?.id)?.status, 'expired');
  assert.equal(snapshot.messages.some((message) => /brief pendiente caducó/i.test(message.content)), true);
});

test('refuses a prepared stream render when the canonical plan changes outside the agent', async (t) => {
  const fixture = await controllerFixture(t);
  let planUpdatedAt = '2026-07-20T20:00:00Z';
  fixture.setExecuteImplementation(async (request, options) => {
    if (request.operation === 'streams.get_edit_plan') {
      return {
        arguments: request.arguments ?? {},
        kind: 'executed' as const,
        operation: 'streams.get_edit_plan',
        partialFailure: false,
        result: streamEditPlan(planUpdatedAt),
        status: 'completed' as const,
      };
    }
    if (options.privileged) throw new Error('stale render must not execute');
    return {
      arguments: request.arguments ?? {},
      kind: 'preview' as const,
      operation: request.operation ?? 'streams.start_render',
      preview: { method: 'POST' },
      requiresConfirmation: true as const,
      risk: 'costly' as const,
    };
  });
  await fixture.controller.status();
  await fixture.controller.send('Prepara este render de stream.', {
    kind: 'stream', label: 'Stream actual', pathname: '/streams', streamJobId: 'stream-123',
  });
  const options = fixture.options();
  await options.onDynamicToolCall?.(dynamicCall('creative_brief', streamCreativeBriefArguments()), new AbortController().signal);
  completeTurn(options, 'Confirma el brief.', 'turn-1');
  const brief = fixture.controller.snapshot().pendingActions.find((item) => item.operation === 'studio.confirm_creative_brief');
  await fixture.controller.approve(brief?.id as string);
  const renderPreview = await options.onDynamicToolCall?.(dynamicCall('preview', {
    arguments: { stream_job_id: 'stream-123', variant: 'streamer-vertical-stack-40-60' },
    operation: 'streams.start_render',
  }, 'turn-2'), new AbortController().signal);
  assert.equal(renderPreview?.success, true);
  completeTurn(options, 'Render preparado.', 'turn-2');
  const renderAction = fixture.controller.snapshot().pendingActions.find((item) => item.operation === 'streams.start_render');

  planUpdatedAt = '2026-07-20T20:01:00Z';
  await assert.rejects(fixture.controller.approve(renderAction?.id as string), /No se pudo aplicar/);

  const rejected = fixture.controller.snapshot();
  assert.equal(rejected.pendingActions.find((item) => item.id === renderAction?.id)?.status, 'failed');
  assert.equal(fixture.calls.some((call) => call.operation === 'streams.start_render' && call.privileged === true), false);
  assert.equal(rejected.messages.some((message) => /brief creativo anterior quedó invalidado/i.test(message.content)), true);
});

test('invalidates sibling render approvals when the backend rejects the atomic plan revision', async (t) => {
  const fixture = await controllerFixture(t);
  fixture.setExecuteImplementation(async (request, options) => {
    if (request.operation === 'streams.get_edit_plan') {
      return {
        arguments: request.arguments ?? {},
        kind: 'executed' as const,
        operation: 'streams.get_edit_plan',
        partialFailure: false,
        result: streamEditPlan(),
        status: 'completed' as const,
      };
    }
    if (options.privileged) throw new Error('stream edit plan changed after approval');
    return {
      arguments: request.arguments ?? {},
      kind: 'preview' as const,
      operation: request.operation ?? 'streams.start_render',
      preview: { method: 'POST' },
      requiresConfirmation: true as const,
      risk: 'costly' as const,
    };
  });
  await fixture.controller.status();
  await fixture.controller.send('Prepara el render del stream.', {
    kind: 'stream', label: 'Stream actual', pathname: '/streams', streamJobId: 'stream-123',
  });
  const options = fixture.options();
  await options.onDynamicToolCall?.(dynamicCall('creative_brief', streamCreativeBriefArguments()), new AbortController().signal);
  completeTurn(options, 'Brief pendiente.', 'turn-1');
  const brief = fixture.controller.snapshot().pendingActions.find((item) => item.operation === 'studio.confirm_creative_brief');
  await fixture.controller.approve(brief?.id as string);
  const renderRequest = {
    arguments: { stream_job_id: 'stream-123', variant: 'streamer-vertical-stack-40-60' },
    operation: 'streams.start_render',
  };
  await options.onDynamicToolCall?.(dynamicCall('preview', renderRequest, 'turn-2'), new AbortController().signal);
  await options.onDynamicToolCall?.(dynamicCall('preview', renderRequest, 'turn-2'), new AbortController().signal);
  completeTurn(options, 'Dos confirmaciones preparadas.', 'turn-2');
  const renderActions = fixture.controller.snapshot().pendingActions.filter((item) => item.operation === 'streams.start_render');
  assert.equal(renderActions.length, 2);

  await assert.rejects(fixture.controller.approve(renderActions[0]?.id as string), /No se pudo aplicar/);

  const snapshot = fixture.controller.snapshot();
  assert.equal(snapshot.pendingActions.find((item) => item.id === renderActions[0]?.id)?.status, 'failed');
  assert.equal(snapshot.pendingActions.find((item) => item.id === renderActions[1]?.id)?.status, 'expired');
  assert.equal(snapshot.messages.some((message) => /brief creativo anterior quedó invalidado/i.test(message.content)), true);
  const retry = await options.onDynamicToolCall?.(dynamicCall('preview', renderRequest, 'turn-2'), new AbortController().signal);
  assert.equal(retry?.success, false);
});

test('invalidates stream approvals after a durable partial edit-plan mutation', async (t) => {
  const fixture = await controllerFixture(t);
  await fixture.controller.status();
  await fixture.controller.send('Prepara el stream.', {
    kind: 'stream', label: 'Stream actual', pathname: '/streams', streamJobId: 'stream-123',
  });
  const options = fixture.options();
  await options.onDynamicToolCall?.(dynamicCall('creative_brief', streamCreativeBriefArguments()), new AbortController().signal);
  completeTurn(options, 'Confirma el brief.', 'turn-1');
  const brief = fixture.controller.snapshot().pendingActions.find((item) => item.operation === 'studio.confirm_creative_brief');
  await fixture.controller.approve(brief?.id as string);
  await options.onDynamicToolCall?.(dynamicCall('preview', {
    arguments: { stream_job_id: 'stream-123', variant: 'streamer-vertical-stack-40-60' },
    operation: 'streams.start_render',
  }, 'turn-2'), new AbortController().signal);
  completeTurn(options, 'Render preparado.', 'turn-2');
  const renderAction = fixture.controller.snapshot().pendingActions.find((item) => item.operation === 'streams.start_render');

  await fixture.controller.send('Actualiza el plan.', {
    kind: 'stream', label: 'Stream actual', pathname: '/streams', streamJobId: 'stream-123',
  });
  const editPreview = await options.onDynamicToolCall?.(dynamicCall('preview', {
    arguments: {
      plan: { clips: [{ end_seconds: 20, id: 'clip-1', start_seconds: 10 }], title: 'Nuevo título' },
      stream_job_id: 'stream-123',
    },
    operation: 'streams.update_edit_plan',
  }, 'turn-3'), new AbortController().signal);
  assert.equal(editPreview?.success, true);
  completeTurn(options, 'Cambio preparado.', 'turn-3');
  const editAction = fixture.controller.snapshot().pendingActions.find((item) => item.operation === 'streams.update_edit_plan');
  fixture.setExecuteImplementation(async (request, executeOptions) => ({
    arguments: request.arguments ?? {},
    kind: 'executed' as const,
    operation: request.operation ?? 'streams.update_edit_plan',
    partialFailure: executeOptions.privileged === true,
    result: { accepted: true },
    status: 'completed' as const,
  }));

  await assert.rejects(fixture.controller.approve(editAction?.id as string), /No se pudo aplicar/);

  const snapshot = fixture.controller.snapshot();
  assert.equal(snapshot.pendingActions.find((item) => item.id === renderAction?.id)?.status, 'expired');
  assert.equal(snapshot.messages.some((message) => /brief creativo anterior quedó invalidado/i.test(message.content)), true);
});

test('keeps an approved brief recoverable when automatic continuation cannot start', async (t) => {
  const fixture = await controllerFixture(t);
  await fixture.controller.status();
  fixture.appServer.startTurnImplementation = async (turnID) => {
    if (turnID === 'turn-2') throw new Error('turn start failed');
    return { id: turnID, status: 'inProgress' };
  };
  await fixture.controller.send('Prepara un reel.', {
    jobId: 'job-123', kind: 'demo', label: 'Demo actual', pathname: '/matches/job-123',
  });
  const options = fixture.options();
  const result = await options.onDynamicToolCall?.(dynamicCall(
    'creative_brief',
    creativeBriefArguments(),
  ), new AbortController().signal);
  assert.equal(result?.success, true);
  completeTurn(options, 'Confirma el brief.', 'turn-1');

  const briefID = fixture.controller.snapshot().pendingActions[0]?.id;
  assert.notEqual(briefID, undefined);
  await fixture.controller.approve(briefID as string);

  const recovered = fixture.controller.snapshot();
  assert.equal(recovered.busy, false);
  assert.equal(recovered.pendingActions[0]?.status, 'completed');
  assert.match(recovered.messages.at(-1)?.content ?? '', /brief sigue aprobado.*prepara la acción aprobada/i);
});

test('keeps a completed operation recoverable when its automatic next turn cannot start', async (t) => {
  const fixture = await controllerFixture(t);
  await fixture.controller.status();
  fixture.appServer.startTurnImplementation = async (turnID) => {
    if (turnID === 'turn-2') throw new Error('turn start failed');
    return { id: turnID, status: 'inProgress' };
  };
  await fixture.controller.send('Selecciona el jugador de esta demo.', {
    jobId: 'job-123', kind: 'demo', label: 'Demo actual', pathname: '/matches/job-123',
  });
  const options = fixture.options();
  const result = await options.onDynamicToolCall?.(dynamicCall('preview', {
    arguments: { job_id: 'job-123', target_steamid: '76561198000000000' },
    operation: 'jobs.parse',
  }), new AbortController().signal);
  assert.equal(result?.success, true);
  completeTurn(options, 'La selección está preparada.', 'turn-1');

  const action = fixture.controller.snapshot().pendingActions.find((item) => item.operation === 'jobs.parse');
  await fixture.controller.approve(action?.id as string);

  const recovered = fixture.controller.snapshot();
  assert.equal(recovered.busy, false);
  assert.equal(recovered.pendingActions.find((item) => item.operation === 'jobs.parse')?.status, 'completed');
  assert.match(recovered.messages.at(-1)?.content ?? '', /completó jobs\.parse.*continúa el flujo/i);
});

test('exposes every operation needed for complete demo and stream journeys', async (t) => {
  const fixture = await controllerFixture(t);
  await fixture.controller.status();
  await fixture.controller.send('Busca operaciones del agente.', {
    kind: 'none', label: 'Studio', pathname: '/settings',
  });
  const options = fixture.options();
  const requiredOperations = [
    'jobs.list', 'jobs.get', 'jobs.roster', 'jobs.parse', 'jobs.plan', 'jobs.moments', 'jobs.record', 'jobs.generate',
    'renders.get', 'renders.quality', 'renders.publish', 'artifacts.get_url',
    'streams.list', 'streams.create_from_url', 'streams.get', 'streams.get_edit_plan', 'streams.resume_initialization',
    'streams.update_edit_plan', 'streams.configure_captions', 'streams.start_caption_candidates',
    'streams.get_caption_candidates', 'streams.review_caption_candidates', 'streams.edit_clip',
    'streams.start_killfeed_analysis', 'streams.get_killfeed_analysis', 'streams.apply_killfeed_analysis',
    'streams.read_killfeed', 'streams.start_render', 'streams.get_render', 'artifacts.get_stream_url',
  ];
  for (const operation of requiredOperations) {
    const result = await options.onDynamicToolCall?.(dynamicCall('search', {
      include_dynamic_inputs: false,
      operation,
    }), new AbortController().signal);
    assert.equal(result?.success, true, `${operation} should be available`);
    assert.match(result?.contentItems[0]?.text ?? '', new RegExp(operation.replace('.', '\\.')));
  }

  const namespace = fixture.appServer.startThreadOptions?.dynamicTools?.find((tool) => tool.name === 'fragforge');
  if (namespace === undefined || namespace.type !== 'namespace') throw new Error('expected fragforge namespace');
  assert.equal(namespace?.tools.some((tool) => tool.name === 'select_local_media'), true);
  assert.equal(namespace?.tools.some((tool) => tool.name === 'watch_state'), true);

  const localFile = await options.onDynamicToolCall?.(dynamicCall('search', {
    include_dynamic_inputs: false,
    operation: 'jobs.create',
  }), new AbortController().signal);
  assert.equal(localFile?.success, false);
  completeTurn(options);
});

test('wakes the agent automatically when a watched background job changes state', async (t) => {
  const fixture = await controllerFixture(t, { stateWatchIntervalMs: 5, stateWatchTimeoutMs: 1_000 });
  let reads = 0;
  fixture.setExecuteImplementation(async (request) => {
    reads += 1;
    return {
      arguments: request.arguments ?? {},
      kind: 'executed' as const,
      operation: request.operation ?? 'jobs.get',
      partialFailure: false,
      result: { id: 'job-123', state: reads === 1 ? 'scanning' : 'parsed' },
      status: 'completed' as const,
    };
  });
  await fixture.controller.status();
  await fixture.controller.send('Vigila esta demo y sigue cuando termine de analizarse.', {
    jobId: 'job-123', kind: 'demo', label: 'Demo actual', pathname: '/matches/job-123',
  });
  const options = fixture.options();
  const watching = await options.onDynamicToolCall?.(dynamicCall('watch_state', {
    arguments: { job_id: 'job-123' },
    operation: 'jobs.get',
  }), new AbortController().signal);
  assert.equal(watching?.success, true);
  assert.match(watching?.contentItems[0]?.text ?? '', /"state":"scanning"/);
  completeTurn(options, 'Seguiré automáticamente cuando cambie.', 'turn-1');

  await new Promise<void>((resolve) => setTimeout(resolve, 25));
  await waitFor(() => fixture.appServer.turnCount === 2);

  const snapshot = fixture.controller.snapshot();
  assert.equal(snapshot.busy, true);
  assert.equal(snapshot.messages.filter((message) => message.role === 'user').length, 1);
  assert.equal(snapshot.messages.some((message) => /cambió de scanning a parsed/i.test(message.content)), true);
  assert.match(fixture.appServer.turnText ?? '', /changed from scanning to parsed/i);
  assert.match(fixture.appServer.turnText ?? '', /Current demo job: job-123/);
  completeTurn(options, 'Seleccionaré momentos y prepararé el brief.', 'turn-2');
});

test('ignores a late watched-state result after the conversation clears it', async (t) => {
  const fixture = await controllerFixture(t, { stateWatchIntervalMs: 5, stateWatchTimeoutMs: 1_000 });
  let reads = 0;
  let resolveLateRead: ((value: unknown) => void) | undefined;
  fixture.setExecuteImplementation(async (request) => {
    reads += 1;
    if (reads > 1) {
      return new Promise((resolve) => {
        resolveLateRead = resolve;
      });
    }
    return {
      arguments: request.arguments ?? {},
      kind: 'executed' as const,
      operation: 'jobs.get',
      partialFailure: false,
      result: { id: 'job-123', state: 'scanning' },
      status: 'completed' as const,
    };
  });
  await fixture.controller.status();
  await fixture.controller.send('Vigila esta demo.', {
    jobId: 'job-123', kind: 'demo', label: 'Demo actual', pathname: '/matches/job-123',
  });
  const options = fixture.options();
  const watching = await options.onDynamicToolCall?.(dynamicCall('watch_state', {
    arguments: { job_id: 'job-123' },
    operation: 'jobs.get',
  }), new AbortController().signal);
  assert.equal(watching?.success, true);
  completeTurn(options, 'Vigilancia activa.', 'turn-1');
  await new Promise<void>((resolve) => setTimeout(resolve, 15));
  await waitFor(() => resolveLateRead !== undefined);

  await fixture.controller.newConversation();
  resolveLateRead?.({
    arguments: { job_id: 'job-123' },
    kind: 'executed',
    operation: 'jobs.get',
    partialFailure: false,
    result: { id: 'job-123', state: 'parsed' },
    status: 'completed',
  });
  await new Promise<void>((resolve) => setTimeout(resolve, 20));

  const snapshot = fixture.controller.snapshot();
  assert.equal(snapshot.busy, false);
  assert.equal(snapshot.messages.length, 0);
  assert.equal(fixture.appServer.turnCount, 1);
});

test('expires and aborts a watched-state read that remains in flight', async (t) => {
  const fixture = await controllerFixture(t, { stateWatchIntervalMs: 5, stateWatchTimeoutMs: 25 });
  let reads = 0;
  let pollSignal: AbortSignal | undefined;
  fixture.setExecuteImplementation(async (request, options) => {
    reads += 1;
    if (reads > 1) {
      pollSignal = options.signal;
      return new Promise((_resolve, reject) => {
        options.signal?.addEventListener('abort', () => reject(new Error('watch aborted')), { once: true });
      });
    }
    return {
      arguments: request.arguments ?? {},
      kind: 'executed' as const,
      operation: 'jobs.get',
      partialFailure: false,
      result: { id: 'job-123', state: 'recording' },
      status: 'completed' as const,
    };
  });
  await fixture.controller.status();
  await fixture.controller.send('Vigila esta grabación.', {
    jobId: 'job-123', kind: 'demo', label: 'Demo actual', pathname: '/matches/job-123',
  });
  const options = fixture.options();
  const watching = await options.onDynamicToolCall?.(dynamicCall('watch_state', {
    arguments: { job_id: 'job-123' },
    operation: 'jobs.get',
  }), new AbortController().signal);
  assert.equal(watching?.success, true);
  completeTurn(options, 'Vigilancia activa.', 'turn-1');
  await new Promise<void>((resolve) => setTimeout(resolve, 15));
  await waitFor(() => pollSignal !== undefined);
  await new Promise<void>((resolve) => setTimeout(resolve, 30));

  assert.equal(pollSignal?.aborted, true);
  const snapshot = fixture.controller.snapshot();
  assert.equal(snapshot.busy, false);
  assert.equal(snapshot.messages.some((message) => /dejó de vigilar jobs\.get/i.test(message.content)), true);
  assert.equal(fixture.appServer.turnCount, 1);
});

test('caps simultaneous background watches per conversation', async (t) => {
  const fixture = await controllerFixture(t, { stateWatchIntervalMs: 1_000, stateWatchTimeoutMs: 2_000 });
  fixture.setExecuteImplementation(async (request) => ({
    arguments: request.arguments ?? {},
    kind: 'executed' as const,
    operation: 'jobs.get',
    partialFailure: false,
    result: { state: 'scanning' },
    status: 'completed' as const,
  }));
  await fixture.controller.status();
  await fixture.controller.send('Vigila estos trabajos.', {
    kind: 'none', label: 'Studio', pathname: '/matches',
  });
  const options = fixture.options();
  for (let index = 1; index <= 4; index += 1) {
    const result = await options.onDynamicToolCall?.(dynamicCall('watch_state', {
      arguments: { job_id: `job-${index}` },
      operation: 'jobs.get',
    }), new AbortController().signal);
    assert.equal(result?.success, true);
  }
  const overflow = await options.onDynamicToolCall?.(dynamicCall('watch_state', {
    arguments: { job_id: 'job-5' },
    operation: 'jobs.get',
  }), new AbortController().signal);
  assert.equal(overflow?.success, false);
  assert.match(overflow?.contentItems[0]?.text ?? '', /4 active state watches/i);
  completeTurn(options, 'Vigilancias registradas.', 'turn-1');
  await fixture.controller.newConversation();
});

test('deduplicates equivalent state watches regardless of argument insertion order', async (t) => {
  const fixture = await controllerFixture(t, { stateWatchIntervalMs: 1_000, stateWatchTimeoutMs: 2_000 });
  fixture.setExecuteImplementation(async (request) => ({
    arguments: request.arguments ?? {},
    kind: 'executed' as const,
    operation: request.operation ?? 'streams.get_render',
    partialFailure: false,
    result: { state: 'rendering' },
    status: 'completed' as const,
  }));
  await fixture.controller.status();
  await fixture.controller.send('Vigila estos renders.', {
    kind: 'stream', label: 'Streams', pathname: '/streams', streamJobId: 'stream-1',
  });
  const options = fixture.options();
  for (const argumentsValue of [
    { stream_job_id: 'stream-1', variant: 'streamer-vertical-stack-40-60' },
    { variant: 'streamer-vertical-stack-40-60', stream_job_id: 'stream-1' },
    { stream_job_id: 'stream-2', variant: 'streamer-vertical-stack-40-60' },
    { stream_job_id: 'stream-3', variant: 'streamer-vertical-stack-40-60' },
    { stream_job_id: 'stream-4', variant: 'streamer-vertical-stack-40-60' },
  ]) {
    const result = await options.onDynamicToolCall?.(dynamicCall('watch_state', {
      arguments: argumentsValue,
      operation: 'streams.get_render',
    }), new AbortController().signal);
    assert.equal(result?.success, true);
  }
  const overflow = await options.onDynamicToolCall?.(dynamicCall('watch_state', {
    arguments: { stream_job_id: 'stream-5', variant: 'streamer-vertical-stack-40-60' },
    operation: 'streams.get_render',
  }), new AbortController().signal);
  assert.equal(overflow?.success, false);
  assert.match(overflow?.contentItems[0]?.text ?? '', /4 active state watches/i);
  completeTurn(options, 'Vigilancias registradas.', 'turn-1');
  await fixture.controller.newConversation();
});

test('imports a local demo through the native picker without exposing its path and continues with the new job', async (t) => {
  const selectedKinds: string[] = [];
  const privatePath = 'C:\\Users\\creator\\Downloads\\private-match.dem';
  const fixture = await controllerFixture(t, {
    selectLocalMedia: async (kind) => {
      selectedKinds.push(kind);
      return privatePath;
    },
  });
  fixture.setExecuteImplementation(async (request, options) => {
    if (options.privileged) {
      return {
        arguments: request.arguments ?? {},
        kind: 'executed' as const,
        operation: 'jobs.create',
        partialFailure: false,
        result: { demo_path: privatePath, id: 'job-new', state: 'scanning' },
        status: 'completed' as const,
      };
    }
    return {
      arguments: request.arguments ?? {},
      kind: 'preview' as const,
      operation: 'jobs.create',
      preview: { file: privatePath, method: 'POST', path: '/api/jobs' },
      requiresConfirmation: true as const,
      risk: 'write' as const,
    };
  });
  await fixture.controller.status();
  await fixture.controller.send('Sube una demo y encárgate del proceso.', {
    kind: 'demo', label: 'Nueva demo', pathname: '/upload',
  });
  const options = fixture.options();

  const selected = await options.onDynamicToolCall?.(dynamicCall('select_local_media', {
    kind: 'demo',
    target_steamid: '76561198000000000',
  }), new AbortController().signal);
  assert.equal(selected?.success, true);
  assert.deepEqual(selectedKinds, ['demo']);
  assert.doesNotMatch(selected?.contentItems[0]?.text ?? '', /private-match|Users|Downloads/i);
  const action = fixture.controller.snapshot().pendingActions.find((item) => item.operation === 'jobs.create');
  assert.notEqual(action, undefined);
  assert.equal(action?.preview?.fields?.some((field) => field.label === 'Archivo' && field.value === 'private-match.dem'), true);
  assert.doesNotMatch(JSON.stringify(action), /Users|Downloads/i);
  completeTurn(options, 'La subida está preparada.', 'turn-1');

  await fixture.controller.approve(action?.id as string);

  assert.deepEqual(fixture.calls.map((call) => [call.operation, call.privileged]), [
    ['jobs.create', undefined],
    ['jobs.create', true],
  ]);
  assert.deepEqual(fixture.calls.at(-1)?.arguments, {
    demo_path: privatePath,
    target_steamid: '76561198000000000',
  });
  assert.equal(fixture.controller.snapshot().busy, true);
  assert.match(fixture.appServer.turnText ?? '', /Current demo job: job-new/);
  assert.match(fixture.appServer.turnText ?? '', /"id":"job-new"/);
  assert.doesNotMatch(fixture.appServer.turnText ?? '', /private-match|Users|Downloads/i);
  completeTurn(options, 'Voy a comprobar el parseo y seguir.', 'turn-2');
});

test('imports a local stream recording through the native picker and continues with its ready edit plan', async (t) => {
  const privatePath = 'D:\\captures\\private-stream.mp4';
  const fixture = await controllerFixture(t, {
    selectLocalMedia: async (kind) => kind === 'stream' ? privatePath : null,
  });
  fixture.setExecuteImplementation(async (request, options) => {
    if (options.privileged) {
      return {
        arguments: request.arguments ?? {},
        kind: 'executed' as const,
        operation: 'streams.create_from_file',
        partialFailure: false,
        result: {
          job: { id: 'stream-new', state: 'ready' },
          media_path: privatePath,
        },
        status: 'completed' as const,
      };
    }
    return {
      arguments: request.arguments ?? {},
      kind: 'preview' as const,
      operation: 'streams.create_from_file',
      preview: { file: privatePath },
      requiresConfirmation: true as const,
      risk: 'write' as const,
    };
  });
  await fixture.controller.status();
  await fixture.controller.send('Sube este clip de stream y edítalo.', {
    kind: 'none', label: 'Clips de stream', pathname: '/streams',
  });
  const options = fixture.options();

  const selected = await options.onDynamicToolCall?.(dynamicCall('select_local_media', {
    kind: 'stream',
    title: 'Ronda decisiva',
  }), new AbortController().signal);
  assert.equal(selected?.success, true);
  assert.doesNotMatch(selected?.contentItems[0]?.text ?? '', /private-stream|captures/i);
  const action = fixture.controller.snapshot().pendingActions.find((item) => item.operation === 'streams.create_from_file');
  assert.notEqual(action, undefined);
  assert.equal(action?.preview?.fields?.some((field) => field.label === 'Archivo' && field.value === 'private-stream.mp4'), true);
  assert.doesNotMatch(JSON.stringify(action), /captures/i);
  completeTurn(options, 'La subida del stream está preparada.', 'turn-1');

  await fixture.controller.approve(action?.id as string);

  assert.deepEqual(fixture.calls.at(-1)?.arguments, { title: 'Ronda decisiva', video_path: privatePath });
  assert.match(fixture.appServer.turnText ?? '', /Current stream job: stream-new/);
  assert.match(fixture.appServer.turnText ?? '', /"id":"stream-new"/);
  assert.doesNotMatch(fixture.appServer.turnText ?? '', /private-stream|captures/i);
  completeTurn(options, 'Leeré el plan y prepararé la edición.', 'turn-2');
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
