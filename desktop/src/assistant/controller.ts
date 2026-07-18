import { randomUUID } from 'node:crypto';
import {
  type AssistantAction,
  type AssistantActionPreview,
  type AssistantContext,
  type AssistantEvent,
  type AssistantMessage,
  type AssistantSnapshot,
} from '../assistant-ipc.ts';
import { searchOperationCatalog, parseSearchRequest } from '../mcp/discovery.ts';
import { isJsonObject, type JsonObject, type JsonValue } from '../mcp/json.ts';
import {
  McpOperationGateway,
  type McpOperationGatewayOutcome,
} from '../mcp/operation-gateway.ts';
import { operationNamed } from '../mcp/operations.ts';
import { OrchestratorClient } from '../mcp/orchestrator-client.ts';
import {
  createCodexAppServer,
  type AppServerAgentMessageDelta,
  type AppServerDynamicTool,
  type AppServerDynamicToolCall,
  type AppServerDynamicToolResult,
  type AppServerStartThreadOptions,
  type AppServerTurnCompletedEvent,
  type CodexAppServer,
  type CodexAppServerClientOptions,
  type CodexAppServerFactory,
} from './app-server-client.ts';
import { AssistantHistoryStore } from './history.ts';

const ACTION_EXPIRY_MS = 15 * 60_000;
const INITIALIZE_TIMEOUT_MS = 15_000;
const MAX_ACTION_CARDS = 16;
const MAX_MESSAGE_CONTENT_LENGTH = 12_000;
const MAX_MESSAGES = 200;
const MAX_MODEL_VALUE_LENGTH = 12_000;
const MAX_MODEL_VALUE_DEPTH = 8;
const MAX_MODEL_VALUE_ITEMS = 100;
const MAX_REVIEWABLE_ACTION_ARRAY_ITEMS = 32;
const MAX_REVIEWABLE_ACTION_FIELDS = 48;
const MAX_REVIEWABLE_ACTION_STRING_LENGTH = 240;

const ASSISTANT_OPERATIONS = new Set([
  'studio.status',
  'studio.metrics',
  'catalog.presets',
  'catalog.loadouts',
  'catalog.songs',
  'catalog.stream_variants',
  'jobs.list',
  'jobs.get',
  'jobs.roster',
  'jobs.parse',
  'jobs.plan',
  'jobs.moments',
  'jobs.record',
  'jobs.generate',
  'jobs.compose',
  'jobs.delete',
  'renders.get',
  'renders.start',
  'renders.publish',
  'renders.quality',
  'renders.start_caption_agent',
  'renders.caption_candidates',
  'renders.publish_assistant',
  'renders.delete_video',
  'streams.list',
  'streams.get',
  'streams.get_edit_plan',
  'streams.resume_initialization',
  'streams.update_edit_plan',
  'streams.configure_captions',
  'streams.edit_clip',
  'streams.start_render',
  'streams.get_render',
  'streams.list_killfeed_weapons',
  'streams.read_killfeed',
  'streams.preview_killfeed_notice',
]);

const DYNAMIC_TOOLS: AppServerDynamicTool[] = [{
  description: 'Allowed local FragForge Studio operations. Search first. Read operations are safe; every change is only prepared as a preview and must be approved in Studio.',
  name: 'fragforge',
  tools: [
    {
      description: 'Search the safe FragForge Studio operation catalog and live IDs/options. Always call this before asking to read or preview an operation.',
      inputSchema: {
        additionalProperties: false,
        properties: {
          arguments: { additionalProperties: true, type: 'object' },
          category: { enum: ['artifacts', 'catalog', 'jobs', 'renders', 'streams', 'studio'], type: 'string' },
          include_dynamic_inputs: { type: 'boolean' },
          limit: { maximum: 20, minimum: 1, type: 'integer' },
          operation: { type: 'string' },
          query: { type: 'string' },
          risk: { enum: ['read', 'write', 'costly', 'destructive'], type: 'string' },
        },
        type: 'object',
      },
      name: 'search',
      type: 'function',
    },
    {
      description: 'Run one exact read-only FragForge operation returned by search. It cannot change Studio.',
      inputSchema: operationToolSchema(),
      name: 'read',
      type: 'function',
    },
    {
      description: 'Prepare one exact write, costly, or destructive FragForge operation returned by search. This only creates a Studio approval card; it never executes the operation.',
      inputSchema: operationToolSchema(),
      name: 'preview',
      type: 'function',
    },
    {
      description: 'Create a complete creative-brief confirmation card before one capture or render operation. Studio will not preview that costly operation until the user approves this exact brief card.',
      inputSchema: creativeBriefSchema(),
      name: 'creative_brief',
      type: 'function',
    },
  ],
  type: 'namespace',
}];

const DEVELOPER_INSTRUCTIONS = `You are the integrated FragForge Studio assistant. You may use only the fragforge dynamic tools for Studio data and actions. They are not shell, filesystem, browser, URL, or generic MCP tools. Never request, inspect, or repeat local file paths, raw media, URLs, credentials, tokens, or secrets; direct media intake to Studio's existing upload flow.

Always search the catalog before choosing an exact operation and use only IDs/options returned by the tool. Use read only for read-only operations. Preview only prepares a change for an exact Studio approval card; it never executes it. Never claim that a change ran unless Studio later reports completion. Never attempt a tool or workaround outside fragforge.

Before previewing any capture or render action (including jobs.record, jobs.generate, renders.start, or streams.start_render), collect every unanswered creative-brief choice: format/aspect, HUD/killfeed treatment, effect/transition, numbering or counter, intro/outro, music, and cover strategy. Then call creative_brief with all choices. The user must explicitly approve its Studio card before you can preview the costly operation. Generic words such as “go”, “dale”, “ok”, or “hazlo” are not creative approval unless that approved brief is already visible in the conversation. The later Studio approval card is the separate final approval for the exact operation.

Be concise, answer in the user's language, and explain unavailable capabilities honestly.`;

