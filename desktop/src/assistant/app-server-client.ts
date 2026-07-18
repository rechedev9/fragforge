import { Buffer } from 'node:buffer';
import { StringDecoder } from 'node:string_decoder';
import {
  launchCodexAppServer,
  type AppServerLaunchOptions,
  type AppServerTransport,
  type AppServerTransportLauncher,
} from './app-server-transport.ts';

export type AppServerRequestID = string | number;
export type AppServerStatus = 'starting' | 'ready' | 'failed' | 'closed';
export type AppServerApprovalPolicy = 'untrusted' | 'on-request' | 'never';
export type AppServerSandbox = 'read-only' | 'workspace-write' | 'danger-full-access';
export type AppServerJsonValue = null | boolean | number | string | AppServerJsonValue[] | {
  [key: string]: AppServerJsonValue;
};

export interface AppServerClientInfo {
  name: string;
  title: string;
  version: string;
}

export interface AppServerDynamicToolFunction {
  type: 'function';
  name: string;
  description: string;
  inputSchema: AppServerJsonValue;
  deferLoading?: boolean;
}

export interface AppServerDynamicToolNamespace {
  type: 'namespace';
  name: string;
  description: string;
  tools: AppServerDynamicToolFunction[];
}

export type AppServerDynamicTool = AppServerDynamicToolFunction | AppServerDynamicToolNamespace;

export interface AppServerThread {
  id: string;
  sessionId: string;
  [key: string]: unknown;
}

export interface AppServerTurn {
  id: string;
  status: string;
  [key: string]: unknown;
}

export interface AppServerNotification {
  method: string;
  params: unknown;
}

export interface AppServerAgentMessageDelta {
  threadId: string;
  turnId: string;
  itemId: string;
  delta: string;
}

export interface AppServerTurnCompletedEvent {
  threadId: string;
  turn: AppServerTurn;
}

export type AppServerTurnStartedEvent = AppServerTurnCompletedEvent;

export interface AppServerDynamicToolCall {
  requestId: AppServerRequestID;
  threadId: string;
  turnId: string;
  callId: string;
  namespace: string | null;
  tool: string;
  arguments: AppServerJsonValue;
}

export interface AppServerDynamicToolContentItem {
  type: 'inputText' | 'inputImage';
  text?: string;
  imageUrl?: string;
}

export interface AppServerDynamicToolResult {
  success: boolean;
  contentItems: AppServerDynamicToolContentItem[];
}

export interface AppServerStartThreadOptions {
  approvalPolicy?: AppServerApprovalPolicy;
  baseInstructions?: string;
  cwd?: string;
  developerInstructions?: string;
  dynamicTools?: AppServerDynamicTool[];
  model?: string;
  personality?: string;
  sandbox?: AppServerSandbox;
  serviceName?: string;
}

export interface AppServerResumeThreadOptions {
  approvalPolicy?: AppServerApprovalPolicy;
  baseInstructions?: string;
  cwd?: string;
  developerInstructions?: string;
  excludeTurns?: boolean;
  model?: string;
  personality?: string;
  sandbox?: AppServerSandbox;
}

export interface AppServerStartTurnOptions {
  clientUserMessageId?: string;
  cwd?: string;
  model?: string;
}

export interface CodexAppServerClientOptions extends AppServerLaunchOptions {
  /** Supply a connected test or alternate transport instead of starting Codex. */
  transport?: AppServerTransport;
  /** Defaults to launchCodexAppServer when no transport is supplied. */
  launch?: AppServerTransportLauncher;
  clientInfo?: AppServerClientInfo;
  dynamicTools?: AppServerDynamicTool[];
  dynamicToolTimeoutMs?: number;
  maxFrameBytes?: number;
  onAgentMessageDelta?: (delta: AppServerAgentMessageDelta) => void;
  onDiagnostic?: (message: string) => void;
  onDynamicToolCall?: (call: AppServerDynamicToolCall, signal: AbortSignal) => Promise<AppServerDynamicToolResult>;
  onError?: (error: Error) => void;
  onNotification?: (notification: AppServerNotification) => void;
  onStatus?: (status: AppServerStatus) => void;
  onTurnCompleted?: (event: AppServerTurnCompletedEvent) => void;
  onTurnStarted?: (event: AppServerTurnStartedEvent) => void;
  requestTimeoutMs?: number;
}

