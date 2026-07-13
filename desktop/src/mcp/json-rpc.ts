import { Buffer } from 'node:buffer';
import type { Readable, Writable } from 'node:stream';
import { StringDecoder } from 'node:string_decoder';

export type JsonRpcId = string | number;

export interface JsonRpcRequest {
  jsonrpc: '2.0';
  id: JsonRpcId;
  method: string;
  params?: unknown;
}

export interface JsonRpcNotification {
  jsonrpc: '2.0';
  method: string;
  params?: unknown;
}

export interface JsonRpcErrorObject {
  code: number;
  message: string;
  data?: unknown;
}

export type JsonRpcRequestHandler = (request: JsonRpcRequest) => void | Promise<void>;
export type JsonRpcNotificationHandler = (
  notification: JsonRpcNotification,
) => void | Promise<void>;
export type JsonRpcConnectionErrorHandler = (error: Error) => void;
export type JsonRpcConnectionCloseHandler = (reason: Error) => void;

export interface JsonRpcConnectionOptions {
  input: Readable;
  output: Writable;
  requestHandler?: JsonRpcRequestHandler;
  notificationHandler?: JsonRpcNotificationHandler;
  errorHandler?: JsonRpcConnectionErrorHandler;
  closeHandler?: JsonRpcConnectionCloseHandler;
  maxFrameBytes?: number;
}

interface PendingRequest {
  resolve: (result: unknown) => void;
  reject: (error: Error) => void;
}

interface JsonRpcResponse {
  jsonrpc: '2.0';
  id: JsonRpcId;
  result?: unknown;
  error?: JsonRpcErrorObject;
}

type ProtocolErrorKind = 'frame_too_large' | 'parse' | 'invalid_request' | 'invalid_response' | 'transport';

const JSON_RPC_VERSION = '2.0';
const PARSE_ERROR_CODE = -32_700;
const INVALID_REQUEST_CODE = -32_600;
const METHOD_NOT_FOUND_CODE = -32_601;
const DEFAULT_MAX_FRAME_BYTES = 1024 * 1024;

/** A JSON-RPC error response returned by the remote peer. */
export class JsonRpcResponseError extends Error {
  readonly code: number;
  readonly data: unknown;

  constructor(error: JsonRpcErrorObject) {
    super(error.message);
    this.name = 'JsonRpcResponseError';
    this.code = error.code;
    this.data = error.data;
  }
}

/** Raised when an operation is attempted after the transport has closed. */
export class JsonRpcConnectionClosedError extends Error {
  constructor(message = 'json-rpc connection closed') {
    super(message);
    this.name = 'JsonRpcConnectionClosedError';
  }
}

/** Raised when the caller cancels an outgoing JSON-RPC request. */
export class JsonRpcRequestCancelledError extends Error {
  constructor(message = 'json-rpc request cancelled') {
    super(message);
    this.name = 'JsonRpcRequestCancelledError';
  }
}

/** Reports malformed peer messages and stream failures to the server host. */
export class JsonRpcProtocolError extends Error {
  readonly kind: ProtocolErrorKind;
  readonly cause: unknown;

  constructor(kind: ProtocolErrorKind, message: string, cause?: unknown) {
    super(message);
    this.name = 'JsonRpcProtocolError';
    this.kind = kind;
    this.cause = cause;
  }
}

/**
 * A dependency-free, newline-delimited JSON-RPC 2.0 connection for MCP stdio.
 *
 * The supplied output is protocol-only: this class writes one compact JSON
 * object followed by `\n` and never emits logs or other text. Callers should
 * direct diagnostics to stderr through `errorHandler`.
 */
export class JsonRpcConnection {
  private readonly input: Readable;
  private readonly output: Writable;
  private readonly decoder = new StringDecoder('utf8');
  private readonly pendingRequests = new Map<JsonRpcId, PendingRequest>();
  private requestHandler: JsonRpcRequestHandler | undefined;
  private notificationHandler: JsonRpcNotificationHandler | undefined;
  private errorHandler: JsonRpcConnectionErrorHandler | undefined;
  private closeHandler: JsonRpcConnectionCloseHandler | undefined;
  private readonly maxFrameBytes: number;
  private buffer = '';
  private currentFrameBytes = 0;
  private nextRequestId = 1;
  private started = false;
  private isClosed = false;