interface PendingMcpAction {
  arguments: JsonObject;
  card: AssistantAction;
  expiryTimer: NodeJS.Timeout;
  kind: 'mcp';
  operation: string;
}

interface PendingCreativeBrief {
  brief: CreativeBrief;
  card: AssistantAction;
  expiryTimer: NodeJS.Timeout;
  kind: 'creative-brief';
}

type PendingAction = PendingMcpAction | PendingCreativeBrief;

interface CreativeBrief {
  cover: CreativeBriefCover;
  counter: CreativeBriefCounter;
  effect: CreativeBriefEffect;
  format: CreativeBriefFormat;
  hud: CreativeBriefHUD;
  intro: CreativeBriefIntro;
  killfeed: CreativeBriefKillfeed;
  music: CreativeBriefMusic;
  operation: CreativeOperation;
  outro: CreativeBriefOutro;
  targetID: string;
  targetKind: 'job' | 'stream-job';
  transition: CreativeBriefTransition;
}

interface ApprovedCreativeBrief extends CreativeBrief {
  expiresAt: number;
}

interface CreativeBriefRequirement {
  format?: CreativeBriefFormat;
  operation: CreativeOperation;
  targetID: string;
  targetKind: 'job' | 'stream-job';
}

type CreativeOperation = 'jobs.generate' | 'jobs.record' | 'renders.start' | 'streams.start_render';
type CreativeBriefFormat = 'landscape-16x9' | 'short-9x16';
type CreativeBriefHUD = 'clean-hudless' | 'full-game-ui';
type CreativeBriefKillfeed = 'preserve' | 'synthetic';
type CreativeBriefEffect = 'clean' | 'freeze-flash' | 'punch-in' | 'velocity';
type CreativeBriefTransition = 'cut' | 'dip' | 'flash' | 'whip';
type CreativeBriefCounter = 'off' | 'on';
type CreativeBriefIntro = 'hook' | 'none';
type CreativeBriefOutro = 'loop' | 'none';
type CreativeBriefMusic = 'none' | 'selected';
type CreativeBriefCover = 'generated-gameplay-candidates' | 'no-cover';

export interface AssistantControllerOptions {
  /** A dedicated empty directory; Codex never receives the Studio repository or user-data directory as its cwd. */
  cwd: string;
  gateway: McpOperationGateway;
  history: AssistantHistoryStore;
  onEvent?: (event: AssistantEvent) => void;
  orchestratorClient: OrchestratorClient;
  /** Non-sensitive diagnostic sink (usually the desktop studio log). */
  log?: (message: string) => void;
  createAppServer?: CodexAppServerFactory;
  appServerOptions?: Omit<CodexAppServerClientOptions,
    'clientInfo' | 'cwd' | 'dynamicTools' | 'onAgentMessageDelta' | 'onDiagnostic' | 'onDynamicToolCall' | 'onError' | 'onStatus' | 'onTurnCompleted'>;
  version?: string;
}

/**
 * Owns a single local Codex app-server thread and the narrow bridge between
 * it and Studio's allowlisted MCP operation gateway. Renderer input never
 * becomes app-server JSON-RPC or a child-process command.
 */
export class AssistantController {
  readonly #appServerOptions: AssistantControllerOptions['appServerOptions'];
  readonly #createAppServer: CodexAppServerFactory;
  readonly #cwd: string;
  readonly #gateway: McpOperationGateway;
  readonly #history: AssistantHistoryStore;
  readonly #log: ((message: string) => void) | undefined;
  readonly #onEvent: ((event: AssistantEvent) => void) | undefined;
  readonly #orchestratorClient: OrchestratorClient;
  readonly #version: string;
  readonly #pendingActions = new Map<string, PendingAction>();
  readonly #actionCards: AssistantAction[] = [];
  readonly #approvalControllers = new Set<AbortController>();
  readonly #approvedBriefs = new Map<string, ApprovedCreativeBrief>();
  readonly #stateNotes: string[] = [];
  #appServer: CodexAppServer | null = null;
  #availability: AssistantSnapshot['availability'] = 'starting';
  #busy = false;
  #closed = false;
  #error: string | undefined;
  #historyLoaded = false;
  #initializing: Promise<void> | null = null;
  #messages: AssistantMessage[] = [];
  #activeMessageID: string | null = null;
  #activeTurnID: string | null = null;
  #threadID: string | undefined;

  constructor(options: AssistantControllerOptions) {
    this.#appServerOptions = options.appServerOptions;
    this.#createAppServer = options.createAppServer ?? createCodexAppServer;
    this.#cwd = options.cwd;
    this.#gateway = options.gateway;
    this.#history = options.history;
    this.#log = options.log;
    this.#onEvent = options.onEvent;
    this.#orchestratorClient = options.orchestratorClient;
    this.#version = options.version ?? '0.0.0';
  }

  async status(): Promise<AssistantSnapshot> {
    await this.#ensureReady();
    return this.snapshot();
  }