/** The controller-facing contract; tests can supply a lightweight fake. */
export interface CodexAppServer {
  readonly closed: boolean;
  readonly status: AppServerStatus;
  close(): void;
  initialize(): Promise<void>;
  interruptTurn(threadId: string, turnId: string): Promise<void>;
  resumeThread(threadId: string, options?: AppServerResumeThreadOptions): Promise<AppServerThread>;
  startThread(options?: AppServerStartThreadOptions): Promise<AppServerThread>;
  startTurn(threadId: string, text: string, options?: AppServerStartTurnOptions): Promise<AppServerTurn>;
}

export type CodexAppServerFactory = (options: CodexAppServerClientOptions) => CodexAppServer;

interface PendingRequest {
  reject: (reason: Error) => void;
  resolve: (value: unknown) => void;
  timeout: NodeJS.Timeout;
}

interface AppServerErrorObject {
  code: number;
  data?: unknown;
  message: string;
}

interface AppServerResponse {
  error?: AppServerErrorObject;
  id: AppServerRequestID;
  result?: unknown;
}

const DEFAULT_MAX_FRAME_BYTES = 1024 * 1024;
const DEFAULT_DYNAMIC_TOOL_TIMEOUT_MS = 35_000;
const DEFAULT_REQUEST_TIMEOUT_MS = 15_000;
const DEFAULT_CLIENT_INFO: AppServerClientInfo = {
  name: 'fragforge_studio',
  title: 'FragForge Studio',
  version: '0.0.0',
};
const METHOD_NOT_FOUND = -32_601;
const INVALID_REQUEST = -32_600;

/** A JSON-RPC response error sent by codex app-server. */
export class AppServerResponseError extends Error {
  readonly code: number;
  readonly data: unknown;

  constructor(error: AppServerErrorObject) {
    super(error.message);
    this.name = 'AppServerResponseError';
    this.code = error.code;
    this.data = error.data;
  }
}

/** A protocol error from the line-delimited app-server transport. */
export class AppServerProtocolError extends Error {
  readonly cause: unknown;

  constructor(message: string, cause: unknown = undefined) {
    super(message);
    this.name = 'AppServerProtocolError';
    this.cause = cause;
  }
}

/** An operation was attempted after the app-server transport stopped. */
export class AppServerClosedError extends Error {
  constructor(message = 'codex app-server is closed') {
    super(message);
    this.name = 'AppServerClosedError';
  }
}

/** An app-server RPC did not acknowledge before its bounded deadline. */
export class AppServerRequestTimeoutError extends Error {
  constructor(method: string, timeoutMs: number) {
    super(`${method} timed out after ${timeoutMs}ms`);
    this.name = 'AppServerRequestTimeoutError';
  }
}

class AppServerDynamicToolTimeoutError extends Error {
  constructor() {
    super('Dynamic tool call timed out.');
    this.name = 'AppServerDynamicToolTimeoutError';
  }
}

/**
 * A narrow client for the documented `codex app-server` JSONL protocol.
 *
 * The app-server intentionally omits the `jsonrpc` field on the wire, so this
 * client does too while still accepting a versioned peer message for forward
 * compatibility. It never exposes a generic renderer-controlled RPC surface.
 */
export class CodexAppServerClient implements CodexAppServer {
  readonly #transport: AppServerTransport;
  readonly #clientInfo: AppServerClientInfo;
  readonly #defaultDynamicTools: AppServerDynamicTool[];
  readonly #dynamicToolTimeoutMs: number;
  readonly #maxFrameBytes: number;
  readonly #onAgentMessageDelta: ((delta: AppServerAgentMessageDelta) => void) | undefined;
  readonly #onDiagnostic: ((message: string) => void) | undefined;
  readonly #onDynamicToolCall: ((call: AppServerDynamicToolCall, signal: AbortSignal) => Promise<AppServerDynamicToolResult>) | undefined;
  readonly #onError: ((error: Error) => void) | undefined;
  readonly #onNotification: ((notification: AppServerNotification) => void) | undefined;
  readonly #onStatus: ((status: AppServerStatus) => void) | undefined;
  readonly #onTurnCompleted: ((event: AppServerTurnCompletedEvent) => void) | undefined;
  readonly #onTurnStarted: ((event: AppServerTurnStartedEvent) => void) | undefined;
  readonly #requestTimeoutMs: number;
  readonly #decoder = new StringDecoder('utf8');
  readonly #dynamicToolControllers = new Set<AbortController>();
  readonly #pending = new Map<AppServerRequestID, PendingRequest>();
  #buffer = '';
  #currentFrameBytes = 0;
  #initialization: Promise<void> | null = null;
  #nextRequestID = 1;
  #status: AppServerStatus = 'starting';