  constructor(options: JsonRpcConnectionOptions) {
    this.input = options.input;
    this.output = options.output;
    this.requestHandler = options.requestHandler;
    this.notificationHandler = options.notificationHandler;
    this.errorHandler = options.errorHandler;
    this.closeHandler = options.closeHandler;
    this.maxFrameBytes = positiveInteger(options.maxFrameBytes ?? DEFAULT_MAX_FRAME_BYTES, 'maxFrameBytes');
  }

  get closed(): boolean {
    return this.isClosed;
  }

  setRequestHandler(handler: JsonRpcRequestHandler | undefined): void {
    this.requestHandler = handler;
  }

  setNotificationHandler(handler: JsonRpcNotificationHandler | undefined): void {
    this.notificationHandler = handler;
  }

  setErrorHandler(handler: JsonRpcConnectionErrorHandler | undefined): void {
    this.errorHandler = handler;
  }

  /** Starts consuming the readable. Calling start more than once is harmless. */
  start(): void {
    if (this.started || this.isClosed) return;
    this.started = true;
    this.input.on('data', this.handleData);
    this.input.once('end', this.handleInputEnd);
    this.input.once('close', this.handleInputClose);
    this.input.once('error', this.handleInputError);
    this.output.once('close', this.handleOutputClose);
    this.output.once('error', this.handleOutputError);
  }

  async sendResult(id: JsonRpcId, result: unknown): Promise<void> {
    await this.writeMessage({
      jsonrpc: JSON_RPC_VERSION,
      id,
      result: result === undefined ? null : result,
    });
  }

  async sendError(
    id: JsonRpcId | null,
    code: number,
    message: string,
    data?: unknown,
  ): Promise<void> {
    const error: JsonRpcErrorObject = data === undefined
      ? { code, message }
      : { code, message, data };
    await this.writeMessage({ jsonrpc: JSON_RPC_VERSION, id, error });
  }

  async sendNotification(method: string, params?: unknown): Promise<void> {
    const notification = params === undefined
      ? { jsonrpc: JSON_RPC_VERSION, method }
      : { jsonrpc: JSON_RPC_VERSION, method, params };
    await this.writeMessage(notification);
  }

  /** Sends a request to the peer and resolves when its matching response arrives. */
  sendRequest(method: string, params?: unknown, signal?: AbortSignal): Promise<unknown> {
    if (this.isClosed) return Promise.reject(new JsonRpcConnectionClosedError());
    if (signal?.aborted) return Promise.reject(new JsonRpcRequestCancelledError());
    const id = this.nextRequestId;
    this.nextRequestId += 1;

    return new Promise((resolve, reject) => {
      const cleanup = (): void => signal?.removeEventListener('abort', handleAbort);
      const handleAbort = (): void => {
        const pending = this.pendingRequests.get(id);
        if (pending === undefined) return;
        this.pendingRequests.delete(id);
        cleanup();
        pending.reject(new JsonRpcRequestCancelledError());
        void this.sendNotification('notifications/cancelled', {
          reason: 'caller cancelled request',
          requestId: id,
        }).catch((error: unknown) => this.reportError(toError(error)));
      };
      this.pendingRequests.set(id, {
        reject: (error) => {
          cleanup();
          reject(error);
        },
        resolve: (result) => {
          cleanup();
          resolve(result);
        },
      });
      signal?.addEventListener('abort', handleAbort, { once: true });
      const request = params === undefined
        ? { jsonrpc: JSON_RPC_VERSION, id, method }
        : { jsonrpc: JSON_RPC_VERSION, id, method, params };
      void this.writeMessage(request).catch((error: unknown) => {
        const pending = this.pendingRequests.get(id);
        if (pending === undefined) return;
        this.pendingRequests.delete(id);
        pending.reject(toError(error));
      });
    });
  }

  /** Stops the connection without ending streams owned by the caller. */
  close(reason: Error = new JsonRpcConnectionClosedError()): void {
    if (this.isClosed) return;
    this.isClosed = true;
    this.detachStreamListeners();
    this.buffer = '';
    this.currentFrameBytes = 0;
    for (const pending of this.pendingRequests.values()) pending.reject(reason);
    this.pendingRequests.clear();
    const handler = this.closeHandler;
    this.closeHandler = undefined;
    if (handler !== undefined) {
      try {
        handler(reason);
      } catch (error) {
        this.reportError(toError(error));
      }
    }
  }

  private readonly handleData = (chunk: unknown): void => {
    if (this.isClosed) return;
    let bytes: Buffer;
    if (typeof chunk === 'string') {
      bytes = Buffer.from(chunk, 'utf8');
    } else if (Buffer.isBuffer(chunk) || chunk instanceof Uint8Array) {
      bytes = Buffer.from(chunk);
    } else {
      this.reportError(new JsonRpcProtocolError('transport', 'stdio produced a non-text chunk'));
      return;
    }
    if (!this.acceptFrameBytes(bytes)) return;
    this.buffer += typeof chunk === 'string' ? chunk : this.decoder.write(bytes);
    this.drainFrames();
  };