  snapshot(): AssistantSnapshot {
    this.#expireActions();
    return {
      availability: this.#availability,
      busy: this.#busy,
      ...(this.#error === undefined ? {} : { error: this.#error }),
      messages: this.#messages.map((message) => ({ ...message })),
      pendingActions: this.#actionCards.map((action) => ({
        ...action,
        ...(action.preview === undefined ? {} : {
          preview: {
            ...action.preview,
            ...(action.preview.fields === undefined ? {} : { fields: action.preview.fields.map((field) => ({ ...field })) }),
          },
        }),
      })),
      ...(this.#threadID === undefined ? {} : { threadId: this.#threadID }),
    };
  }

  async send(message: string, context: AssistantContext): Promise<void> {
    await this.#ensureReady();
    if (this.#availability !== 'ready' || this.#appServer === null) {
      throw new Error('Codex is not available');
    }
    if (this.#busy) throw new Error('a Codex turn is already running');

    const userMessage = this.#appendMessage({ content: message, role: 'user' });
    const assistantMessage = this.#appendMessage({ content: '', role: 'assistant', streaming: true });
    this.#activeMessageID = assistantMessage.id;
    this.#activeTurnID = null;
    this.#busy = true;
    this.#publish();
    void this.#saveHistory();

    try {
      const threadID = await this.#ensureThread();
      const turn = await this.#appServer.startTurn(threadID, turnPrompt(message, context, this.#stateNotes), {
        clientUserMessageId: userMessage.id,
        cwd: this.#cwd,
      });
      if (this.#busy) this.#activeTurnID = turn.id;
    } catch (error) {
      this.#finishTurnWithError(error);
      throw new Error('No se pudo enviar el mensaje a Codex.');
    }
  }

  async cancel(): Promise<void> {
    if (!this.#busy || this.#appServer === null || this.#threadID === undefined || this.#activeTurnID === null) return;
    try {
      await this.#appServer.interruptTurn(this.#threadID, this.#activeTurnID);
    } catch {
      throw new Error('No se pudo cancelar el turno de Codex.');
    }
  }

  async approve(actionID: string): Promise<void> {
    this.#expireActions();
    if (this.#busy) throw new Error('wait for the active Codex turn before approving an action');
    const pending = this.#pendingActions.get(actionID);
    if (pending === undefined) throw new Error('this action is no longer available for approval');

    // Remove executable state before any await so duplicate IPC calls cannot
    // run the mutation twice. The card remains as a non-interactive audit row.
    this.#pendingActions.delete(actionID);
    clearTimeout(pending.expiryTimer);
    pending.card = { ...pending.card, status: 'approved' };
    this.#replaceActionCard(pending.card);
    this.#publish();

    if (pending.kind === 'creative-brief') {
      this.#approvedBriefs.set(creativeBriefKey(pending.brief), {
        ...pending.brief,
        expiresAt: Date.now() + ACTION_EXPIRY_MS,
      });
      this.#replaceActionCard({ ...pending.card, status: 'completed' });
      const summary = `Studio registró el brief creativo aprobado para ${pending.brief.operation}. Ya puedes preparar esa acción exacta.`;
      this.#addStateNote(summary);
      this.#appendMessage({ content: summary, role: 'system' });
      this.#publish();
      void this.#saveHistory();
      return;
    }

    const controller = new AbortController();
    this.#approvalControllers.add(controller);
    try {
      const outcome = await this.#gateway.execute(
        { arguments: pending.arguments, operation: pending.operation },
        { privileged: true, signal: controller.signal },
      );
      if (outcome.kind !== 'executed') throw new Error('approved action did not execute');
      const succeeded = !outcome.partialFailure;
      const status = succeeded ? 'completed' : 'failed';
      this.#replaceActionCard({ ...pending.card, status });
      const summary = succeeded
        ? `Studio completó ${pending.operation}.`
        : `Studio no completó ${pending.operation}; revisa el estado actual antes de reintentar.`;
      this.#addStateNote(summary);
      this.#appendMessage({ content: summary, role: 'system' });
      this.#publish();
      void this.#saveHistory();
      if (!succeeded) throw new Error('the action produced a partial result');
    } catch {
      this.#replaceActionCard({ ...pending.card, status: 'failed' });
      const summary = `Studio no pudo completar ${pending.operation}. La acción no se volverá a ejecutar automáticamente.`;
      this.#addStateNote(summary);
      this.#appendMessage({ content: summary, role: 'system' });
      this.#publish();
      void this.#saveHistory();
      throw new Error('No se pudo aplicar la acción aprobada.');
    } finally {
      this.#approvalControllers.delete(controller);
    }
  }

  reject(actionID: string): void {
    this.#expireActions();
    const pending = this.#pendingActions.get(actionID);
    if (pending === undefined) throw new Error('this action is no longer available');
    this.#pendingActions.delete(actionID);
    clearTimeout(pending.expiryTimer);
    this.#replaceActionCard({ ...pending.card, status: 'rejected' });
    this.#publish();
  }

  async newConversation(): Promise<void> {
    if (this.#busy) throw new Error('wait for the active Codex turn before starting a new conversation');
    this.#threadID = undefined;
    this.#messages = [];
    this.#stateNotes.length = 0;
    this.#approvedBriefs.clear();
    this.#clearActions();
    await this.#history.save({ messages: [] });
    this.#publish();
  }

  async clearHistory(): Promise<void> {
    if (this.#busy) throw new Error('wait for the active Codex turn before clearing history');
    this.#messages = [];
    await this.#history.clear();
    this.#publish();
  }

  close(): void {
    if (this.#closed) return;
    this.#closed = true;
    for (const controller of this.#approvalControllers) controller.abort();
    this.#approvalControllers.clear();
    this.#approvedBriefs.clear();
    this.#clearActions();
    this.#appServer?.close();
    this.#appServer = null;
  }

  async #ensureReady(): Promise<void> {
    if (this.#closed || this.#availability === 'ready') return;
    if (this.#initializing !== null) return this.#initializing;
    this.#availability = 'starting';
    this.#error = undefined;
    this.#publish();
    this.#initializing = this.#start().finally(() => {
      this.#initializing = null;
    });
    return this.#initializing;
  }

  async #start(): Promise<void> {
    try {
      await this.#loadHistory();
      const appServer = this.#createAppServer({
        ...this.#appServerOptions,
        args: safeAppServerArgs(this.#appServerOptions?.args),
        clientInfo: { name: 'fragforge_studio', title: 'FragForge Studio', version: this.#version },
        cwd: this.#cwd,
        dynamicTools: DYNAMIC_TOOLS,
        env: appServerEnvironment(this.#appServerOptions?.env),
        onAgentMessageDelta: (delta) => this.#handleAgentDelta(delta),
        onDiagnostic: (message) => this.#writeDiagnostic(message),
        onDynamicToolCall: (call) => this.#handleDynamicToolCall(call),
        onError: () => this.#handleAppServerFailure(),
        onStatus: (status) => {
          if (status === 'failed' || (status === 'closed' && !this.#closed)) this.#handleAppServerFailure();
        },
        onTurnCompleted: (event) => this.#handleTurnCompleted(event),
      });
      this.#appServer = appServer;
      await withTimeout(appServer.initialize(), INITIALIZE_TIMEOUT_MS, 'Codex tardó demasiado en responder.');
      if (this.#closed) return;
      this.#availability = 'ready';
      this.#error = undefined;
      this.#publish();
    } catch (error) {
      if (this.#closed) return;
      this.#writeDiagnostic(`startup failed: ${errorMessage(error)}`);
      this.#appServer?.close();
      this.#appServer = null;
      this.#availability = 'unavailable';
      this.#error = 'No se pudo iniciar Codex local. Comprueba que Codex esté instalado, actualizado e iniciado sesión.';
      this.#publish();
    }
  }

  async #loadHistory(): Promise<void> {
    if (this.#historyLoaded) return;
    const history = await this.#history.load();
    this.#messages = history.messages.map((message) => ({ ...message, streaming: false }));
    this.#threadID = history.threadId;
    this.#historyLoaded = true;
  }

  async #ensureThread(): Promise<string> {
    const appServer = this.#appServer;
    if (appServer === null) throw new Error('Codex app-server is unavailable');
    const options: AppServerStartThreadOptions = {
      approvalPolicy: 'never',
      cwd: this.#cwd,
      developerInstructions: DEVELOPER_INSTRUCTIONS,
      dynamicTools: DYNAMIC_TOOLS,
      sandbox: 'read-only',
      serviceName: 'fragforge_studio',
    };
    if (this.#threadID !== undefined) {
      try {
        await appServer.resumeThread(this.#threadID, options);
        return this.#threadID;
      } catch (error) {
        this.#writeDiagnostic(`could not resume stored Codex thread: ${errorMessage(error)}`);
        this.#threadID = undefined;
        this.#appendMessage({ content: 'Studio no pudo reanudar el hilo anterior de Codex; se creó una conversación nueva.', role: 'system' });
      }
    }
    const thread = await appServer.startThread(options);
    this.#threadID = thread.id;
    void this.#saveHistory();
    return thread.id;
  }

  async #handleDynamicToolCall(call: AppServerDynamicToolCall): Promise<AppServerDynamicToolResult> {
    if (this.#closed || call.namespace !== 'fragforge' || call.threadId !== this.#threadID) {
      return toolFailure('This FragForge tool call is not valid for the active Studio conversation.');
    }
    try {
      if (call.tool === 'search') return await this.#search(call.arguments);
      if (call.tool === 'read') return await this.#read(call.arguments);
      if (call.tool === 'preview') return await this.#preview(call.arguments);
      if (call.tool === 'creative_brief') return this.#creativeBrief(call.arguments);
      return toolFailure('Unknown FragForge Studio tool.');
    } catch (error) {
      this.#writeDiagnostic(`dynamic tool ${call.tool} failed: ${errorMessage(error)}`);
      return toolFailure('FragForge Studio could not complete that safe tool request. Check the current Studio state and try again.');
    }
  }

  async #search(value: unknown): Promise<AppServerDynamicToolResult> {
    const input = parseSearchInput(value);
    const operation = input.operation;
    if (typeof operation === 'string' && !ASSISTANT_OPERATIONS.has(operation)) {
      return toolFailure('That operation is not available in the embedded assistant. Use the corresponding Studio screen instead.');
    }
    const result = await searchOperationCatalog(this.#orchestratorClient, parseSearchRequest(input));
    const operations = Array.isArray(result.operations)
      ? result.operations.filter((entry) => isJsonObject(entry) && typeof entry.name === 'string' && ASSISTANT_OPERATIONS.has(entry.name))
      : [];
    return toolSuccess({
      ...result,
      count: operations.length,
      operations,
      instructions: 'Choose only one listed operation. The embedded assistant never accepts local files, raw media, URLs, or arbitrary shell commands.',
    });
  }

  async #read(value: unknown): Promise<AppServerDynamicToolResult> {
    const request = parseOperationToolInput(value);
    const definition = allowedOperation(request.operation);
    if (definition.risk !== 'read') return toolFailure('That operation changes Studio. Use preview so Studio can request exact user approval.');
    const outcome = await this.#gateway.execute(request);
    if (outcome.kind !== 'executed') return toolFailure('The read operation could not be completed.');
    return toolSuccess(executionForModel(outcome));
  }

  async #preview(value: unknown): Promise<AppServerDynamicToolResult> {
    const request = parseOperationToolInput(value);
    const definition = allowedOperation(request.operation);
    if (definition.risk === 'read') return toolFailure('That operation is read-only. Use read instead.');
    const requiredBrief = requiredCreativeBrief(request.operation, request.arguments);
    if (requiredBrief !== null && !this.#hasApprovedBrief(requiredBrief)) {
      return toolFailure('Studio requires a complete creative brief approved by the user for this exact capture or render action. Collect the choices, call creative_brief, wait for its card approval, then preview this operation again.');
    }
    const outcome = await this.#gateway.execute(request);
    if (outcome.kind !== 'preview') return toolFailure('The operation did not produce an approval preview.');
    const cardPreview = reviewableActionPreview(outcome.arguments);
    if (cardPreview === null) {
      return toolFailure('Studio cannot show every validated detail of this request in one exact approval card. Use the corresponding Studio screen or reduce the selection before trying again.');
    }
    const card = this.#createActionCard(outcome, definition.title, definition.description, cardPreview);
    const pending: PendingAction = {
      arguments: cloneJsonObject(outcome.arguments),
      card,
      expiryTimer: setTimeout(() => this.#expireAction(card.id), ACTION_EXPIRY_MS),
      kind: 'mcp',
      operation: outcome.operation,
    };
    pending.expiryTimer.unref();
    this.#pendingActions.set(card.id, pending);
    this.#actionCards.push(card);
    this.#trimActionCards();
    this.#publish();
    return toolSuccess({
      action_id: card.id,
      ...(card.expiresAt === undefined ? {} : { expires_at: card.expiresAt }),
      operation: outcome.operation,
      preview: modelSafeValue(outcome.preview),
      requires_user_approval: true,
      risk: outcome.risk,
      status: 'pending',
    });
  }

  #creativeBrief(value: unknown): AppServerDynamicToolResult {
    const brief = parseCreativeBrief(value);
    const id = randomUUID();
    const createdAt = new Date();
    const card: AssistantAction = {
      description: `Confirma el brief creativo antes de preparar ${brief.operation}.`,
      expiresAt: new Date(createdAt.getTime() + ACTION_EXPIRY_MS).toISOString(),
      id,
      operation: 'studio.confirm_creative_brief',
      preview: creativeBriefPreview(brief),
      requiresApproval: true,
      risk: 'costly',
      status: 'pending',
      title: 'Confirmar brief creativo',
    };
    const pending: PendingCreativeBrief = {
      brief,
      card,
      expiryTimer: setTimeout(() => this.#expireAction(id), ACTION_EXPIRY_MS),
      kind: 'creative-brief',
    };
    pending.expiryTimer.unref();
    this.#pendingActions.set(id, pending);
    this.#actionCards.push(card);
    this.#trimActionCards();
    this.#publish();
    return toolSuccess({
      action_id: id,
      operation: brief.operation,
      requires_user_approval: true,
      status: 'creative_brief_pending',
      target: brief.targetID,
    });
  }

  #createActionCard(
    outcome: Extract<McpOperationGatewayOutcome, { kind: 'preview' }>,
    title: string,
    description: string,
    preview: AssistantActionPreview,
  ): AssistantAction {
    const createdAt = new Date();
    return {
      description,
      expiresAt: new Date(createdAt.getTime() + ACTION_EXPIRY_MS).toISOString(),
      id: randomUUID(),
      operation: outcome.operation,
      preview,
      requiresApproval: true,
      risk: outcome.risk,
      status: 'pending',
      title,
    };
  }

  #hasApprovedBrief(required: CreativeBriefRequirement): boolean {
    const approved = this.#approvedBriefs.get(creativeBriefKey(required));
    if (approved === undefined) return false;
    if (approved.expiresAt <= Date.now()) {
      this.#approvedBriefs.delete(creativeBriefKey(required));
      return false;
    }
    return required.format === undefined || approved.format === required.format;
  }

  #handleAgentDelta(delta: AppServerAgentMessageDelta): void {
    if (!this.#busy || delta.threadId !== this.#threadID || this.#activeMessageID === null) return;
    if (this.#activeTurnID !== null && delta.turnId !== this.#activeTurnID) return;
    const message = this.#messages.find((item) => item.id === this.#activeMessageID);
    if (message === undefined) return;
    if (message.content.length < MAX_MESSAGE_CONTENT_LENGTH) {
      message.content += delta.delta.slice(0, MAX_MESSAGE_CONTENT_LENGTH - message.content.length);
    }
    this.#publish();
  }

  #handleTurnCompleted(event: AppServerTurnCompletedEvent): void {
    if (!this.#busy || event.threadId !== this.#threadID) return;
    if (this.#activeTurnID !== null && event.turn.id !== this.#activeTurnID) return;
    const message = this.#activeMessageID === null ? undefined : this.#messages.find((item) => item.id === this.#activeMessageID);
    if (message !== undefined) message.streaming = false;
    this.#busy = false;
    this.#activeMessageID = null;
    this.#activeTurnID = null;
    this.#publish();
    void this.#saveHistory();
  }

  #finishTurnWithError(error: unknown): void {
    this.#writeDiagnostic(`turn failed: ${errorMessage(error)}`);
    const message = this.#activeMessageID === null ? undefined : this.#messages.find((item) => item.id === this.#activeMessageID);
    if (message !== undefined) {
      message.content = message.content || 'Codex no pudo completar esta respuesta.';
      message.streaming = false;
    }
    this.#busy = false;
    this.#activeMessageID = null;
    this.#activeTurnID = null;
    this.#publish();
    void this.#saveHistory();
  }

  #handleAppServerFailure(): void {
    if (this.#closed) return;
    this.#availability = 'error';
    this.#error = 'La conexión local con Codex se cerró. Abre una conversación nueva para volver a intentarlo.';
    this.#finishTurnWithError(new Error('Codex app-server closed'));
  }

  #appendMessage(input: Pick<AssistantMessage, 'content' | 'role'> & { streaming?: boolean }): AssistantMessage {
    const message: AssistantMessage = {
      content: input.content.slice(0, MAX_MESSAGE_CONTENT_LENGTH),
      createdAt: new Date().toISOString(),
      id: randomUUID(),
      role: input.role,
      ...(input.streaming === true ? { streaming: true } : {}),
    };
    this.#messages.push(message);
    if (this.#messages.length > MAX_MESSAGES) this.#messages.splice(0, this.#messages.length - MAX_MESSAGES);
    return message;
  }

  #replaceActionCard(next: AssistantAction): void {
    const index = this.#actionCards.findIndex((action) => action.id === next.id);
    if (index === -1) return;
    this.#actionCards[index] = next;
  }

  #trimActionCards(): void {
    while (this.#actionCards.length > MAX_ACTION_CARDS) {
      const index = this.#actionCards.findIndex((action) => action.status !== 'pending');
      const removed = this.#actionCards.splice(index === -1 ? 0 : index, 1)[0];
      if (removed !== undefined) {
        const pending = this.#pendingActions.get(removed.id);
        if (pending !== undefined) {
          clearTimeout(pending.expiryTimer);
          this.#pendingActions.delete(removed.id);
        }
      }
    }
  }

  #expireActions(): void {
    const now = Date.now();
    for (const [id, pending] of this.#pendingActions) {
      const expiresAt = pending.card.expiresAt === undefined ? NaN : Date.parse(pending.card.expiresAt);
      if (Number.isNaN(expiresAt) || expiresAt > now) continue;
      this.#pendingActions.delete(id);
      clearTimeout(pending.expiryTimer);
      this.#replaceActionCard({ ...pending.card, status: 'expired' });
    }
  }

  #expireAction(id: string): void {
    const pending = this.#pendingActions.get(id);
    if (pending === undefined) return;
    this.#pendingActions.delete(id);
    this.#replaceActionCard({ ...pending.card, status: 'expired' });
    this.#publish();
  }

  #clearActions(): void {
    for (const pending of this.#pendingActions.values()) clearTimeout(pending.expiryTimer);
    this.#pendingActions.clear();
    this.#actionCards.length = 0;
  }

  #addStateNote(note: string): void {
    this.#stateNotes.push(note);
    if (this.#stateNotes.length > 5) this.#stateNotes.splice(0, this.#stateNotes.length - 5);
  }

  #publish(): void {
    if (this.#closed) return;
    try {
      this.#onEvent?.({ snapshot: this.snapshot(), type: 'snapshot' });
    } catch {
      // Renderer delivery cannot change whether an action is approved or run.
    }
  }

  async #saveHistory(): Promise<void> {
    try {
      await this.#history.save({ messages: this.#messages, ...(this.#threadID === undefined ? {} : { threadId: this.#threadID }) });
    } catch (error) {
      this.#writeDiagnostic(`could not save assistant history: ${errorMessage(error)}`);
    }
  }

  #writeDiagnostic(message: string): void {
    const compact = message.replace(/[\r\n]+/g, ' ').slice(0, 1_000);
    this.#log?.(`[assistant] ${compact}\n`);
  }
}