  constructor(options: CodexAppServerClientOptions = {}) {
    if (options.transport !== undefined && options.launch !== undefined) {
      throw new Error('transport and launch cannot both be supplied');
    }
    this.#transport = options.transport
      ?? (options.launch ?? launchCodexAppServer)({
        args: options.args,
        command: options.command,
        cwd: options.cwd,
        env: options.env,
      });
    this.#clientInfo = { ...DEFAULT_CLIENT_INFO, ...options.clientInfo };
    this.#defaultDynamicTools = options.dynamicTools ?? [];
    this.#dynamicToolTimeoutMs = positiveInteger(options.dynamicToolTimeoutMs ?? DEFAULT_DYNAMIC_TOOL_TIMEOUT_MS, 'dynamicToolTimeoutMs');
    this.#maxFrameBytes = positiveInteger(options.maxFrameBytes ?? DEFAULT_MAX_FRAME_BYTES, 'maxFrameBytes');
    this.#onAgentMessageDelta = options.onAgentMessageDelta;
    this.#onDiagnostic = options.onDiagnostic;
    this.#onDynamicToolCall = options.onDynamicToolCall;
    this.#onError = options.onError;
    this.#onNotification = options.onNotification;
    this.#onStatus = options.onStatus;
    this.#onTurnCompleted = options.onTurnCompleted;
    this.#onTurnStarted = options.onTurnStarted;
    this.#requestTimeoutMs = positiveInteger(options.requestTimeoutMs ?? DEFAULT_REQUEST_TIMEOUT_MS, 'requestTimeoutMs');

    this.#transport.onData((chunk) => this.#handleData(chunk));
    this.#transport.onDiagnostic((chunk) => this.#emitDiagnostic(toText(chunk)));
    this.#transport.onError((error) => this.#fail(error));
    this.#transport.onClose((reason) => this.#handleClose(reason));
    this.#emitStatus();
  }

  get status(): AppServerStatus {
    return this.#status;
  }

  get closed(): boolean {
    return this.#status === 'closed' || this.#status === 'failed';
  }

