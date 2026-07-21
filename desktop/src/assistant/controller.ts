import { createHash, randomUUID } from 'node:crypto';
import { basename } from 'node:path';
import {
  type AssistantAction,
  type AssistantActionPreview,
  type AssistantActionPreviewField,
  type AssistantContext,
  type AssistantEvent,
  type AssistantMessage,
  type AssistantSnapshot,
} from '../assistant-ipc.ts';
import { searchOperationCatalog, parseSearchRequest } from '../mcp/discovery.ts';
import { isJsonObject, type JsonObject, type JsonValue } from '../mcp/json.ts';
import {
  OperationGateway,
  type OperationGatewayOutcome,
} from '../studio-operations/operation-gateway.ts';
import { listOperations, operationNamed } from '../mcp/operations.ts';
import { OrchestratorClient } from '../mcp/orchestrator-client.ts';
import {
  createCodexAppServer,
  type AppServerAgentMessageDelta,
  type AppServerDynamicTool,
  type AppServerDynamicToolCall,
  type AppServerDynamicToolResult,
  type AppServerNotification,
  type AppServerStartThreadOptions,
  type AppServerTurnCompletedEvent,
  type AppServerTurnStartedEvent,
  type CodexAppServer,
  type CodexAppServerClientOptions,
  type CodexAppServerFactory,
} from './app-server-client.ts';
import { AssistantHistoryStore } from './history.ts';

const ACTION_EXPIRY_MS = 15 * 60_000;
const INITIALIZE_TIMEOUT_MS = 15_000;
const INTERRUPT_TIMEOUT_MS = 5_000;
const TURN_TIMEOUT_MS = 5 * 60_000;
const STATE_WATCH_INTERVAL_MS = 3_000;
const STATE_WATCH_TIMEOUT_MS = 2 * 60 * 60_000;
const MAX_ACTION_CARDS = 16;
const MAX_STATE_WATCHES = 4;
const MAX_MESSAGE_CONTENT_LENGTH = 12_000;
const MAX_MESSAGES = 200;
const MAX_MODEL_VALUE_LENGTH = 12_000;
const MAX_MODEL_VALUE_DEPTH = 8;
const MAX_MODEL_VALUE_ITEMS = 100;
const MAX_OPERATION_CONTINUATION_RESULT_LENGTH = 8_000;
const MAX_REVIEWABLE_ACTION_ARRAY_ITEMS = 32;
const MAX_REVIEWABLE_ACTION_FIELDS = 48;
const MAX_REVIEWABLE_ACTION_STRING_LENGTH = 240;
const MAX_STREAM_BRIEF_CLIPS = MAX_REVIEWABLE_ACTION_FIELDS - 10;
const MAX_STREAM_BRIEF_TITLE_SUMMARY_LENGTH = 8_000;

// Local media intake uses a dedicated native picker tool below so paths never
// enter model context. The path-taking catalog operations stay hidden while
// every other typed Studio operation remains available to the agent.
const STUDIO_FILE_PICKER_OPERATIONS = new Set([
  'jobs.create',
  'streams.create_from_file',
  'voices.save_profile',
]);
const ASSISTANT_OPERATIONS = new Set(
  listOperations()
    .map((operation) => operation.name)
    .filter((name) => !STUDIO_FILE_PICKER_OPERATIONS.has(name)),
);
const STATE_WATCH_OPERATIONS = new Set<StateWatchOperation>([
  'jobs.get',
  'renders.get',
  'streams.get',
  'streams.get_render',
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
          category: { enum: ['artifacts', 'catalog', 'jobs', 'renders', 'streams', 'studio', 'voices'], type: 'string' },
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
    {
      description: 'Open Studio\'s native file picker for a local CS2 demo or stream recording, then prepare its upload as a separate approval card. The local path is never returned to the agent.',
      inputSchema: localMediaSchema(),
      name: 'select_local_media',
      type: 'function',
    },
    {
      description: 'Watch one exact demo, stream, or render status read in the background. When its state changes, Studio automatically starts a new agent turn so the workflow can continue without another user message.',
      inputSchema: watchStateSchema(),
      name: 'watch_state',
      type: 'function',
    },
  ],
  type: 'namespace',
}];

const DEVELOPER_INSTRUCTIONS = `You are the integrated FragForge Studio agent. You may use only the fragforge dynamic tools for Studio data and actions. They are not shell, filesystem, browser, or generic MCP tools. Never request, inspect, or repeat local file paths, raw media, credentials, tokens, or secrets. When the user wants to import a local demo or stream recording, call select_local_media so Studio opens its native picker; never ask the user to type a path. A public Twitch URL supplied by the user may only be used with streams.create_from_url.

Always search the catalog before choosing an exact operation and use only IDs/options returned by the tool. Use read only for read-only operations. Preview only prepares a change for an exact Studio approval card; it never executes it. Never claim that a change ran unless Studio later reports completion. Never attempt a tool or workaround outside fragforge.

Before previewing demo capture or render (jobs.record, jobs.generate, or renders.start), collect every unanswered demo brief choice: format/aspect, HUD and killfeed treatment, effect and transition, numbering/counter, intro/outro, music, and cover strategy. Before streams.start_render, first read and prepare the saved edit plan, then collect the stream brief: delivery format, exact live layout variant, saved clip selection and title, crop/framing, killfeed treatment, Spanish subtitle/review policy, music, and cover strategy. Call creative_brief with the matching complete shape. The user must explicitly approve its Studio card before you can preview the costly operation. Generic words such as “go”, “dale”, “ok”, or “hazlo” are not creative approval unless that approved brief is already visible in the conversation. The later Studio approval card is the separate final approval for the exact operation.

Own the complete workflow instead of stopping after one step. After Studio reports an approved operation as started or completed, inspect the returned IDs and current state, continue all safe reads yourself, and prepare the next required mutation for approval. For demos this includes intake, roster/target selection, parsing, moment selection, creative brief, generation, render QA, and publish readiness. For streams this includes intake, edit planning, optional caption and killfeed review, creative brief, rendering, and artifact QA. Never repeat an accepted upload or costly action merely because its background job is still running.

When a demo, stream analysis, capture, or render is still running and no next action is possible yet, call watch_state for its exact status-read operation. Studio will wake you with the changed state; do not ask the user to poll or send another message.

Be concise, answer in the user's language, and explain unavailable capabilities honestly.`;

interface PendingOperationAction {
  arguments: JsonObject;
  card: AssistantAction;
  context: AssistantContext;
  expiryTimer: NodeJS.Timeout;
  kind: 'operation';
  operation: string;
  streamPlanFingerprint?: string;
}

interface PendingCreativeBrief {
  brief: CreativeBrief;
  card: AssistantAction;
  expiryTimer: NodeJS.Timeout;
  kind: 'creative-brief';
}

type PendingAction = PendingOperationAction | PendingCreativeBrief;
type TurnOutcome = 'cancelled' | 'completed' | 'failed';

interface CreativeBriefBase {
  cover: CreativeBriefCover;
  format: CreativeBriefFormat;
  music: CreativeBriefMusic;
  targetID: string;
  targetKind: 'job' | 'stream-job';
}

interface DemoCreativeBrief extends CreativeBriefBase {
  counter: CreativeBriefCounter;
  effect: CreativeBriefEffect;
  hud: CreativeBriefHUD;
  intro: CreativeBriefIntro;
  killfeed: CreativeBriefKillfeed;
  operation: Exclude<CreativeOperation, 'streams.start_render'>;
  outro: CreativeBriefOutro;
  targetKind: 'job';
  transition: CreativeBriefTransition;
}

interface StreamCreativeBriefInput extends CreativeBriefBase {
  captions: StreamCreativeBriefCaptions;
  clipSelection: 'saved-edit-plan';
  framing: StreamCreativeBriefFraming;
  killfeed: StreamCreativeBriefKillfeed;
  layout: string;
  operation: 'streams.start_render';
  targetKind: 'stream-job';
  title: string;
}

interface StreamCreativeBrief extends StreamCreativeBriefInput {
  planFingerprint: string;
  planPreviewFields: readonly AssistantActionPreviewField[];
  planUpdatedAt: string;
}

type CreativeBrief = DemoCreativeBrief | StreamCreativeBrief;
type ParsedCreativeBrief = DemoCreativeBrief | StreamCreativeBriefInput;
type ApprovedCreativeBrief = CreativeBrief & { expiresAt: number };

interface CreativeBriefRequirement {
  format?: CreativeBriefFormat;
  layout?: string;
  operation: CreativeOperation;
  targetID: string;
  targetKind: 'job' | 'stream-job';
}

interface LocalMediaRequest {
  kind: LocalMediaKind;
  targetSteamID?: string;
  title?: string;
}