function operationToolSchema(): JsonObject {
  return {
    additionalProperties: false,
    properties: {
      arguments: { additionalProperties: true, default: {}, type: 'object' },
      operation: { type: 'string' },
    },
    required: ['operation'],
    type: 'object',
  };
}

function creativeBriefSchema(): JsonObject {
  return {
    additionalProperties: false,
    properties: {
      counter: { enum: ['on', 'off'], type: 'string' },
      cover: { enum: ['generated-gameplay-candidates', 'no-cover'], type: 'string' },
      effect: { enum: ['clean', 'punch-in', 'velocity', 'freeze-flash'], type: 'string' },
      format: { enum: ['short-9x16', 'landscape-16x9'], type: 'string' },
      hud: { enum: ['full-game-ui', 'clean-hudless'], type: 'string' },
      intro: { enum: ['hook', 'none'], type: 'string' },
      job_id: { type: 'string' },
      killfeed: { enum: ['preserve', 'synthetic'], type: 'string' },
      music: { enum: ['none', 'selected'], type: 'string' },
      operation: { enum: ['jobs.record', 'jobs.generate', 'renders.start', 'streams.start_render'], type: 'string' },
      outro: { enum: ['loop', 'none'], type: 'string' },
      stream_job_id: { type: 'string' },
      transition: { enum: ['cut', 'flash', 'whip', 'dip'], type: 'string' },
    },
    required: ['operation', 'format', 'hud', 'killfeed', 'effect', 'transition', 'counter', 'intro', 'outro', 'music', 'cover'],
    type: 'object',
  };
}