  private readonly handleInputEnd = (): void => {
    if (this.isClosed) return;
    this.buffer += this.decoder.end();
    const trailing = trimFrameEnding(this.buffer);
    this.buffer = '';
    if (trailing.trim() !== '') this.handleFrame(trailing);
    this.close(new JsonRpcConnectionClosedError('json-rpc input ended'));
  };

  private readonly handleInputClose = (): void => {
    this.close(new JsonRpcConnectionClosedError('json-rpc input closed'));
  };

  private readonly handleOutputClose = (): void => {
    this.close(new JsonRpcConnectionClosedError('json-rpc output closed'));
  };

  private readonly handleInputError = (error: Error): void => {
    this.reportError(new JsonRpcProtocolError('transport', 'json-rpc input failed', error));
    this.close(error);
  };

  private readonly handleOutputError = (error: Error): void => {
    this.reportError(new JsonRpcProtocolError('transport', 'json-rpc output failed', error));
    this.close(error);
  };

  private drainFrames(): void {
    let newline = this.buffer.indexOf('\n');
    while (newline >= 0) {
      const frame = trimFrameEnding(this.buffer.slice(0, newline));
      this.buffer = this.buffer.slice(newline + 1);
      if (frame.trim() !== '') this.handleFrame(frame);
      newline = this.buffer.indexOf('\n');
    }
  }

  private acceptFrameBytes(bytes: Buffer): boolean {
    for (const byte of bytes) {
      if (byte === 0x0a) {
        this.currentFrameBytes = 0;
        continue;
      }
      this.currentFrameBytes += 1;
      if (this.currentFrameBytes <= this.maxFrameBytes) continue;
      const error = new JsonRpcProtocolError(
        'frame_too_large',
        `json-rpc frame exceeds the ${this.maxFrameBytes} byte limit`,
      );
      this.reportError(error);
      const response = this.sendError(null, INVALID_REQUEST_CODE, 'Invalid Request', {
        reason: 'message exceeds the configured size limit',
      });
      this.close(error);
      void response.catch((writeError: unknown) => this.reportError(toError(writeError)));
      return false;
    }
    return true;
  }

  private handleFrame(frame: string): void {
    let value: unknown;
    try {
      value = JSON.parse(frame) as unknown;
    } catch (error) {
      this.reportError(new JsonRpcProtocolError('parse', 'could not parse json-rpc message', error));
      void this.sendError(null, PARSE_ERROR_CODE, 'Parse error').catch((writeError: unknown) => {
        this.reportError(toError(writeError));
      });
      return;
    }

    if (looksLikeResponse(value)) {
      this.handleResponse(value);
      return;
    }
    this.handleCall(value);
  }

  private handleCall(value: unknown): void {
    const call = parseCall(value);
    if (call === null) {
      this.reportError(new JsonRpcProtocolError('invalid_request', 'received an invalid json-rpc request'));
      void this.sendError(errorResponseId(value), INVALID_REQUEST_CODE, 'Invalid Request').catch(
        (writeError: unknown) => this.reportError(toError(writeError)),
      );
      return;
    }

    if ('id' in call) {
      const handler = this.requestHandler;
      if (handler === undefined) {
        void this.sendError(call.id, METHOD_NOT_FOUND_CODE, 'Method not found').catch(
          (writeError: unknown) => this.reportError(toError(writeError)),
        );
        return;
      }
      invokeHandler(() => handler(call), (error) => this.reportError(error));
      return;
    }

    const handler = this.notificationHandler;
    if (handler !== undefined) {
      invokeHandler(() => handler(call), (error) => this.reportError(error));
    }
  }

  private handleResponse(value: unknown): void {
    const response = parseResponse(value);
    if (response === null) {
      const error = new JsonRpcProtocolError('invalid_response', 'received an invalid json-rpc response');
      this.rejectResponseIfPending(value, error);
      this.reportError(error);
      return;
    }

    const pending = this.pendingRequests.get(response.id);
    if (pending === undefined) {
      this.reportError(
        new JsonRpcProtocolError(
          'invalid_response',
          `received a response for unknown request id ${String(response.id)}`,
        ),
      );
      return;
    }
    this.pendingRequests.delete(response.id);
    if (response.error !== undefined) {
      pending.reject(new JsonRpcResponseError(response.error));
      return;
    }
    pending.resolve(response.result);
  }