  /** Completes the required initialize -> initialized handshake once per transport. */
  initialize(): Promise<void> {
    if (this.#status === 'ready') return Promise.resolve();
    if (this.closed) return Promise.reject(new AppServerClosedError());
    if (this.#initialization !== null) return this.#initialization;
    this.#initialization = this.#initialize();
    return this.#initialization;
  }

  async startThread(options: AppServerStartThreadOptions = {}): Promise<AppServerThread> {
    await this.initialize();
    const result = await this.#request('thread/start', threadStartParams(options, this.#defaultDynamicTools));
    return threadFromResult(result, 'thread/start');
  }

  async resumeThread(threadId: string, options: AppServerResumeThreadOptions = {}): Promise<AppServerThread> {
    await this.initialize();
    if (!isNonEmptyString(threadId)) throw new Error('threadId is required');
    const result = await this.#request('thread/resume', threadResumeParams(threadId, options));
    return threadFromResult(result, 'thread/resume');
  }

  async startTurn(threadId: string, text: string, options: AppServerStartTurnOptions = {}): Promise<AppServerTurn> {
    await this.initialize();
    if (!isNonEmptyString(threadId)) throw new Error('threadId is required');
    if (!isNonEmptyString(text)) throw new Error('turn text is required');
    const params: Record<string, unknown> = {
      input: [{ text, text_elements: [], type: 'text' }],
      threadId,
    };
    addOptional(params, 'clientUserMessageId', options.clientUserMessageId);
    addOptional(params, 'cwd', options.cwd);
    addOptional(params, 'model', options.model);
    const result = await this.#request('turn/start', params);
    if (!isRecord(result) || !isTurn(result.turn)) throw new AppServerProtocolError('turn/start response is missing turn');
    return result.turn;
  }

  async interruptTurn(threadId: string, turnId: string): Promise<void> {
    await this.initialize();
    if (!isNonEmptyString(threadId)) throw new Error('threadId is required');
    if (!isNonEmptyString(turnId)) throw new Error('turnId is required');
    await this.#request('turn/interrupt', { threadId, turnId });
  }

  /** Stops the child transport and rejects every outstanding app-server request. */
  close(): void {
    if (this.#status === 'closed') return;
    this.#close(new AppServerClosedError('codex app-server client closed'), this.#status !== 'failed');
    this.#transport.close();
  }

  async #initialize(): Promise<void> {
    try {
      await this.#request('initialize', {
        capabilities: {
          experimentalApi: true,
          requestAttestation: false,
        },
        clientInfo: this.#clientInfo,
      });
      await this.#notify('initialized', {});
      this.#setStatus('ready');
    } catch (error) {
      this.#fail(toError(error));
      this.#transport.close();
      throw error;
    }
  }