function parseCreativeBrief(value: unknown): CreativeBrief {
  if (!isJsonObject(value)) throw new Error('creative brief must be an object');
  const allowed = new Set([
    'counter', 'cover', 'effect', 'format', 'hud', 'intro', 'job_id', 'killfeed', 'music', 'operation', 'outro', 'stream_job_id', 'transition',
  ]);
  if (Object.keys(value).some((key) => !allowed.has(key))) throw new Error('creative brief contains an unknown field');
  const operation = enumValue(value.operation, ['jobs.record', 'jobs.generate', 'renders.start', 'streams.start_render'] as const, 'operation');
  const format = enumValue(value.format, ['short-9x16', 'landscape-16x9'] as const, 'format');
  const hud = enumValue(value.hud, ['full-game-ui', 'clean-hudless'] as const, 'hud');
  const killfeed = enumValue(value.killfeed, ['preserve', 'synthetic'] as const, 'killfeed');
  const effect = enumValue(value.effect, ['clean', 'punch-in', 'velocity', 'freeze-flash'] as const, 'effect');
  const transition = enumValue(value.transition, ['cut', 'flash', 'whip', 'dip'] as const, 'transition');
  const counter = enumValue(value.counter, ['on', 'off'] as const, 'counter');
  const intro = enumValue(value.intro, ['hook', 'none'] as const, 'intro');
  const outro = enumValue(value.outro, ['loop', 'none'] as const, 'outro');
  const music = enumValue(value.music, ['none', 'selected'] as const, 'music');
  const cover = enumValue(value.cover, ['generated-gameplay-candidates', 'no-cover'] as const, 'cover');
  if (operation === 'streams.start_render') {
    if (!isSafeReference(value.stream_job_id) || value.job_id !== undefined) throw new Error('stream creative brief needs only stream_job_id');
    return {
      cover,
      counter,
      effect,
      format,
      hud,
      intro,
      killfeed,
      music,
      operation,
      outro,
      targetID: value.stream_job_id,
      targetKind: 'stream-job',
      transition,
    };
  }
  if (!isSafeReference(value.job_id) || value.stream_job_id !== undefined) throw new Error('demo creative brief needs only job_id');
  return {
    cover,
    counter,
    effect,
    format,
    hud,
    intro,
    killfeed,
    music,
    operation,
    outro,
    targetID: value.job_id,
    targetKind: 'job',
    transition,
  };
}