interface PendingStateWatch {
  arguments: JsonObject;
  controller: AbortController;
  context: AssistantContext;
  expiresAt: number;
  expiryTimer: NodeJS.Timeout;
  operation: StateWatchOperation;
  state: string;
  timer?: NodeJS.Timeout;
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
type LocalMediaKind = 'demo' | 'stream';
type StreamCreativeBriefCaptions = 'disabled' | 'spanish-auto-review' | 'spanish-reviewed';
type StreamCreativeBriefFraming = 'clean-crop' | 'full-frame';
type StreamCreativeBriefKillfeed = 'none' | 'preserve' | 'synthetic';
type StateWatchOperation = 'jobs.get' | 'renders.get' | 'streams.get' | 'streams.get_render';

export interface AssistantControllerOptions {
  /** A dedicated empty directory; Codex never receives the Studio repository or user-data directory as its cwd. */
  cwd: string;
  gateway: OperationGateway;
  history: AssistantHistoryStore;
  onEvent?: (event: AssistantEvent) => void;
  /** Opens the Codex-owned ChatGPT OAuth URL in the user's system browser. */
  openAuthURL?: (url: string) => Promise<void>;
  orchestratorClient: OrchestratorClient;
  /** Non-sensitive diagnostic sink (usually the desktop studio log). */
  log?: (message: string) => void;
  /** Opens a native picker and returns its selected path only to the main process. */
  selectLocalMedia?: (kind: LocalMediaKind) => Promise<string | null>;
  stateWatchIntervalMs?: number;
  stateWatchTimeoutMs?: number;
  createAppServer?: CodexAppServerFactory;
  appServerOptions?: Omit<CodexAppServerClientOptions,
    'clientInfo' | 'cwd' | 'dynamicTools' | 'onAgentMessageDelta' | 'onDiagnostic' | 'onDynamicToolCall' | 'onError' | 'onNotification' | 'onStatus' | 'onTurnCompleted' | 'onTurnStarted'>;
  interruptTimeoutMs?: number;
  version?: string;
  turnTimeoutMs?: number;
}

/**
 * Owns a single local Codex app-server thread and the narrow bridge between
 * it and Studio's typed operation gateway. Renderer input never
 * becomes app-server JSON-RPC or a child-process command.
 */
export class AssistantController {
  readonly #appServerOptions: AssistantControllerOptions['appServerOptions'];
  readonly #createAppServer: CodexAppServerFactory;
  readonly #cwd: string;
  readonly #gateway: OperationGateway;
  readonly #history: AssistantHistoryStore;
  readonly #interruptTimeoutMs: number;
  readonly #log: ((message: string) => void) | undefined;
  readonly #onEvent: ((event: AssistantEvent) => void) | undefined;
  readonly #openAuthURL: (url: string) => Promise<void>;
  readonly #orchestratorClient: OrchestratorClient;
  readonly #selectLocalMedia?: AssistantControllerOptions['selectLocalMedia'];
  readonly #stateWatchIntervalMs: number;
  readonly #stateWatchTimeoutMs: number;
  readonly #version: string;
  readonly #turnTimeoutMs: number;
  readonly #pendingActions = new Map<string, PendingAction>();
  readonly #actionCards: AssistantAction[] = [];
  readonly #approvalControllers = new Set<AbortController>();
  readonly #approvedBriefs = new Map<string, ApprovedCreativeBrief>();
  readonly #stateNotes: string[] = [];
  readonly #stateWatches = new Map<string, PendingStateWatch>();
  #appServer: CodexAppServer | null = null;
  #account: AssistantSnapshot['account'] = { status: 'checking' };
  #availability: AssistantSnapshot['availability'] = 'starting';
  #busy = false;
  #closed = false;
  #error: string | undefined;
  #historyLoaded = false;
  #initializing: Promise<void> | null = null;
  #lastContext: AssistantContext = { kind: 'none', label: 'Studio', pathname: '/' };
  #loginID: string | null = null;
  #messages: AssistantMessage[] = [];
  #revision = 0;
  #activeMessageID: string | null = null;
  #activeTurnGeneration: number | null = null;
  #activeTurnID: string | null = null;
  #appServerGeneration = 0;
  readonly #completedTurnIDs = new Set<string>();
  #threadAttached = false;
  #threadID: string | undefined;
  readonly #turnOutcomes = new Map<number, TurnOutcome>();
  #turnGeneration = 0;
  #turnTimeout: NodeJS.Timeout | null = null;

  constructor(options: AssistantControllerOptions) {
    this.#appServerOptions = options.appServerOptions;
    this.#createAppServer = options.createAppServer ?? createCodexAppServer;
    this.#cwd = options.cwd;
    this.#gateway = options.gateway;
    this.#history = options.history;
    this.#interruptTimeoutMs = positiveDuration(options.interruptTimeoutMs ?? INTERRUPT_TIMEOUT_MS, 'interruptTimeoutMs');
    this.#log = options.log;
    this.#onEvent = options.onEvent;
    this.#openAuthURL = options.openAuthURL ?? (() => Promise.reject(new Error('OAuth browser opener is unavailable')));
    this.#orchestratorClient = options.orchestratorClient;
    this.#selectLocalMedia = options.selectLocalMedia;
    this.#stateWatchIntervalMs = positiveDuration(options.stateWatchIntervalMs ?? STATE_WATCH_INTERVAL_MS, 'stateWatchIntervalMs');
    this.#stateWatchTimeoutMs = positiveDuration(options.stateWatchTimeoutMs ?? STATE_WATCH_TIMEOUT_MS, 'stateWatchTimeoutMs');
    this.#version = options.version ?? '0.0.0';
    this.#turnTimeoutMs = positiveDuration(options.turnTimeoutMs ?? TURN_TIMEOUT_MS, 'turnTimeoutMs');
  }

  async status(): Promise<AssistantSnapshot> {
    await this.#ensureReady();
    return this.snapshot();
  }