  #request(method: string, params?: unknown): Promise<unknown> {
    if (this.closed) return Promise.reject(new AppServerClosedError());
    const id = this.#nextRequestID;
    this.#nextRequestID += 1;
    return new Promise((resolve, reject) => {
      const timeout = setTimeout(() => {
        const pending = this.#pending.get(id);
        if (pending === undefined) return;
        this.#pending.delete(id);
        const failure = new AppServerRequestTimeoutError(method, this.#requestTimeoutMs);
        pending.reject(failure);
        this.#fail(failure);
        this.#transport.close();
      }, this.#requestTimeoutMs);
      timeout.unref();
      this.#pending.set(id, { reject, resolve, timeout });
      const message = params === undefined ? { id, method } : { id, method, params };
      void this.#write(message).catch((error: unknown) => {
        const pending = this.#pending.get(id);
        if (pending === undefined) return;
        this.#pending.delete(id);
        clearTimeout(pending.timeout);
        const failure = toError(error);
        pending.reject(failure);
        this.#fail(failure);
        this.#transport.close();
      });
    });
  }

  #notify(method: string, params?: unknown): Promise<void> {
    const message = params === undefined ? { method } : { method, params };
    return this.#write(message);
  }

  async #write(message: unknown): Promise<void> {
    if (this.closed) throw new AppServerClosedError();
    let frame: string;
    try {
      frame = `${JSON.stringify(message)}\n`;
    } catch (error) {
      throw toError(error);
    }
    await this.#transport.write(frame);
  }

  #handleData(chunk: Buffer | string): void {
    if (this.closed) return;
    const bytes = typeof chunk === 'string' ? Buffer.from(chunk, 'utf8') : Buffer.from(chunk);
    if (!this.#acceptFrameBytes(bytes)) return;
    this.#buffer += typeof chunk === 'string' ? chunk : this.#decoder.write(bytes);
    this.#drainFrames();
  }

  #acceptFrameBytes(bytes: Buffer): boolean {
    for (const byte of bytes) {
      if (byte === 0x0a) {
        this.#currentFrameBytes = 0;
        continue;
      }
      this.#currentFrameBytes += 1;
      if (this.#currentFrameBytes <= this.#maxFrameBytes) continue;
      const error = new AppServerProtocolError(`app-server frame exceeds the ${this.#maxFrameBytes} byte limit`);
      this.#protocolFailure(error);
      return false;
    }
    return true;
  }

  #drainFrames(): void {
    let newline = this.#buffer.indexOf('\n');
    while (newline >= 0) {
      const frame = trimCarriageReturn(this.#buffer.slice(0, newline));
      this.#buffer = this.#buffer.slice(newline + 1);
      if (frame.trim() !== '') this.#handleFrame(frame);
      newline = this.#buffer.indexOf('\n');
    }
  }

  #handleFrame(frame: string): void {
    let message: unknown;
    try {
      message = JSON.parse(frame) as unknown;
    } catch (error) {
      this.#protocolFailure(new AppServerProtocolError('could not parse app-server message', error));
      return;
    }
    if (isResponse(message)) {
      this.#handleResponse(message);
      return;
    }
    if (isCall(message)) {
      if ('id' in message) {
        void this.#handleServerRequest(message).catch((error: unknown) => this.#protocolFailure(toError(error)));
      } else {
        this.#handleNotification(message);
      }
      return;
    }
    this.#protocolFailure(new AppServerProtocolError('received an invalid app-server message'));
  }

  #handleResponse(response: AppServerResponse): void {
    const pending = this.#pending.get(response.id);
    if (pending === undefined) {
      this.#protocolFailure(new AppServerProtocolError(`received a response for unknown request id ${String(response.id)}`));
      return;
    }
    this.#pending.delete(response.id);
    clearTimeout(pending.timeout);
    if (response.error !== undefined) {
      pending.reject(new AppServerResponseError(response.error));
      return;
    }
    pending.resolve(response.result);
  }

  async #handleServerRequest(request: AppServerServerRequest): Promise<void> {
    if (request.method !== 'item/tool/call') {
      await this.#sendError(request.id, METHOD_NOT_FOUND, 'Method not found');
      return;
    }
    const call = parseDynamicToolCall(request.id, request.params);
    if (call === null) {
      await this.#sendError(request.id, INVALID_REQUEST, 'Invalid Request');
      return;
    }
    const handler = this.#onDynamicToolCall;
    if (handler === undefined) {
      await this.#sendResult(request.id, toolFailure('FragForge Studio has no handler for dynamic tool calls.'));
      return;
    }
    const controller = new AbortController();
    this.#dynamicToolControllers.add(controller);
    try {
      await this.#sendResult(request.id, await withTimeout(
        handler(call, controller.signal),
        this.#dynamicToolTimeoutMs,
        () => new AppServerDynamicToolTimeoutError(),
      ));
    } catch (error) {
      if (error instanceof AppServerDynamicToolTimeoutError) {
        controller.abort(error);
        await this.#sendResult(request.id, toolFailure(error.message));
        return;
      }
      this.#emitError(toError(error));
      await this.#sendResult(request.id, toolFailure(`Dynamic tool call failed: ${errorMessage(error)}`));
    } finally {
      this.#dynamicToolControllers.delete(controller);
    }
  }

  #handleNotification(notification: AppServerNotification): void {
    this.#invoke(() => this.#onNotification?.(notification));
    if (notification.method === 'item/agentMessage/delta') {
      const delta = parseAgentMessageDelta(notification.params);
      if (delta !== null) this.#invoke(() => this.#onAgentMessageDelta?.(delta));
      return;
    }
    if (notification.method === 'turn/completed') {
      const completed = parseTurnCompleted(notification.params);
      if (completed !== null) this.#invoke(() => this.#onTurnCompleted?.(completed));
      return;
    }
    if (notification.method === 'turn/started') {
      const started = parseTurnCompleted(notification.params);
      if (started !== null) this.#invoke(() => this.#onTurnStarted?.(started));
    }
  }

  #sendResult(id: AppServerRequestID, result: unknown): Promise<void> {
    return this.#write({ id, result });
  }

  #sendError(id: AppServerRequestID, code: number, message: string): Promise<void> {
    return this.#write({ error: { code, message }, id });
  }

  #handleClose(reason: Error): void {
    if (this.#status === 'closed') return;
    if (this.#status !== 'failed') this.#flushTrailingFrame();
    const closed = reason instanceof AppServerClosedError
      ? reason
      : new AppServerClosedError(reason.message);
    this.#close(closed, this.#status !== 'failed');
  }

  #flushTrailingFrame(): void {
    this.#buffer += this.#decoder.end();
    const trailing = trimCarriageReturn(this.#buffer);
    this.#buffer = '';
    if (trailing.trim() !== '') this.#handleFrame(trailing);
  }

  #close(reason: Error, transitionToClosed = true): void {
    if (this.#status === 'closed') return;
    this.#buffer = '';
    this.#currentFrameBytes = 0;
    this.#abortDynamicTools(reason);
    for (const pending of this.#pending.values()) {
      clearTimeout(pending.timeout);
      pending.reject(reason);
    }
    this.#pending.clear();
    if (transitionToClosed) this.#setStatus('closed');
  }

  #fail(error: Error): void {
    if (this.#status === 'failed' || this.#status === 'closed') return;
    this.#abortDynamicTools(error);
    for (const pending of this.#pending.values()) {
      clearTimeout(pending.timeout);
      pending.reject(error);
    }
    this.#pending.clear();
    this.#setStatus('failed');
    this.#emitError(error);
  }

  #protocolFailure(error: Error): void {
    this.#fail(error);
    this.#transport.close();
  }

  #abortDynamicTools(reason: Error): void {
    for (const controller of this.#dynamicToolControllers) controller.abort(reason);
    this.#dynamicToolControllers.clear();
  }

  #setStatus(status: AppServerStatus): void {
    if (this.#status === status) return;
    this.#status = status;
    this.#emitStatus();
  }

  #emitStatus(): void {
    this.#invoke(() => this.#onStatus?.(this.#status));
  }

  #emitDiagnostic(message: string): void {
    this.#invoke(() => this.#onDiagnostic?.(message));
  }

  #emitError(error: Error): void {
    this.#invoke(() => this.#onError?.(error));
  }

  #invoke(run: () => void): void {
    try {
      run();
    } catch (error) {
      // Callbacks are host UI hooks; a bad callback must not corrupt the protocol.
      if (error instanceof Error && error !== undefined) {
        try {
          this.#onError?.(error);
        } catch {
          // An error reporter must itself be isolated from the protocol loop.
        }
      }
    }
  }
}