function requiredCreativeBrief(operation: string, argumentsValue: JsonObject): CreativeBriefRequirement | null {
  if (!isCreativeOperation(operation)) return null;
  const stream = operation === 'streams.start_render';
  const targetKey = stream ? 'stream_job_id' : 'job_id';
  const targetID = argumentsValue[targetKey];
  if (!isSafeReference(targetID)) throw new Error(`${targetKey} is required before a creative brief can be checked`);
  const variant = argumentsValue.variant;
  const edit = argumentsValue.edit;
  const editFormat = isJsonObject(edit) ? edit.format : undefined;
  const format = asCreativeFormat(variant) ?? asCreativeFormat(editFormat);
  return {
    ...(format === undefined ? {} : { format }),
    operation,
    targetID,
    targetKind: stream ? 'stream-job' : 'job',
  };
}

function creativeBriefKey(brief: Pick<CreativeBriefRequirement, 'operation' | 'targetID' | 'targetKind'>): string {
  return `${brief.operation}:${brief.targetKind}:${brief.targetID}`;
}

function creativeBriefPreview(brief: CreativeBrief): AssistantActionPreview {
  return {
    fields: [
      { label: 'Acción', value: brief.operation },
      { label: brief.targetKind === 'job' ? 'Demo' : 'Stream', value: brief.targetID },
      { label: 'Formato', value: brief.format },
      { label: 'HUD', value: brief.hud },
      { label: 'Killfeed', value: brief.killfeed },
      { label: 'Efecto', value: brief.effect },
      { label: 'Transición', value: brief.transition },
      { label: 'Contador', value: brief.counter },
      { label: 'Intro / outro', value: `${brief.intro} / ${brief.outro}` },
      { label: 'Música', value: brief.music },
      { label: 'Portada', value: brief.cover },
    ],
  };
}