  private rejectResponseIfPending(value: unknown, error: Error): void {
    if (!isRecord(value) || !isJsonRpcId(value.id)) return;
    const pending = this.pendingRequests.get(value.id);
    if (pending === undefined) return;
    this.pendingRequests.delete(value.id);
    pending.reject(error);
  }

  private writeMessage(message: unknown): Promise<void> {
    if (this.isClosed) return Promise.reject(new JsonRpcConnectionClosedError());
    let frame: string;
    try {
      frame = `${JSON.stringify(message)}\n`;
    } catch (error) {
      return Promise.reject(toError(error));
    }

    return new Promise((resolve, reject) => {
      this.output.write(frame, 'utf8', (error: Error | null | undefined) => {
        if (error) {
          reject(error);
          return;
        }
        resolve();
      });
    });
  }

  private reportError(error: Error): void {
    const handler = this.errorHandler;
    if (handler === undefined) return;
    try {
      handler(error);
    } catch {
      // Error reporting must never contaminate stdout or break protocol handling.
    }
  }

  private detachStreamListeners(): void {
    this.input.off('data', this.handleData);
    this.input.off('end', this.handleInputEnd);
    this.input.off('close', this.handleInputClose);
    this.input.off('error', this.handleInputError);
    this.output.off('close', this.handleOutputClose);
    this.output.off('error', this.handleOutputError);
  }
}

function parseCall(value: unknown): JsonRpcRequest | JsonRpcNotification | null {
  if (!isRecord(value) || value.jsonrpc !== JSON_RPC_VERSION || typeof value.method !== 'string') {
    return null;
  }
  if ('result' in value || 'error' in value) return null;
  if ('params' in value && !isStructuredValue(value.params)) return null;
  const params = value.params;
  if ('id' in value) {
    if (!isJsonRpcId(value.id)) return null;
    return params === undefined
      ? { jsonrpc: JSON_RPC_VERSION, id: value.id, method: value.method }
      : { jsonrpc: JSON_RPC_VERSION, id: value.id, method: value.method, params };
  }
  return params === undefined
    ? { jsonrpc: JSON_RPC_VERSION, method: value.method }
    : { jsonrpc: JSON_RPC_VERSION, method: value.method, params };
}

function parseResponse(value: unknown): JsonRpcResponse | null {
  if (!isRecord(value) || value.jsonrpc !== JSON_RPC_VERSION || !isJsonRpcId(value.id)) return null;
  if ('method' in value) return null;
  const hasResult = 'result' in value;
  const hasError = 'error' in value;
  if (hasResult === hasError) return null;
  if (hasError) {
    const error = parseErrorObject(value.error);
    return error === null ? null : { jsonrpc: JSON_RPC_VERSION, id: value.id, error };
  }
  return { jsonrpc: JSON_RPC_VERSION, id: value.id, result: value.result };
}

function parseErrorObject(value: unknown): JsonRpcErrorObject | null {
  if (!isRecord(value)) return null;
  const { code, message } = value;
  if (typeof code !== 'number' || !Number.isInteger(code) || typeof message !== 'string') {
    return null;
  }
  return 'data' in value
    ? { code, message, data: value.data }
    : { code, message };
}

function looksLikeResponse(value: unknown): boolean {
  return isRecord(value) && !('method' in value) && ('result' in value || 'error' in value);
}

function errorResponseId(value: unknown): JsonRpcId | null {
  return isRecord(value) && isJsonRpcId(value.id) ? value.id : null;
}

function isJsonRpcId(value: unknown): value is JsonRpcId {
  return typeof value === 'string'
    || (typeof value === 'number' && Number.isFinite(value));
}

function isStructuredValue(value: unknown): boolean {
  return typeof value === 'object' && value !== null;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function trimFrameEnding(frame: string): string {
  return frame.endsWith('\r') ? frame.slice(0, -1) : frame;
}

function invokeHandler(run: () => void | Promise<void>, onError: (error: Error) => void): void {
  try {
    void Promise.resolve(run()).catch((error: unknown) => onError(toError(error)));
  } catch (error) {
    onError(toError(error));
  }
}

function toError(value: unknown): Error {
  return value instanceof Error ? value : new Error(String(value));
}

function positiveInteger(value: number, label: string): number {
  if (!Number.isSafeInteger(value) || value <= 0) throw new Error(`${label} must be a positive integer`);
  return value;
}