export function createCodexAppServer(options: CodexAppServerClientOptions = {}): CodexAppServer {
  return new CodexAppServerClient(options);
}

interface AppServerServerRequest {
  id: AppServerRequestID;
  method: string;
  params: unknown;
}

function threadStartParams(
  options: AppServerStartThreadOptions,
  defaultDynamicTools: AppServerDynamicTool[],
): Record<string, unknown> {
  const params: Record<string, unknown> = {};
  addOptional(params, 'approvalPolicy', options.approvalPolicy ?? 'never');
  addOptional(params, 'baseInstructions', options.baseInstructions);
  addOptional(params, 'cwd', options.cwd);
  addOptional(params, 'developerInstructions', options.developerInstructions);
  addOptional(params, 'model', options.model);
  addOptional(params, 'personality', options.personality);
  addOptional(params, 'sandbox', options.sandbox ?? 'read-only');
  addOptional(params, 'serviceName', options.serviceName ?? 'fragforge_studio');
  addDynamicTools(params, options.dynamicTools ?? defaultDynamicTools);
  return params;
}

function threadResumeParams(
  threadId: string,
  options: AppServerResumeThreadOptions,
): Record<string, unknown> {
  const params: Record<string, unknown> = { threadId };
  addOptional(params, 'approvalPolicy', options.approvalPolicy ?? 'never');
  addOptional(params, 'baseInstructions', options.baseInstructions);
  addOptional(params, 'cwd', options.cwd);
  addOptional(params, 'developerInstructions', options.developerInstructions);
  addOptional(params, 'excludeTurns', options.excludeTurns);
  addOptional(params, 'model', options.model);
  addOptional(params, 'personality', options.personality);
  addOptional(params, 'sandbox', options.sandbox ?? 'read-only');
  return params;
}

function addDynamicTools(params: Record<string, unknown>, tools: AppServerDynamicTool[]): void {
  if (tools.length > 0) params.dynamicTools = tools;
}

function addOptional(params: Record<string, unknown>, key: string, value: unknown): void {
  if (value !== undefined) params[key] = value;
}

function threadFromResult(result: unknown, method: string): AppServerThread {
  if (!isRecord(result) || !isThread(result.thread)) {
    throw new AppServerProtocolError(`${method} response is missing thread`);
  }
  return result.thread;
}