function isCreativeOperation(value: string): value is CreativeOperation {
  return value === 'jobs.record' || value === 'jobs.generate' || value === 'renders.start' || value === 'streams.start_render';
}

function asCreativeFormat(value: JsonValue | undefined): CreativeBriefFormat | undefined {
  return value === 'short-9x16' || value === 'landscape-16x9' ? value : undefined;
}

function enumValue<T extends readonly string[]>(value: JsonValue | undefined, allowed: T, label: string): T[number] {
  if (typeof value !== 'string' || !allowed.includes(value)) throw new Error(`${label} is invalid`);
  return value as T[number];
}

function isSafeReference(value: JsonValue | undefined): value is string {
  return typeof value === 'string' && /^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$/.test(value);
}

function safeAppServerArgs(existing: readonly string[] | undefined): string[] {
  if (existing !== undefined) return [...existing];
  // Disable ambient capabilities: the only intentional model-visible Studio
  // interface is the dynamic fragforge namespace registered above. Emptying
  // external MCP configuration also avoids launching a user's unrelated MCPs.
  return [
    'app-server',
    '--stdio',
    '--disable', 'shell_tool',
    '--disable', 'browser_use',
    '--disable', 'browser_use_external',
    '--disable', 'computer_use',
    '--disable', 'image_generation',
    '--disable', 'apps',
    '--disable', 'hooks',
    '--disable', 'plugins',
    '--disable', 'remote_plugin',
    '--disable', 'skill_mcp_dependency_install',
    '--disable', 'workspace_dependencies',
    '--config', 'mcp_servers={}',
    '--config', "web_search='disabled'",
    '--strict-config',
  ];
}

/**
 * Codex retains its local sign-in session from its normal home directory, but
 * the Studio child does not inherit API keys or FragForge service secrets.
 * The app-server uses its own dynamic tools, so none of these values are
 * needed to answer about the current Studio page.
 */
function appServerEnvironment(overrides: NodeJS.ProcessEnv | undefined): NodeJS.ProcessEnv {
  const environment: NodeJS.ProcessEnv = { ...process.env, ...overrides };
  for (const key of Object.keys(environment)) {
    if (/^(?:FRAGFORGE_|ZV_)/i.test(key)
      || /(?:API[_-]?KEY|AUTH(?:ORIZATION)?|CREDENTIAL|PASSWORD|SECRET|TOKEN)/i.test(key)) {
      delete environment[key];
    }
  }
  return environment;
}

function parseSearchInput(value: unknown): JsonObject {
  if (!isJsonObject(value)) throw new Error('search input must be an object');
  const allowed = new Set(['arguments', 'category', 'include_dynamic_inputs', 'limit', 'operation', 'query', 'risk']);
  if (Object.keys(value).some((key) => !allowed.has(key))) throw new Error('search input contains an unknown field');
  if (value.query !== undefined && (typeof value.query !== 'string' || value.query.length > 1_000)) throw new Error('query is invalid');
  if (value.operation !== undefined && (typeof value.operation !== 'string' || value.operation.length > 128)) throw new Error('operation is invalid');
  if (value.category !== undefined && !['artifacts', 'catalog', 'jobs', 'renders', 'streams', 'studio'].includes(String(value.category))) {
    throw new Error('category is invalid');
  }
  if (value.risk !== undefined && !['read', 'write', 'costly', 'destructive'].includes(String(value.risk))) throw new Error('risk is invalid');
  if (value.include_dynamic_inputs !== undefined && typeof value.include_dynamic_inputs !== 'boolean') throw new Error('include_dynamic_inputs is invalid');
  if (value.limit !== undefined && (typeof value.limit !== 'number' || !Number.isInteger(value.limit) || value.limit < 1 || value.limit > 20)) {
    throw new Error('limit is invalid');
  }
  if (value.arguments !== undefined && !isJsonObject(value.arguments)) throw new Error('arguments is invalid');
  return value;
}

function parseOperationToolInput(value: unknown): { arguments: JsonObject; operation: string } {
  if (!isJsonObject(value)) throw new Error('operation input must be an object');
  const keys = Object.keys(value).sort();
  if (keys.some((key) => key !== 'arguments' && key !== 'operation')
    || typeof value.operation !== 'string'
    || value.operation.length === 0
    || value.operation.length > 128
    || (value.arguments !== undefined && !isJsonObject(value.arguments))) {
    throw new Error('operation input is invalid');
  }
  return { arguments: value.arguments === undefined ? {} : value.arguments, operation: value.operation };
}

function allowedOperation(name: string) {
  if (!ASSISTANT_OPERATIONS.has(name)) throw new Error('operation is not available in the embedded assistant');
  const definition = operationNamed(name);
  if (definition === undefined) throw new Error('operation is no longer available');
  return definition;
}