  snapshot(): AssistantSnapshot {
    this.#expireActions();
    return {
      account: { ...this.#account },
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
      revision: this.#revision,
      ...(this.#threadID === undefined ? {} : { threadId: this.#threadID }),
    };
  }

  async send(message: string, context: AssistantContext): Promise<void> {
    await this.#ensureReady();
    if (this.#availability !== 'ready' || this.#appServer === null) {
      throw new Error('FragForge Agent is not available');
    }
    if (this.#account.status !== 'signed-in') throw new Error('the personal Codex account is not connected');
    if (this.#busy) throw new Error('an agent turn is already running');

    const userMessage = this.#appendMessage({ content: message, role: 'user' });
    await this.#startAgentTurn(message, context, userMessage.id);
  }

  async #startAgentTurn(message: string, context: AssistantContext, clientUserMessageID?: string): Promise<void> {
    const appServer = this.#appServer;
    if (this.#availability !== 'ready' || appServer === null) throw new Error('FragForge Agent is not available');
    if (this.#busy) throw new Error('an agent turn is already running');

    this.#lastContext = { ...context };
    const assistantMessage = this.#appendMessage({ content: '', role: 'assistant', streaming: true });
    const generation = ++this.#turnGeneration;
    this.#activeMessageID = assistantMessage.id;
    this.#activeTurnGeneration = generation;
    this.#activeTurnID = null;
    this.#busy = true;
    this.#armTurnTimeout(generation);
    this.#publish();
    void this.#saveHistory();

    try {
      const threadID = await this.#ensureThread();
      const turn = await appServer.startTurn(threadID, turnPrompt(message, context, this.#stateNotes), {
        ...(clientUserMessageID === undefined ? {} : { clientUserMessageId: clientUserMessageID }),
        cwd: this.#cwd,
      });
      if (this.#busy && this.#activeTurnGeneration === generation) {
        if (this.#activeTurnID === null) this.#activeTurnID = turn.id;
        else if (this.#activeTurnID !== turn.id) this.#writeDiagnostic('turn/start response did not match turn/started');
      }
    } catch (error) {
      if (this.#activeTurnGeneration !== generation) {
        const outcome = this.#turnOutcomes.get(generation);
        if (outcome !== undefined && outcome !== 'failed') return;
        throw new Error('No se pudo enviar el mensaje al agente.');
      }
      this.#finishTurnWithError(error);
      throw new Error('No se pudo enviar el mensaje al agente.');
    }
  }

  async cancel(): Promise<void> {
    if (!this.#busy || this.#appServer === null) return;
    if (this.#threadID === undefined || this.#activeTurnID === null) {
      const appServer = this.#appServer;
      this.#appServer = null;
      this.#appServerGeneration += 1;
      this.#threadAttached = false;
      appServer.close();
      this.#availability = 'starting';
      this.#finishTurnWithCancellation();
      await this.#ensureReady();
      return;
    }
    const generation = this.#activeTurnGeneration;
    try {
      await withTimeout(
        this.#appServer.interruptTurn(this.#threadID, this.#activeTurnID),
        this.#interruptTimeoutMs,
        'El agente no confirmó la cancelación a tiempo.',
      );
      if (this.#busy && this.#activeTurnGeneration !== null) {
        this.#armTurnTimeout(this.#activeTurnGeneration, this.#interruptTimeoutMs);
      }
    } catch (error) {
      if (generation !== null && this.#busy && this.#activeTurnGeneration === generation) {
        await this.#restartAfterTurnFailure(error);
        return;
      }
      throw new Error('No se pudo cancelar el turno del agente.');
    }
  }

  async approve(actionID: string): Promise<void> {
    this.#expireActions();
    if (this.#busy) throw new Error('wait for the active agent turn before approving an action');
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
      if (pending.brief.targetKind === 'stream-job') {
        const controller = new AbortController();
        this.#approvalControllers.add(controller);
        let planMatches = false;
        try {
          const plan = await this.#readStreamEditPlan(pending.brief.targetID, controller.signal);
          planMatches = streamPlanFingerprint(plan) === pending.brief.planFingerprint;
        } catch {
          // A stream brief fails closed when its canonical plan cannot be revalidated.
        } finally {
          this.#approvalControllers.delete(controller);
        }
        if (!planMatches) {
          this.#replaceActionCard({ ...pending.card, status: 'expired' });
          const invalidated = this.#invalidateStreamCreativeBrief('streams.update_edit_plan', {
            stream_job_id: pending.brief.targetID,
          });
          const summary = invalidated
            ?? `Studio detectó que el plan del stream ${pending.brief.targetID} cambió; el brief pendiente caducó antes de aprobarse.`;
          this.#addStateNote(summary);
          this.#appendMessage({ content: summary, role: 'system' });
          this.#publish();
          void this.#saveHistory();
          throw new Error('El plan del stream cambió; revisa y aprueba un brief nuevo.');
        }
      }
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
      try {
        await this.#startAgentTurn(
          creativeBriefContinuationPrompt(pending.brief),
          creativeBriefContext(pending.brief),
        );
      } catch {
        const recovery = `El brief sigue aprobado para ${pending.brief.operation}, pero el Agent no pudo preparar la tarjeta final automáticamente. Escribe «prepara la acción aprobada» para reintentarlo sin repetir el brief.`;
        this.#addStateNote(recovery);
        this.#appendMessage({ content: recovery, role: 'system' });
        this.#publish();
        void this.#saveHistory();
      }
      return;
    }

    const controller = new AbortController();
    this.#approvalControllers.add(controller);
    let outcome: Extract<OperationGatewayOutcome, { kind: 'executed' }>;
    try {
      if (pending.streamPlanFingerprint !== undefined) {
        const streamJobID = safeReferenceFrom(pending.arguments.stream_job_id);
        let planMatches = false;
        if (streamJobID !== undefined) {
          try {
            const plan = await this.#readStreamEditPlan(streamJobID, controller.signal);
            planMatches = streamPlanFingerprint(plan) === pending.streamPlanFingerprint;
          } catch {
            // A costly render must fail closed when its approved plan cannot be verified.
          }
        }
        if (!planMatches) {
          const invalidated = streamJobID === undefined
            ? undefined
            : this.#invalidateStreamCreativeBrief('streams.update_edit_plan', { stream_job_id: streamJobID });
          if (invalidated !== undefined) {
            this.#addStateNote(invalidated);
            this.#appendMessage({ content: invalidated, role: 'system' });
          }
          throw new Error('the approved stream plan changed before render execution');
        }
      }
      const execution = await this.#gateway.execute(
        { arguments: pending.arguments, operation: pending.operation },
        { privileged: true, signal: controller.signal },
      );
      if (execution.kind !== 'executed') throw new Error('approved action did not execute');
      if (execution.partialFailure) {
        const invalidated = this.#invalidateStreamCreativeBrief(pending.operation, pending.arguments);
        if (invalidated !== undefined) {
          this.#addStateNote(invalidated);
          this.#appendMessage({ content: invalidated, role: 'system' });
        }
        throw new Error('the action produced a partial result');
      }
      outcome = execution;
      this.#replaceActionCard({ ...pending.card, status: 'completed' });
      const summary = pending.card.risk === 'costly'
        ? `Studio inició ${pending.operation}. La tarea continúa en segundo plano; consulta su estado antes de reintentar.`
        : `Studio completó ${pending.operation}.`;
      this.#addStateNote(summary);
      this.#appendMessage({ content: summary, role: 'system' });
      const invalidatedBrief = this.#invalidateStreamCreativeBrief(pending.operation, pending.arguments);
      if (invalidatedBrief !== undefined) {
        this.#addStateNote(invalidatedBrief);
        this.#appendMessage({ content: invalidatedBrief, role: 'system' });
      }
      this.#publish();
      void this.#saveHistory();
    } catch {
      if (pending.streamPlanFingerprint !== undefined) {
        const streamJobID = safeReferenceFrom(pending.arguments.stream_job_id);
        const invalidated = streamJobID === undefined
          ? undefined
          : this.#invalidateStreamCreativeBrief('streams.update_edit_plan', { stream_job_id: streamJobID });
        if (invalidated !== undefined) {
          this.#addStateNote(invalidated);
          this.#appendMessage({ content: invalidated, role: 'system' });
        }
      }
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

    const context = operationContinuationContext(pending.operation, pending.arguments, outcome.result, pending.context);
    try {
      await this.#startAgentTurn(operationContinuationPrompt(pending.operation, outcome), context);
    } catch {
      const recovery = `Studio completó ${pending.operation}, pero el Agent no pudo continuar el flujo automáticamente. Escribe «continúa el flujo» para reanudar desde el estado actual sin repetir la acción.`;
      this.#addStateNote(recovery);
      this.#appendMessage({ content: recovery, role: 'system' });
      this.#publish();
      void this.#saveHistory();
    }
  }

  reject(actionID: string): void {
    this.#expireActions();
    const pending = this.#pendingActions.get(actionID);
    if (pending === undefined) throw new Error('this action is no longer available');
    this.#pendingActions.delete(actionID);
    clearTimeout(pending.expiryTimer);
    this.#replaceActionCard({ ...pending.card, status: 'rejected' });
    const summary = pending.kind === 'creative-brief'
      ? `El usuario rechazó el brief creativo para ${pending.brief.operation}; no se ejecutó ninguna acción.`
      : `El usuario rechazó ${pending.operation}; no se ejecutó la acción.`;
    this.#addStateNote(summary);
    this.#appendMessage({ content: summary, role: 'system' });
    this.#publish();
    void this.#saveHistory();
  }

  async newConversation(): Promise<void> {
    if (this.#busy) throw new Error('wait for the active agent turn before starting a new conversation');
    this.#threadID = undefined;
    this.#threadAttached = false;
    this.#messages = [];
    this.#stateNotes.length = 0;
    this.#approvedBriefs.clear();
    this.#clearStateWatches();
    this.#clearActions();
    await this.#history.save({ messages: [] });
    this.#publish();
    if (this.#availability !== 'ready' || this.#appServer === null) {
      await this.#ensureReady();
      if (this.#availability !== 'ready' || this.#appServer === null) throw new Error('FragForge Agent is not available');
    }
  }

  async clearHistory(): Promise<void> {
    if (this.#busy) throw new Error('wait for the active agent turn before clearing history');
    this.#messages = [];
    await this.#history.clear();
    this.#publish();
  }

  async login(): Promise<void> {
    if (this.#busy) throw new Error('wait for the active agent turn before signing in');
    await this.#ensureReady();
    const appServer = this.#appServer;
    if (this.#availability !== 'ready' || appServer === null) throw new Error('FragForge Agent is not available');
    if (this.#account.status === 'signed-in' || this.#account.status === 'signing-in') return;

    if (this.#account.status === 'unsupported') await appServer.logoutAccount();
    this.#detachThread();
    this.#account = { status: 'signing-in' };
    this.#error = undefined;
    this.#publish();
    try {
      const login = await appServer.loginChatGPT();
      this.#loginID = login.loginId;
      await this.#openAuthURL(login.authUrl);
    } catch {
      const loginID = this.#loginID;
      this.#loginID = null;
      if (loginID !== null) {
        try {
          await appServer.cancelLogin(loginID);
        } catch {
          // The login may already have completed or the app-server may be closing.
        }
      }
      this.#account = { status: 'error' };
      this.#error = 'No se pudo abrir el inicio de sesión de Codex.';
      this.#publish();
      throw new Error('No se pudo iniciar sesión con Codex.');
    }
  }

  async logout(): Promise<void> {
    if (this.#busy) throw new Error('wait for the active agent turn before signing out');
    const appServer = this.#appServer;
    if (appServer === null) return;
    const loginID = this.#loginID;
    this.#loginID = null;
    if (loginID !== null) {
      try {
        await appServer.cancelLogin(loginID);
      } catch {
        // A completed login may race with an explicit disconnect.
      }
    }
    await appServer.logoutAccount();
    this.#detachThread();
    this.#account = { status: 'signed-out' };
    this.#error = undefined;
    this.#publish();
  }

  close(): void {
    if (this.#closed) return;
    this.#closed = true;
    for (const controller of this.#approvalControllers) controller.abort();
    this.#approvalControllers.clear();
    this.#approvedBriefs.clear();
    this.#clearStateWatches();
    this.#clearActions();
    this.#clearTurnTimeout();
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
    const generation = ++this.#appServerGeneration;
    try {
      await this.#loadHistory();
      const appServer = this.#createAppServer({
        ...this.#appServerOptions,
        args: safeAppServerArgs(this.#appServerOptions?.args),
        clientInfo: { name: 'fragforge_studio', title: 'FragForge Studio', version: this.#version },
        cwd: this.#cwd,
        dynamicTools: DYNAMIC_TOOLS,
        env: appServerEnvironment(this.#appServerOptions?.env),
        onAgentMessageDelta: (delta) => this.#handleAgentDelta(delta, generation),
        onDiagnostic: (message) => this.#writeDiagnostic(message),
        onDynamicToolCall: (call, signal) => this.#handleDynamicToolCall(call, generation, signal),
        onError: () => this.#handleAppServerFailure(generation),
        onNotification: (notification) => this.#handleAppServerNotification(notification, generation),
        onStatus: (status) => {
          if (status === 'failed' || (status === 'closed' && !this.#closed)) this.#handleAppServerFailure(generation);
        },
        onTurnCompleted: (event) => this.#handleTurnCompleted(event, generation),
        onTurnStarted: (event) => this.#handleTurnStarted(event, generation),
      });
      this.#appServer = appServer;
      this.#threadAttached = false;
      await withTimeout(appServer.initialize(), INITIALIZE_TIMEOUT_MS, 'Codex tardó demasiado en responder.');
      if (this.#closed || generation !== this.#appServerGeneration) {
        appServer.close();
        return;
      }
      this.#setAccountFromSnapshot(await appServer.readAccount(false));
      this.#availability = 'ready';
      this.#error = undefined;
      this.#publish();
    } catch (error) {
      if (this.#closed || generation !== this.#appServerGeneration) return;
      this.#writeDiagnostic(`startup failed: ${errorMessage(error)}`);
      this.#appServer?.close();
      this.#appServer = null;
      this.#availability = 'unavailable';
      this.#account = { status: 'error' };
      this.#error = 'No se pudo iniciar el agente local. Comprueba que Codex esté instalado y actualizado.';
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

  #detachThread(): void {
    this.#threadID = undefined;
    this.#threadAttached = false;
    void this.#saveHistory();
  }

  #setAccountFromSnapshot(snapshot: Awaited<ReturnType<CodexAppServer['readAccount']>>): void {
    if (snapshot.account?.type === 'chatgpt') {
      this.#account = { planType: snapshot.account.planType, status: 'signed-in' };
      return;
    }
    this.#account = snapshot.account === null ? { status: 'signed-out' } : { status: 'unsupported' };
  }

  #handleAppServerNotification(notification: AppServerNotification, appServerGeneration: number): void {
    if (this.#closed || appServerGeneration !== this.#appServerGeneration) return;
    if (notification.method === 'account/updated' && isJsonObject(notification.params)) {
      const authMode = notification.params.authMode;
      const planType = notification.params.planType;
      this.#loginID = null;
      if (authMode === 'chatgpt') {
        this.#account = {
          ...(typeof planType === 'string' ? { planType } : {}),
          status: 'signed-in',
        };
        this.#error = undefined;
      } else if (authMode === null) {
        this.#account = { status: 'signed-out' };
      } else if (typeof authMode === 'string') {
        this.#account = { status: 'unsupported' };
      }
      this.#publish();
      return;
    }
    if (notification.method !== 'account/login/completed' || !isJsonObject(notification.params)) return;
    if (notification.params.loginId !== null && notification.params.loginId !== this.#loginID) return;
    this.#loginID = null;
    if (notification.params.success !== true) {
      this.#account = { status: 'error' };
      this.#error = 'No se pudo completar el inicio de sesión con Codex.';
      this.#publish();
      return;
    }
    const appServer = this.#appServer;
    if (appServer === null) return;
    void appServer.readAccount(true).then((snapshot) => {
      if (this.#closed || appServerGeneration !== this.#appServerGeneration) return;
      this.#setAccountFromSnapshot(snapshot);
      this.#error = undefined;
      this.#publish();
    }).catch(() => {
      if (this.#closed || appServerGeneration !== this.#appServerGeneration) return;
      this.#account = { status: 'error' };
      this.#error = 'Codex inició sesión, pero Studio no pudo verificar la cuenta.';
      this.#publish();
    });
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
    if (this.#threadID !== undefined && this.#threadAttached) return this.#threadID;
    if (this.#threadID !== undefined) {
      try {
        const thread = await appServer.resumeThread(this.#threadID, {
          approvalPolicy: options.approvalPolicy,
          baseInstructions: options.baseInstructions,
          cwd: options.cwd,
          developerInstructions: options.developerInstructions,
          excludeTurns: true,
          model: options.model,
          personality: options.personality,
          sandbox: options.sandbox,
        });
        this.#threadID = thread.id;
        this.#threadAttached = true;
        return thread.id;
      } catch (error) {
        this.#writeDiagnostic(`could not resume stored Codex thread: ${errorMessage(error)}`);
        this.#threadID = undefined;
        this.#appendMessage({ content: 'Studio no pudo reanudar el hilo anterior del agente; se creó una conversación nueva.', role: 'system' });
      }
    }
    const thread = await appServer.startThread(options);
    this.#threadID = thread.id;
    this.#threadAttached = true;
    void this.#saveHistory();
    return thread.id;
  }

  async #handleDynamicToolCall(
    call: AppServerDynamicToolCall,
    appServerGeneration: number,
    signal: AbortSignal,
  ): Promise<AppServerDynamicToolResult> {
    const turnGeneration = this.#activeTurnGeneration;
    if (this.#closed
      || appServerGeneration !== this.#appServerGeneration
      || turnGeneration === null
      || this.#completedTurnIDs.has(call.turnId)
      || call.namespace !== 'fragforge'
      || call.threadId !== this.#threadID
      || (this.#activeTurnID !== null && call.turnId !== this.#activeTurnID)) {
      return toolFailure('This FragForge tool call is not valid for the active Studio conversation.');
    }
    if (this.#activeTurnID === null) this.#activeTurnID = call.turnId;
    this.#armTurnTimeout(turnGeneration);
    const isActive = (): boolean => !signal.aborted
      && appServerGeneration === this.#appServerGeneration
      && turnGeneration === this.#activeTurnGeneration
      && this.#busy
      && call.threadId === this.#threadID
      && call.turnId === this.#activeTurnID;
    try {
      if (call.tool === 'search') {
        const result = await this.#search(call.arguments, signal);
        return isActive() ? result : toolFailure('This FragForge tool call is no longer active.');
      }
      if (call.tool === 'read') {
        const result = await this.#read(call.arguments, signal);
        return isActive() ? result : toolFailure('This FragForge tool call is no longer active.');
      }
      if (call.tool === 'preview') return await this.#preview(call.arguments, signal, isActive);
      if (call.tool === 'creative_brief') return await this.#creativeBrief(call.arguments, signal, isActive);
      if (call.tool === 'select_local_media') return await this.#selectLocalMediaForUpload(call.arguments, signal, isActive);
      if (call.tool === 'watch_state') return await this.#watchState(call.arguments, signal, isActive);
      return toolFailure('Unknown FragForge Studio tool.');
    } catch (error) {
      this.#writeDiagnostic(`dynamic tool ${call.tool} failed: ${errorMessage(error)}`);
      return toolFailure('FragForge Studio could not complete that safe tool request. Check the current Studio state and try again.');
    }
  }

  async #search(value: unknown, signal: AbortSignal): Promise<AppServerDynamicToolResult> {
    const input = parseSearchInput(value);
    const operation = input.operation;
    if (typeof operation === 'string' && !ASSISTANT_OPERATIONS.has(operation)) {
      return toolFailure('That operation is not available in the embedded assistant. Use the corresponding Studio screen instead.');
    }
    const result = await searchOperationCatalog(this.#orchestratorClient, parseSearchRequest(input), signal);
    const operations = Array.isArray(result.operations)
      ? result.operations.filter((entry) => isJsonObject(entry) && typeof entry.name === 'string' && ASSISTANT_OPERATIONS.has(entry.name))
      : [];
    return toolSuccess({
      ...result,
      count: operations.length,
      operations,
      instructions: 'Choose only one listed operation. The agent never accepts local files, raw media, credentials, or arbitrary shell commands. It accepts only public Twitch URLs for streams.create_from_url.',
    });
  }

  async #read(value: unknown, signal: AbortSignal): Promise<AppServerDynamicToolResult> {
    const request = parseOperationToolInput(value);
    const definition = allowedOperation(request.operation);
    if (definition.risk !== 'read') return toolFailure('That operation changes Studio. Use preview so Studio can request exact user approval.');
    const outcome = await this.#gateway.execute(request, { signal });
    if (outcome.kind !== 'executed') return toolFailure('The read operation could not be completed.');
    return toolSuccess(executionForModel(outcome));
  }

  async #preview(
    value: unknown,
    signal: AbortSignal,
    isActive: () => boolean,
  ): Promise<AppServerDynamicToolResult> {
    const request = parseOperationToolInput(value);
    const definition = allowedOperation(request.operation);
    if (definition.risk === 'read') return toolFailure('That operation is read-only. Use read instead.');
    const requiredBrief = requiredCreativeBrief(request.operation, request.arguments);
    let approvedBrief: ApprovedCreativeBrief | undefined;
    if (requiredBrief !== null) {
      approvedBrief = await this.#approvedBriefFor(requiredBrief, signal);
      if (approvedBrief === undefined) {
        return toolFailure('Studio requires a complete creative brief approved by the user for this exact capture or render action and current saved plan. Read the latest state, collect the choices, call creative_brief, wait for its card approval, then preview this operation again.');
      }
    }
    const operationRequest = approvedBrief?.targetKind === 'stream-job'
      ? {
          arguments: {
            ...request.arguments,
            expected_edit_plan_updated_at: approvedBrief.planUpdatedAt,
          },
          operation: request.operation,
        }
      : request;
    const outcome = await this.#gateway.execute(operationRequest, { signal });
    if (outcome.kind !== 'preview') return toolFailure('The operation did not produce an approval preview.');
    if (!isActive()) return toolFailure('This FragForge tool call is no longer active.');
    const cardPreview = reviewableActionPreview(outcome.arguments);
    if (cardPreview === null) {
      return toolFailure('Studio cannot show every validated detail of this request in one exact approval card. Use the corresponding Studio screen or reduce the selection before trying again.');
    }
    const card = this.#createActionCard(outcome, definition.title, definition.description, cardPreview);
    const pending: PendingAction = {
      arguments: cloneJsonObject(outcome.arguments),
      card,
      context: operationContinuationContext(outcome.operation, outcome.arguments, undefined, this.#lastContext),
      expiryTimer: setTimeout(() => this.#expireAction(card.id), ACTION_EXPIRY_MS),
      kind: 'operation',
      operation: outcome.operation,
      ...(approvedBrief?.targetKind === 'stream-job' ? { streamPlanFingerprint: approvedBrief.planFingerprint } : {}),
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

  async #selectLocalMediaForUpload(
    value: unknown,
    signal: AbortSignal,
    isActive: () => boolean,
  ): Promise<AppServerDynamicToolResult> {
    const request = parseLocalMediaRequest(value);
    if (this.#selectLocalMedia === undefined) {
      return toolFailure('Studio cannot open the local media picker in this build. Use the upload screen instead.');
    }
    const selectedPath = await this.#selectLocalMedia(request.kind);
    if (!isActive()) return toolFailure('This FragForge tool call is no longer active.');
    if (selectedPath === null) return toolSuccess({ kind: request.kind, status: 'cancelled' });

    const operation = request.kind === 'demo' ? 'jobs.create' : 'streams.create_from_file';
    const argumentsValue: JsonObject = request.kind === 'demo'
      ? {
          demo_path: selectedPath,
          ...(request.targetSteamID === undefined ? {} : { target_steamid: request.targetSteamID }),
        }
      : {
          video_path: selectedPath,
          ...(request.title === undefined ? {} : { title: request.title }),
        };
    const definition = operationNamed(operation);
    if (definition === undefined) throw new Error('local media operation is unavailable');
    const outcome = await this.#gateway.execute({ arguments: argumentsValue, operation }, { signal });
    if (outcome.kind !== 'preview') throw new Error('local media operation did not produce an approval preview');
    if (!isActive()) return toolFailure('This FragForge tool call is no longer active.');

    const cardPreview: AssistantActionPreview = {
      fields: [
        { label: 'Archivo', value: basename(selectedPath) },
        ...(request.targetSteamID === undefined ? [] : [{ label: 'SteamID objetivo', value: request.targetSteamID }]),
        ...(request.title === undefined ? [] : [{ label: 'Título', value: request.title }]),
      ],
    };
    const card = this.#createActionCard(outcome, definition.title, definition.description, cardPreview);
    const pending: PendingOperationAction = {
      arguments: cloneJsonObject(outcome.arguments),
      card,
      context: { ...this.#lastContext },
      expiryTimer: setTimeout(() => this.#expireAction(card.id), ACTION_EXPIRY_MS),
      kind: 'operation',
      operation,
    };
    pending.expiryTimer.unref();
    this.#pendingActions.set(card.id, pending);
    this.#actionCards.push(card);
    this.#trimActionCards();
    this.#publish();
    return toolSuccess({
      action_id: card.id,
      kind: request.kind,
      operation,
      requires_user_approval: true,
      status: 'pending',
    });
  }

  async #watchState(
    value: unknown,
    signal: AbortSignal,
    isActive: () => boolean,
  ): Promise<AppServerDynamicToolResult> {
    const request = parseStateWatchRequest(value);
    const outcome = await this.#gateway.execute(request, { signal });
    if (outcome.kind !== 'executed') throw new Error('state watch operation did not execute as a read');
    const state = watchedState(outcome.result);
    if (state === undefined) return toolFailure('Studio could not identify a bounded state field in that response. Use read and continue manually.');
    if (!isActive()) return toolFailure('This FragForge tool call is no longer active.');

    const key = stateWatchKey(request.operation, outcome.arguments);
    if (!this.#stateWatches.has(key) && this.#stateWatches.size >= MAX_STATE_WATCHES) {
      return toolFailure(`Studio already has ${MAX_STATE_WATCHES} active state watches in this conversation. Reuse an existing watch or wait for one to finish.`);
    }
    this.#removeStateWatch(key);
    const expiresAt = Date.now() + this.#stateWatchTimeoutMs;
    let watch: PendingStateWatch;
    const expiryTimer = setTimeout(() => this.#expireStateWatch(key, watch), this.#stateWatchTimeoutMs);
    expiryTimer.unref();
    watch = {
      arguments: cloneJsonObject(outcome.arguments),
      context: operationContinuationContext(request.operation, outcome.arguments, outcome.result, this.#lastContext),
      controller: new AbortController(),
      expiresAt,
      expiryTimer,
      operation: request.operation,
      state,
    };
    this.#stateWatches.set(key, watch);
    this.#scheduleStateWatch(key, watch);
    return toolSuccess({
      current: executionForModel(outcome),
      state,
      status: 'watching',
    });
  }

  #scheduleStateWatch(key: string, watch: PendingStateWatch): void {
    watch.timer = setTimeout(() => void this.#pollStateWatch(key), this.#stateWatchIntervalMs);
    watch.timer.unref();
  }

  async #pollStateWatch(key: string): Promise<void> {
    const watch = this.#stateWatches.get(key);
    if (watch === undefined || this.#closed) return;
    if (watch.expiresAt <= Date.now()) {
      this.#expireStateWatch(key, watch);
      return;
    }

    try {
      const outcome = await this.#gateway.execute(
        { arguments: watch.arguments, operation: watch.operation },
        { signal: watch.controller.signal },
      );
      if (this.#closed || this.#stateWatches.get(key) !== watch) return;
      if (watch.expiresAt <= Date.now()) {
        this.#expireStateWatch(key, watch);
        return;
      }
      if (outcome.kind !== 'executed') throw new Error('state watch read did not execute');
      const state = watchedState(outcome.result);
      if (state === undefined || state === watch.state || this.#busy) {
        this.#scheduleStateWatch(key, watch);
        return;
      }

      this.#removeStateWatch(key);
      const summary = `Studio detectó que ${watch.operation} cambió de ${watch.state} a ${state}.`;
      this.#addStateNote(summary);
      this.#appendMessage({ content: summary, role: 'system' });
      this.#publish();
      void this.#saveHistory();
      try {
        await this.#startAgentTurn(stateWatchContinuationPrompt(watch.operation, watch.state, state, outcome), watch.context);
      } catch {
        const recovery = `${summary} El Agent no pudo reanudarse automáticamente; escribe «continúa el flujo» para seguir desde este estado.`;
        this.#addStateNote(recovery);
        this.#appendMessage({ content: recovery, role: 'system' });
        this.#publish();
        void this.#saveHistory();
      }
    } catch {
      if (!this.#closed && !watch.controller.signal.aborted && this.#stateWatches.get(key) === watch) {
        this.#scheduleStateWatch(key, watch);
      }
    }
  }

  async #creativeBrief(
    value: unknown,
    signal: AbortSignal,
    isActive: () => boolean,
  ): Promise<AppServerDynamicToolResult> {
    const parsed = parseCreativeBrief(value);
    let brief: CreativeBrief;
    if (parsed.targetKind === 'stream-job') {
      const plan = await this.#readStreamEditPlan(parsed.targetID, signal);
      if (!isActive()) return toolFailure('This FragForge tool call is no longer active.');
      brief = bindStreamCreativeBrief(parsed, plan);
    } else {
      brief = parsed;
    }
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
    outcome: Extract<OperationGatewayOutcome, { kind: 'preview' }>,
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

  async #approvedBriefFor(required: CreativeBriefRequirement, signal: AbortSignal): Promise<ApprovedCreativeBrief | undefined> {
    const approved = this.#approvedBriefs.get(creativeBriefKey(required));
    if (approved === undefined) return undefined;
    if (approved.expiresAt <= Date.now()) {
      this.#approvedBriefs.delete(creativeBriefKey(required));
      return undefined;
    }
    if (required.format !== undefined && approved.format !== required.format) return undefined;
    if (required.layout !== undefined) {
      if (approved.targetKind !== 'stream-job' || approved.layout !== required.layout) return undefined;
      const plan = await this.#readStreamEditPlan(required.targetID, signal);
      if (streamPlanFingerprint(plan) !== approved.planFingerprint) {
        const invalidated = this.#invalidateStreamCreativeBrief('streams.update_edit_plan', { stream_job_id: required.targetID });
        if (invalidated !== undefined) {
          this.#addStateNote(invalidated);
          this.#appendMessage({ content: invalidated, role: 'system' });
          this.#publish();
          void this.#saveHistory();
        }
        return undefined;
      }
    }
    return approved;
  }

  async #readStreamEditPlan(streamJobID: string, signal: AbortSignal): Promise<JsonObject> {
    const outcome = await this.#gateway.execute({
      arguments: { stream_job_id: streamJobID },
      operation: 'streams.get_edit_plan',
    }, { signal });
    if (outcome.kind !== 'executed' || !isJsonObject(outcome.result)) throw new Error('stream edit plan could not be read');
    return outcome.result;
  }

  #invalidateStreamCreativeBrief(operation: string, argumentsValue: JsonObject): string | undefined {
    if (!invalidatesStreamCreativeBrief(operation)) return undefined;
    const streamJobID = safeReferenceFrom(argumentsValue.stream_job_id);
    if (streamJobID === undefined) return undefined;
    const key = creativeBriefKey({
      operation: 'streams.start_render',
      targetID: streamJobID,
      targetKind: 'stream-job',
    });
    const briefInvalidated = this.#approvedBriefs.delete(key);
    let briefCardsExpired = 0;
    let renderCardsExpired = 0;
    for (const [actionID, pending] of this.#pendingActions) {
      const isPendingBrief = pending.kind === 'creative-brief'
        && pending.brief.targetKind === 'stream-job'
        && pending.brief.targetID === streamJobID;
      const isPendingRender = pending.kind === 'operation'
        && pending.operation === 'streams.start_render'
        && pending.arguments.stream_job_id === streamJobID;
      if (!isPendingBrief && !isPendingRender) continue;
      this.#pendingActions.delete(actionID);
      clearTimeout(pending.expiryTimer);
      this.#replaceActionCard({ ...pending.card, status: 'expired' });
      if (isPendingBrief) briefCardsExpired += 1;
      else renderCardsExpired += 1;
    }
    if (!briefInvalidated && briefCardsExpired === 0 && renderCardsExpired === 0) return undefined;
    const expiredBriefs = briefCardsExpired === 0
      ? ''
      : ` También caducó ${briefCardsExpired === 1 ? 'el brief pendiente' : `${briefCardsExpired} briefs pendientes`}.`;
    const expiredRenders = renderCardsExpired === 0
      ? ''
      : ` También caducó ${renderCardsExpired === 1 ? 'la confirmación de render pendiente' : `${renderCardsExpired} confirmaciones de render pendientes`}.`;
    return `Studio cambió el plan del stream ${streamJobID}; el brief creativo anterior quedó invalidado y debe revisarse de nuevo antes del render.${expiredBriefs}${expiredRenders}`;
  }

  #handleAgentDelta(delta: AppServerAgentMessageDelta, appServerGeneration: number): void {
    if (appServerGeneration !== this.#appServerGeneration || this.#completedTurnIDs.has(delta.turnId)) return;
    if (!this.#busy || delta.threadId !== this.#threadID || this.#activeMessageID === null) return;
    if (this.#activeTurnID !== null && delta.turnId !== this.#activeTurnID) return;
    const message = this.#messages.find((item) => item.id === this.#activeMessageID);
    if (message === undefined) return;
    if (this.#activeTurnGeneration !== null) this.#armTurnTimeout(this.#activeTurnGeneration);
    if (message.content.length < MAX_MESSAGE_CONTENT_LENGTH) {
      message.content += delta.delta.slice(0, MAX_MESSAGE_CONTENT_LENGTH - message.content.length);
    }
    this.#publish();
  }

  #handleTurnStarted(event: AppServerTurnStartedEvent, appServerGeneration: number): void {
    if (appServerGeneration !== this.#appServerGeneration || this.#completedTurnIDs.has(event.turn.id)) return;
    if (!this.#busy || event.threadId !== this.#threadID) return;
    if (this.#activeTurnID === null) this.#activeTurnID = event.turn.id;
  }

  #handleTurnCompleted(event: AppServerTurnCompletedEvent, appServerGeneration: number): void {
    if (appServerGeneration !== this.#appServerGeneration || this.#completedTurnIDs.has(event.turn.id)) return;
    if (!this.#busy || event.threadId !== this.#threadID) return;
    if (this.#activeTurnID !== null && event.turn.id !== this.#activeTurnID) return;
    this.#completedTurnIDs.add(event.turn.id);
    if (this.#completedTurnIDs.size > 100) this.#completedTurnIDs.delete(this.#completedTurnIDs.values().next().value ?? '');
    const message = this.#activeMessageID === null ? undefined : this.#messages.find((item) => item.id === this.#activeMessageID);
    if (message !== undefined) message.streaming = false;
    this.#markTurnOutcome('completed');
    this.#clearTurnTimeout();
    this.#busy = false;
    this.#activeMessageID = null;
    this.#activeTurnGeneration = null;
    this.#activeTurnID = null;
    this.#publish();
    void this.#saveHistory();
  }

  #finishTurnWithError(error: unknown): void {
    this.#clearTurnTimeout();
    this.#writeDiagnostic(`turn failed: ${errorMessage(error)}`);
    const message = this.#activeMessageID === null ? undefined : this.#messages.find((item) => item.id === this.#activeMessageID);
    if (message !== undefined) {
      message.content = message.content || 'El agente no pudo completar esta respuesta.';
      message.streaming = false;
    }
    this.#markTurnOutcome('failed');
    this.#busy = false;
    this.#activeMessageID = null;
    this.#activeTurnGeneration = null;
    this.#activeTurnID = null;
    this.#publish();
    void this.#saveHistory();
  }

  #finishTurnWithCancellation(): void {
    this.#clearTurnTimeout();
    const message = this.#activeMessageID === null ? undefined : this.#messages.find((item) => item.id === this.#activeMessageID);
    if (message !== undefined) {
      message.content = message.content || 'Respuesta cancelada.';
      message.streaming = false;
    }
    this.#markTurnOutcome('cancelled');
    this.#busy = false;
    this.#activeMessageID = null;
    this.#activeTurnGeneration = null;
    this.#activeTurnID = null;
    this.#publish();
    void this.#saveHistory();
  }

  #handleAppServerFailure(generation: number): void {
    if (this.#closed || generation !== this.#appServerGeneration) return;
    const appServer = this.#appServer;
    this.#appServer = null;
    this.#appServerGeneration += 1;
    appServer?.close();
    this.#availability = 'error';
    this.#threadAttached = false;
    this.#error = 'La conexión local con el agente se cerró. Abre una conversación nueva para volver a intentarlo.';
    this.#finishTurnWithError(new Error('Codex app-server closed'));
  }

  #armTurnTimeout(generation: number, timeoutMs = this.#turnTimeoutMs): void {
    this.#clearTurnTimeout();
    this.#turnTimeout = setTimeout(() => {
      if (!this.#busy || this.#activeTurnGeneration !== generation) return;
      void this.#restartAfterTurnFailure(new Error(`turn timed out after ${timeoutMs}ms`));
    }, timeoutMs);
    this.#turnTimeout.unref();
  }

  #clearTurnTimeout(): void {
    if (this.#turnTimeout === null) return;
    clearTimeout(this.#turnTimeout);
    this.#turnTimeout = null;
  }

  async #restartAfterTurnFailure(error: unknown): Promise<void> {
    const appServer = this.#appServer;
    this.#appServer = null;
    this.#appServerGeneration += 1;
    this.#threadAttached = false;
    this.#availability = 'starting';
    appServer?.close();
    this.#finishTurnWithError(error);
    await this.#ensureReady();
  }

  #markTurnOutcome(outcome: TurnOutcome): void {
    if (this.#activeTurnGeneration === null) return;
    this.#turnOutcomes.set(this.#activeTurnGeneration, outcome);
    if (this.#turnOutcomes.size > 100) this.#turnOutcomes.delete(this.#turnOutcomes.keys().next().value ?? -1);
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

  #removeStateWatch(key: string): void {
    const watch = this.#stateWatches.get(key);
    if (watch === undefined) return;
    if (watch.timer !== undefined) clearTimeout(watch.timer);
    clearTimeout(watch.expiryTimer);
    watch.controller.abort();
    this.#stateWatches.delete(key);
  }

  #expireStateWatch(key: string, expected?: PendingStateWatch): void {
    const watch = this.#stateWatches.get(key);
    if (watch === undefined || (expected !== undefined && watch !== expected)) return;
    this.#removeStateWatch(key);
    const summary = `Studio dejó de vigilar ${watch.operation} porque no cambió de estado dentro del tiempo máximo.`;
    this.#addStateNote(summary);
    this.#appendMessage({ content: summary, role: 'system' });
    this.#publish();
    void this.#saveHistory();
  }

  #clearStateWatches(): void {
    for (const key of this.#stateWatches.keys()) this.#removeStateWatch(key);
  }

  #addStateNote(note: string): void {
    this.#stateNotes.push(note);
    if (this.#stateNotes.length > 5) this.#stateNotes.splice(0, this.#stateNotes.length - 5);
  }

  #publish(): void {
    if (this.#closed) return;
    this.#revision += 1;
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
    oneOf: [
      {
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
          operation: { enum: ['jobs.record', 'jobs.generate', 'renders.start'], type: 'string' },
          outro: { enum: ['loop', 'none'], type: 'string' },
          transition: { enum: ['cut', 'flash', 'whip', 'dip'], type: 'string' },
        },
        required: ['operation', 'job_id', 'format', 'hud', 'killfeed', 'effect', 'transition', 'counter', 'intro', 'outro', 'music', 'cover'],
        type: 'object',
      },
      {
        additionalProperties: false,
        properties: {
          captions: { description: 'Must match the current saved plan: disabled, reviewed Spanish words, or enabled automatic review.', enum: ['disabled', 'spanish-reviewed', 'spanish-auto-review'], type: 'string' },
          clip_selection: { enum: ['saved-edit-plan'], type: 'string' },
          cover: { enum: ['generated-gameplay-candidates', 'no-cover'], type: 'string' },
          format: { enum: ['short-9x16', 'landscape-16x9'], type: 'string' },
          framing: { description: 'Must match the current saved face crop.', enum: ['clean-crop', 'full-frame'], type: 'string' },
          killfeed: { description: 'Must match the current saved killfeed cues.', enum: ['none', 'preserve', 'synthetic'], type: 'string' },
          layout: { description: 'Exact stream variant returned by catalog.stream_variants.', type: 'string' },
          music: { description: 'Must match whether the current saved plan selects a music track.', enum: ['none', 'selected'], type: 'string' },
          operation: { enum: ['streams.start_render'], type: 'string' },
          stream_job_id: { type: 'string' },
          title: { description: 'Exact current saved clip-title summary, joined with " · "; use "Sin título" when no clip has a title.', maxLength: MAX_STREAM_BRIEF_TITLE_SUMMARY_LENGTH, minLength: 1, type: 'string' },
        },
        required: ['operation', 'stream_job_id', 'format', 'layout', 'clip_selection', 'title', 'framing', 'killfeed', 'captions', 'music', 'cover'],
        type: 'object',
      },
    ],
    type: 'object',
  };
}

function localMediaSchema(): JsonObject {
  return {
    additionalProperties: false,
    properties: {
      kind: { enum: ['demo', 'stream'], type: 'string' },
      target_steamid: { description: 'Optional SteamID64 when the demo target is already known.', pattern: '^[0-9]{1,20}$', type: 'string' },
      title: { description: 'Optional title for a stream recording.', maxLength: 160, minLength: 1, type: 'string' },
    },
    required: ['kind'],
    type: 'object',
  };
}

function watchStateSchema(): JsonObject {
  return {
    additionalProperties: false,
    properties: {
      arguments: { additionalProperties: true, type: 'object' },
      operation: { enum: ['jobs.get', 'renders.get', 'streams.get', 'streams.get_render'], type: 'string' },
    },
    required: ['operation', 'arguments'],
    type: 'object',
  };
}

function parseStateWatchRequest(value: unknown): { arguments: JsonObject; operation: StateWatchOperation } {
  const request = parseOperationToolInput(value);
  if (!STATE_WATCH_OPERATIONS.has(request.operation as StateWatchOperation)) throw new Error('operation cannot be watched');
  return { arguments: request.arguments, operation: request.operation as StateWatchOperation };
}

function parseLocalMediaRequest(value: unknown): LocalMediaRequest {
  if (!isJsonObject(value)) throw new Error('local media request must be an object');
  const keys = Object.keys(value);
  if (keys.some((key) => key !== 'kind' && key !== 'target_steamid' && key !== 'title')) {
    throw new Error('local media request contains an unknown field');
  }
  const kind = enumValue(value.kind, ['demo', 'stream'] as const, 'kind');
  if (kind === 'demo') {
    if (value.title !== undefined
      || (value.target_steamid !== undefined
        && (typeof value.target_steamid !== 'string' || !/^[0-9]{1,20}$/.test(value.target_steamid)))) {
      throw new Error('demo local media request is invalid');
    }
    return {
      kind,
      ...(value.target_steamid === undefined ? {} : { targetSteamID: value.target_steamid }),
    };
  }
  if (value.target_steamid !== undefined
    || (value.title !== undefined
      && (typeof value.title !== 'string' || value.title.trim() === '' || value.title.length > 160))) {
    throw new Error('stream local media request is invalid');
  }
  return {
    kind,
    ...(value.title === undefined ? {} : { title: value.title.trim() }),
  };
}

function parseCreativeBrief(value: unknown): ParsedCreativeBrief {
  if (!isJsonObject(value)) throw new Error('creative brief must be an object');
  const operation = enumValue(value.operation, ['jobs.record', 'jobs.generate', 'renders.start', 'streams.start_render'] as const, 'operation');
  const format = enumValue(value.format, ['short-9x16', 'landscape-16x9'] as const, 'format');
  const music = enumValue(value.music, ['none', 'selected'] as const, 'music');
  const cover = enumValue(value.cover, ['generated-gameplay-candidates', 'no-cover'] as const, 'cover');
  if (operation === 'streams.start_render') {
    const allowed = new Set([
      'captions', 'clip_selection', 'cover', 'format', 'framing', 'killfeed', 'layout', 'music', 'operation', 'stream_job_id', 'title',
    ]);
    if (Object.keys(value).some((key) => !allowed.has(key))) throw new Error('stream creative brief contains an unknown field');
    if (!isSafeReference(value.stream_job_id) || !isSafeReference(value.layout)) throw new Error('stream creative brief target or layout is invalid');
    if (typeof value.title !== 'string'
      || value.title.trim() === ''
      || value.title.length > MAX_STREAM_BRIEF_TITLE_SUMMARY_LENGTH) throw new Error('stream creative brief title is invalid');
    return {
      captions: enumValue(value.captions, ['disabled', 'spanish-reviewed', 'spanish-auto-review'] as const, 'captions'),
      clipSelection: enumValue(value.clip_selection, ['saved-edit-plan'] as const, 'clip_selection'),
      cover,
      format,
      framing: enumValue(value.framing, ['clean-crop', 'full-frame'] as const, 'framing'),
      killfeed: enumValue(value.killfeed, ['none', 'preserve', 'synthetic'] as const, 'killfeed'),
      layout: value.layout,
      music,
      operation,
      targetID: value.stream_job_id,
      targetKind: 'stream-job',
      title: value.title.trim(),
    };
  }
  const allowed = new Set([
    'counter', 'cover', 'effect', 'format', 'hud', 'intro', 'job_id', 'killfeed', 'music', 'operation', 'outro', 'transition',
  ]);
  if (Object.keys(value).some((key) => !allowed.has(key))) throw new Error('demo creative brief contains an unknown field');
  if (!isSafeReference(value.job_id)) throw new Error('demo creative brief job is invalid');
  return {
    cover,
    counter: enumValue(value.counter, ['on', 'off'] as const, 'counter'),
    effect: enumValue(value.effect, ['clean', 'punch-in', 'velocity', 'freeze-flash'] as const, 'effect'),
    format,
    hud: enumValue(value.hud, ['full-game-ui', 'clean-hudless'] as const, 'hud'),
    intro: enumValue(value.intro, ['hook', 'none'] as const, 'intro'),
    killfeed: enumValue(value.killfeed, ['preserve', 'synthetic'] as const, 'killfeed'),
    music,
    operation,
    outro: enumValue(value.outro, ['loop', 'none'] as const, 'outro'),
    targetID: value.job_id,
    targetKind: 'job',
    transition: enumValue(value.transition, ['cut', 'flash', 'whip', 'dip'] as const, 'transition'),
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
    ...(stream && typeof variant === 'string' ? { layout: variant } : {}),
    operation,
    targetID,
    targetKind: stream ? 'stream-job' : 'job',
  };
}

function creativeBriefKey(brief: Pick<CreativeBriefRequirement, 'operation' | 'targetID' | 'targetKind'>): string {
  return `${brief.operation}:${brief.targetKind}:${brief.targetID}`;
}

function bindStreamCreativeBrief(input: StreamCreativeBriefInput, plan: JsonObject): StreamCreativeBrief {
  const layout = typeof plan.variant === 'string' ? plan.variant : '';
  const format = streamPlanFormat(layout);
  const title = streamPlanTitle(plan);
  const framing = streamPlanFraming(plan, layout);
  const captions = streamPlanCaptions(plan);
  const killfeed = streamPlanKillfeed(plan);
  const music = streamPlanMusic(plan);
  const planUpdatedAt = typeof plan.updated_at === 'string' ? plan.updated_at : '';
  if (layout === ''
    || format === undefined
    || input.format !== format
    || input.layout !== layout
    || input.title !== title
    || input.framing !== framing
    || captions === undefined
    || input.captions !== captions
    || input.killfeed !== killfeed
    || input.music !== music
    || planUpdatedAt === '') {
    throw new Error('stream creative brief does not match the current saved edit plan');
  }
  const fingerprint = streamPlanFingerprint(plan);
  return {
    ...input,
    planFingerprint: fingerprint,
    planPreviewFields: streamPlanPreviewFields(plan, fingerprint, framing, captions, killfeed, music),
    planUpdatedAt,
  };
}

function streamPlanFingerprint(plan: JsonObject): string {
  return createHash('sha256').update(stableJson(plan)).digest('hex');
}

function stableJson(value: JsonValue): string {
  if (value === null || typeof value !== 'object') return JSON.stringify(value);
  if (Array.isArray(value)) return `[${value.map((item) => stableJson(item)).join(',')}]`;
  return `{${Object.keys(value).sort().map((key) => `${JSON.stringify(key)}:${stableJson(value[key] ?? null)}`).join(',')}}`;
}

function streamPlanClips(plan: JsonObject): JsonObject[] {
  return Array.isArray(plan.clips) ? plan.clips.filter(isJsonObject) : [];
}

function streamPlanTitle(plan: JsonObject): string {
  const clips = streamPlanClips(plan);
  const titles = clips
    .map((clip) => typeof clip.title === 'string' ? clip.title.trim() : '')
    .filter((title) => title !== '');
  const summary = titles.length === 0 ? 'Sin título' : titles.join(' · ');
  if (summary.length > MAX_STREAM_BRIEF_TITLE_SUMMARY_LENGTH) {
    throw new Error('stream clip titles do not fit in one exact creative brief');
  }
  return summary;
}

function streamPlanFraming(plan: JsonObject, layout: string): StreamCreativeBriefFraming {
  if (layout === 'streamer-fullframe-nocam' || layout === 'streamer-landscape-16x9') return 'full-frame';
  const crop = isJsonObject(plan.face_crop) ? plan.face_crop : undefined;
  return typeof crop?.width === 'number' && crop.width > 0 && typeof crop.height === 'number' && crop.height > 0
    ? 'clean-crop'
    : 'full-frame';
}

function streamPlanFormat(layout: string): CreativeBriefFormat | undefined {
  if (layout === 'streamer-landscape-16x9') return 'landscape-16x9';
  if (layout === 'streamer-vertical-stack-40-60'
    || layout === 'streamer-vertical-stack'
    || layout === 'streamer-fullframe-nocam') return 'short-9x16';
  return undefined;
}

function streamPlanCaptions(plan: JsonObject): StreamCreativeBriefCaptions | undefined {
  const captions = isJsonObject(plan.captions) ? plan.captions : undefined;
  if (captions?.enabled !== true) return 'disabled';
  if (captions.language !== undefined && captions.language !== 'es') return undefined;
  const clips = streamPlanClips(plan);
  const reviewed = clips.length > 0 && clips.every((clip) => {
    if (clip.caption_reviewed === true) return true;
    if (Object.hasOwn(clip, 'caption_reviewed')) return false;
    return Array.isArray(clip.caption_words) && clip.caption_words.length > 0;
  });
  return reviewed ? 'spanish-reviewed' : 'spanish-auto-review';
}

function streamPlanKillfeed(plan: JsonObject): StreamCreativeBriefKillfeed {
  const clips = streamPlanClips(plan);
  const hasSynthetic = clips.some((clip) => Array.isArray(clip.killfeed_kills)
    && clip.killfeed_kills.some((kills) => Array.isArray(kills) && kills.length > 0));
  if (hasSynthetic) return 'synthetic';
  const hasPreserved = clips.some((clip) => Array.isArray(clip.killfeed_seconds) && clip.killfeed_seconds.length > 0);
  return hasPreserved ? 'preserve' : 'none';
}

function streamPlanMusic(plan: JsonObject): CreativeBriefMusic {
  const music = isJsonObject(plan.music) ? plan.music : undefined;
  return typeof music?.key === 'string' && music.key !== '' ? 'selected' : 'none';
}

function streamPlanPreviewFields(
  plan: JsonObject,
  fingerprint: string,
  framing: StreamCreativeBriefFraming,
  captions: StreamCreativeBriefCaptions,
  killfeed: StreamCreativeBriefKillfeed,
  music: CreativeBriefMusic,
): AssistantActionPreviewField[] {
  const clips = streamPlanClips(plan);
  if (clips.length > MAX_STREAM_BRIEF_CLIPS) {
    throw new Error(`stream edit plan has ${clips.length} clips; at most ${MAX_STREAM_BRIEF_CLIPS} fit in one exact approval card`);
  }
  const clipFields = clips.length === 0
    ? [{ label: 'Cortes · 0', value: 'Sin clips' }]
    : clips.map((clip, index): AssistantActionPreviewField => {
        const id = typeof clip.id === 'string' ? clip.id : '?';
        const start = typeof clip.start_seconds === 'number' ? clip.start_seconds : '?';
        const end = typeof clip.end_seconds === 'number' ? clip.end_seconds : '?';
        const clipTitle = typeof clip.title === 'string' && clip.title.trim() !== '' ? clip.title.trim() : 'Sin título';
        const value = `${id} · ${start}-${end}s · ${clipTitle}`;
        if (value.length > MAX_REVIEWABLE_ACTION_STRING_LENGTH) {
          throw new Error(`stream clip ${id} does not fit in one exact approval field`);
        }
        return { label: `Corte ${index + 1} de ${clips.length}`, value };
      });
  const planMusic = isJsonObject(plan.music) ? plan.music : undefined;
  const musicValue = music === 'selected'
    ? `${String(planMusic?.key)} · volumen ${String(planMusic?.volume ?? 'predeterminado')}`
    : 'none';
  const fields: AssistantActionPreviewField[] = [
    { label: 'Revisión del plan', value: fingerprint.slice(0, 16) },
    { label: 'Layout', value: String(plan.variant) },
    ...clipFields,
    { label: 'Encuadre', value: framing },
    { label: 'Killfeed', value: killfeed },
    { label: 'Subtítulos', value: captions },
    { label: 'Música', value: musicValue },
  ];
  if (fields.length > MAX_REVIEWABLE_ACTION_FIELDS - 4
    || fields.some((field) => field.value.length > MAX_REVIEWABLE_ACTION_STRING_LENGTH)) {
    throw new Error('stream edit plan does not fit in one exact approval card');
  }
  return fields;
}

function creativeBriefPreview(brief: CreativeBrief): AssistantActionPreview {
  if (brief.targetKind === 'stream-job') {
    return {
      fields: [
        { label: 'Acción', value: brief.operation },
        { label: 'Stream', value: brief.targetID },
        { label: 'Formato', value: brief.format },
        { label: 'Portada', value: brief.cover },
        ...brief.planPreviewFields,
      ],
    };
  }
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

function invalidatesStreamCreativeBrief(operation: string): boolean {
  return operation === 'streams.update_edit_plan'
    || operation === 'streams.configure_captions'
    || operation === 'streams.review_caption_candidates'
    || operation === 'streams.edit_clip'
    || operation === 'streams.apply_killfeed_analysis';
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
  if (value.category !== undefined && !['artifacts', 'catalog', 'jobs', 'renders', 'streams', 'studio', 'voices'].includes(String(value.category))) {
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

function executionForModel(outcome: Extract<OperationGatewayOutcome, { kind: 'executed' }>): JsonObject {
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

function creativeBriefContext(brief: CreativeBrief): AssistantContext {
  if (brief.targetKind === 'stream-job') {
    return {
      kind: 'stream',
      label: 'Clip de stream actual',
      pathname: '/streams',
      streamJobId: brief.targetID,
    };
  }
  return {
    jobId: brief.targetID,
    kind: 'demo',
    label: 'Demo actual',
    pathname: `/matches/${brief.targetID}`,
  };
}

function creativeBriefContinuationPrompt(brief: CreativeBrief): string {
  const approvedValues = brief.targetKind === 'stream-job'
    ? `format=${brief.format}; layout=${brief.layout}; clip_selection=${brief.clipSelection}; title=${brief.title}; framing=${brief.framing}; killfeed=${brief.killfeed}; captions=${brief.captions}; music=${brief.music}; cover=${brief.cover}`
    : `format=${brief.format}; hud=${brief.hud}; killfeed=${brief.killfeed}; effect=${brief.effect}; transition=${brief.transition}; counter=${brief.counter}; intro=${brief.intro}; outro=${brief.outro}; music=${brief.music}; cover=${brief.cover}`;
  return [
    `Studio event: The user approved the creative brief for ${brief.operation} on ${brief.targetKind} ${brief.targetID}.`,
    `Approved values: ${approvedValues}.`,
    `Continue the existing workflow now: search again if needed, then call fragforge.preview for that exact ${brief.operation} action with the target, selected moments, and choices already established in this conversation.`,
    'Do not ask the user to repeat the creative approval. Do not claim execution; the preview must create the separate final Studio approval card.',
  ].join(' ');
}

function operationContinuationContext(
  operation: string,
  argumentsValue: JsonObject,
  result: JsonValue | undefined,
  fallback: AssistantContext,
): AssistantContext {
  const explicitStreamID = safeReferenceFrom(argumentsValue.stream_job_id);
  const resultObject = isJsonObject(result) ? result : undefined;
  const nestedJob = resultObject !== undefined && isJsonObject(resultObject.job) ? resultObject.job : undefined;
  const streamID = explicitStreamID
    ?? (operation.startsWith('streams.') ? safeReferenceFrom(nestedJob?.id) ?? safeReferenceFrom(resultObject?.id) : undefined);
  if (streamID !== undefined) {
    return {
      kind: 'stream',
      label: 'Clip de stream actual',
      pathname: '/streams',
      streamJobId: streamID,
    };
  }

  const explicitJobID = safeReferenceFrom(argumentsValue.job_id);
  const jobID = explicitJobID
    ?? (operation === 'jobs.create' ? safeReferenceFrom(resultObject?.id) : undefined);
  if (jobID !== undefined) {
    return {
      jobId: jobID,
      kind: 'demo',
      label: 'Demo actual',
      pathname: `/matches/${jobID}`,
    };
  }
  return { ...fallback };
}

function operationContinuationPrompt(
  operation: string,
  outcome: Extract<OperationGatewayOutcome, { kind: 'executed' }>,
): string {
  const safeResult = JSON.stringify(executionForModel(outcome));
  const boundedResult = safeResult.length <= MAX_OPERATION_CONTINUATION_RESULT_LENGTH
    ? safeResult
    : `${safeResult.slice(0, MAX_OPERATION_CONTINUATION_RESULT_LENGTH)}…[truncated]`;
  return [
    `Studio event: The user approved ${operation}, and Studio accepted it successfully.`,
    `Safe execution result: ${boundedResult}.`,
    'Continue owning the workflow now. Search or read the new current state yourself, use returned IDs instead of asking the user for them, and perform every safe next step.',
    'If another write, costly, or destructive step is needed, prepare its exact Studio approval card. If background work is still running, report its state and next expected step without retrying it.',
  ].join(' ');
}

function safeReferenceFrom(value: JsonValue | undefined): string | undefined {
  return isSafeReference(value) ? value : undefined;
}

function stateWatchKey(operation: StateWatchOperation, argumentsValue: JsonObject): string {
  return `${operation}:${stableJson(argumentsValue)}`;
}

function watchedState(value: JsonValue, depth = 0): string | undefined {
  if (!isJsonObject(value) || depth >= 4) return undefined;
  for (const key of ['status', 'state', 'phase']) {
    const candidate = value[key];
    if (typeof candidate === 'string' && candidate.length > 0 && candidate.length <= 128) return candidate;
  }
  for (const key of ['job', 'render', 'result']) {
    const nested = value[key];
    if (isJsonObject(nested)) {
      const state = watchedState(nested, depth + 1);
      if (state !== undefined) return state;
    }
  }
  return undefined;
}

function stateWatchContinuationPrompt(
  operation: StateWatchOperation,
  previousState: string,
  nextState: string,
  outcome: Extract<OperationGatewayOutcome, { kind: 'executed' }>,
): string {
  const safeResult = JSON.stringify(executionForModel(outcome));
  const boundedResult = safeResult.length <= MAX_OPERATION_CONTINUATION_RESULT_LENGTH
    ? safeResult
    : `${safeResult.slice(0, MAX_OPERATION_CONTINUATION_RESULT_LENGTH)}…[truncated]`;
  return [
    `Studio event: The background watch for ${operation} changed from ${previousState} to ${nextState}.`,
    `Safe current result: ${boundedResult}.`,
    'Resume the full workflow autonomously from this state. Perform safe reads yourself and prepare the next exact approval when needed.',
    'If work is still running with no actionable next step, register watch_state again. Never repeat the operation that produced this state.',
  ].join(' ');
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

function positiveDuration(value: number, label: string): number {
  if (!Number.isSafeInteger(value) || value <= 0) throw new Error(`${label} must be a positive integer`);
  return value;
}