function parseAgentMessageDelta(value: unknown): AppServerAgentMessageDelta | null {
  if (!isRecord(value)
    || !isNonEmptyString(value.threadId)
    || !isNonEmptyString(value.turnId)
    || !isNonEmptyString(value.itemId)
    || typeof value.delta !== 'string') return null;
  return {
    delta: value.delta,
    itemId: value.itemId,
    threadId: value.threadId,
    turnId: value.turnId,
  };
}

function parseTurnCompleted(value: unknown): AppServerTurnCompletedEvent | null {
  if (!isRecord(value) || !isNonEmptyString(value.threadId) || !isTurn(value.turn)) return null;
  return { threadId: value.threadId, turn: value.turn };
}

function parseDynamicToolCall(
  requestId: AppServerRequestID,
  value: unknown,
): AppServerDynamicToolCall | null {
  if (!isRecord(value)
    || !isNonEmptyString(value.threadId)
    || !isNonEmptyString(value.turnId)
    || !isNonEmptyString(value.callId)
    || !isNonEmptyString(value.tool)
    || (value.namespace !== null && typeof value.namespace !== 'string')
    || !isJsonValue(value.arguments)) return null;
  return {
    arguments: value.arguments,
    callId: value.callId,
    namespace: value.namespace,
    requestId,
    threadId: value.threadId,
    tool: value.tool,
    turnId: value.turnId,
  };
}

function toolFailure(message: string): AppServerDynamicToolResult {
  return {
    contentItems: [{ text: message, type: 'inputText' }],
    success: false,
  };
}

function isResponse(value: unknown): value is AppServerResponse {
  if (!isRecord(value) || !isRequestID(value.id) || typeof value.method === 'string') return false;
  const hasResult = Object.hasOwn(value, 'result');
  const hasError = Object.hasOwn(value, 'error');
  if (hasResult === hasError) return false;
  if (!hasError) return true;
  return isErrorObject(value.error);
}

function isCall(value: unknown): value is AppServerServerRequest | AppServerNotification {
  if (!isRecord(value) || typeof value.method !== 'string') return false;
  if (Object.hasOwn(value, 'result') || Object.hasOwn(value, 'error')) return false;
  if (Object.hasOwn(value, 'id')) return isRequestID(value.id);
  return true;
}

function isErrorObject(value: unknown): value is AppServerErrorObject {
  return isRecord(value) && typeof value.code === 'number' && typeof value.message === 'string';
}

function isThread(value: unknown): value is AppServerThread {
  return isRecord(value) && isNonEmptyString(value.id) && isNonEmptyString(value.sessionId);
}

function isTurn(value: unknown): value is AppServerTurn {
  return isRecord(value) && isNonEmptyString(value.id) && typeof value.status === 'string';
}

function isJsonValue(value: unknown): value is AppServerJsonValue {
  if (value === null || typeof value === 'boolean' || typeof value === 'number' || typeof value === 'string') return true;
  if (Array.isArray(value)) return value.every(isJsonValue);
  return isRecord(value) && Object.values(value).every(isJsonValue);
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function isRequestID(value: unknown): value is AppServerRequestID {
  return typeof value === 'string' || (typeof value === 'number' && Number.isFinite(value));
}

function isNonEmptyString(value: unknown): value is string {
  return typeof value === 'string' && value.trim() !== '';
}

function positiveInteger(value: number, label: string): number {
  if (!Number.isSafeInteger(value) || value <= 0) throw new Error(`${label} must be a positive integer`);
  return value;
}

function trimCarriageReturn(value: string): string {
  return value.endsWith('\r') ? value.slice(0, -1) : value;
}

function toText(value: Buffer | string): string {
  return typeof value === 'string' ? value : value.toString('utf8');
}

function toError(value: unknown): Error {
  return value instanceof Error ? value : new Error(String(value));
}

function errorMessage(value: unknown): string {
  return value instanceof Error ? value.message : String(value);
}

function withTimeout<T>(promise: Promise<T>, timeoutMs: number, timeoutError: () => Error): Promise<T> {
  return new Promise<T>((resolve, reject) => {
    const timeout = setTimeout(() => reject(timeoutError()), timeoutMs);
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