function executionForModel(outcome: Extract<McpOperationGatewayOutcome, { kind: 'executed' }>): JsonObject {
  return {
    operation: outcome.operation,
    result: modelSafeValue(outcome.result),
    status: outcome.status,
    ...(outcome.partialFailure ? { warning: 'The operation produced a durable partial result. Read current Studio state before retrying.' } : {}),
  };
}

/**
 * Produces the *complete* executable argument representation for an approval
 * card. Returning null is intentional: a mutation is never dispatched when
 * its full bounded semantic input cannot be shown to the approving person.
 */
function reviewableActionPreview(argumentsValue: JsonObject): AssistantActionPreview | null {
  const fields: Array<{ label: string; value: string }> = [];
  if (!collectReviewableFields(argumentsValue, [], fields)) return null;
  return { fields };
}

function collectReviewableFields(
  value: JsonValue,
  path: readonly string[],
  fields: Array<{ label: string; value: string }>,
): boolean {
  if (fields.length > MAX_REVIEWABLE_ACTION_FIELDS) return false;
  if (typeof value === 'string') {
    if (value.length > MAX_REVIEWABLE_ACTION_STRING_LENGTH || containsUnsafeTransportValue(value)) return false;
    fields.push({ label: humanizePath(path), value });
    return fields.length <= MAX_REVIEWABLE_ACTION_FIELDS;
  }
  if (value === null || typeof value === 'boolean' || typeof value === 'number') {
    fields.push({ label: humanizePath(path), value: String(value) });
    return fields.length <= MAX_REVIEWABLE_ACTION_FIELDS;
  }
  if (Array.isArray(value)) {
    if (value.length > MAX_REVIEWABLE_ACTION_ARRAY_ITEMS) return false;
    if (value.length === 0) {
      fields.push({ label: humanizePath(path), value: '[]' });
      return fields.length <= MAX_REVIEWABLE_ACTION_FIELDS;
    }
    for (const [index, child] of value.entries()) {
      if (!collectReviewableFields(child, [...path, String(index + 1)], fields)) return false;
    }
    return true;
  }
  const entries = Object.entries(value);
  if (entries.length === 0 || entries.length > MAX_REVIEWABLE_ACTION_ARRAY_ITEMS) return false;
  for (const [key, child] of entries) {
    if (unsafeArgumentKey(key) || !collectReviewableFields(child, [...path, key], fields)) return false;
  }
  return true;
}

function humanizePath(path: readonly string[]): string {
  return path.map(humanize).join(' · ');
}

function unsafeArgumentKey(key: string): boolean {
  return /(authorization|credential|cookie|file|password|secret|token|url)/i.test(key) || key === 'path';
}

function containsUnsafeTransportValue(value: string): boolean {
  return /\b(?:https?|file):\/\//i.test(value)
    || /(?:^|[\s"'(])[A-Za-z]:[\\/]/i.test(value)
    || /(?:^|[\s"'(])\\\\[^\\/\s]/.test(value)
    || /(?:api[_-]?key|authorization|bearer)\s*[:=]/i.test(value);
}

function modelSafeValue(value: JsonValue, depth = 0): JsonValue {
  if (depth >= MAX_MODEL_VALUE_DEPTH) return '[truncated]';
  if (typeof value === 'string') return containsUnsafeTransportValue(value) ? '[redacted]' : value.slice(0, MAX_MODEL_VALUE_LENGTH);
  if (value === null || typeof value === 'boolean' || typeof value === 'number') return value;
  if (Array.isArray(value)) return value.slice(0, MAX_MODEL_VALUE_ITEMS).map((item) => modelSafeValue(item, depth + 1));
  const result: JsonObject = {};
  let count = 0;
  for (const [key, child] of Object.entries(value)) {
    if (count >= MAX_MODEL_VALUE_ITEMS) break;
    if (sensitiveKey(key)) continue;
    result[key] = modelSafeValue(child, depth + 1);
    count += 1;
  }
  return result;
}

function sensitiveKey(key: string): boolean {
  return /(authorization|credential|cookie|error|failure|file|key|log|media|message|password|path|secret|token|trace|url|warning)/i.test(key);
}

function humanize(value: string): string {
  return value.replace(/[_-]+/g, ' ').replace(/^./, (character) => character.toUpperCase());
}

function cloneJsonObject(value: JsonObject): JsonObject {
  return JSON.parse(JSON.stringify(value)) as JsonObject;
}

function toolSuccess(value: JsonValue): AppServerDynamicToolResult {
  return {
    contentItems: [{ text: JSON.stringify(modelSafeValue(value)), type: 'inputText' }],
    success: true,
  };
}

function toolFailure(message: string): AppServerDynamicToolResult {
  return { contentItems: [{ text: message, type: 'inputText' }], success: false };
}

function turnPrompt(message: string, context: AssistantContext, stateNotes: readonly string[]): string {
  const contextLines = [
    `Visible Studio context: ${context.label} (${context.kind}).`,
    `Visible route: ${context.pathname}.`,
    ...(context.jobId === undefined ? [] : ['Current demo job: ' + context.jobId + '.']),
    ...(context.streamJobId === undefined ? [] : ['Current stream job: ' + context.streamJobId + '.']),
    ...(context.variant === undefined ? [] : ['Current render variant: ' + context.variant + '.']),
    ...(stateNotes.length === 0 ? [] : ['Recent Studio updates: ' + stateNotes.join(' ')]),
  ];
  return `${contextLines.join('\n')}\n\nUser message:\n${message}`;
}

function withTimeout<T>(promise: Promise<T>, timeoutMs: number, message: string): Promise<T> {
  return new Promise<T>((resolve, reject) => {
    const timeout = setTimeout(() => reject(new Error(message)), timeoutMs);
    timeout.unref();
    promise.then(
      (value) => {
        clearTimeout(timeout);
        resolve(value);
      },
      (error: unknown) => {
        clearTimeout(timeout);
        reject(error);
      },
    );
  });
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}
