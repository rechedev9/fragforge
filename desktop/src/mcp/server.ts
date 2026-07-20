import type { Readable, Writable } from 'node:stream';
import { searchOperationCatalog, operationCatalogResource, parseSearchRequest } from './discovery.ts';
import {
  JsonRpcConnection,
  JsonRpcRequestCancelledError,
  type JsonRpcId,
  type JsonRpcNotification,
  type JsonRpcRequest,
} from './json-rpc.ts';
import { isJsonObject, type JsonObject, type JsonValue } from './json.ts';
import {
  MissingOperationInputError,
  operationNamed,
  validateJsonSchema,
  validateOperationInput,
  type OperationDefinition,
} from './operations.ts';
import { OperationGateway } from '../studio-operations/operation-gateway.ts';
import { OrchestratorClient } from './orchestrator-client.ts';

const LATEST_PROTOCOL_VERSION = '2025-11-25';
const SUPPORTED_PROTOCOL_VERSIONS = new Set([
  LATEST_PROTOCOL_VERSION,
  '2025-06-18',
  '2025-03-26',
  '2024-11-05',
]);
const INVALID_REQUEST = -32_600;
const INVALID_PARAMS = -32_602;
const METHOD_NOT_FOUND = -32_601;
const INTERNAL_ERROR = -32_603;
const SERVER_BUSY = -32_001;
const DEFAULT_ELICITATION_TIMEOUT_MS = 60_000;
const DEFAULT_MAX_CONCURRENT_REQUESTS = 8;

const SEARCH_INPUT_SCHEMA: JsonObject = {
  additionalProperties: false,
  properties: {
    arguments: {
      additionalProperties: true,
      description: 'Partial arguments used to discover dependent live inputs, such as roster players for a job_id.',
      type: 'object',
    },
    category: { enum: ['artifacts', 'catalog', 'jobs', 'renders', 'streams', 'studio', 'voices'], type: 'string' },
    include_dynamic_inputs: { default: true, type: 'boolean' },
    limit: { default: 8, maximum: 20, minimum: 1, type: 'integer' },
    operation: { description: 'Exact operation name when its schema is already known.', type: 'string' },
    query: { description: 'Natural-language capability search.', type: 'string' },
    risk: { enum: ['read', 'write', 'costly', 'destructive'], type: 'string' },
  },
  type: 'object',
};

const EXECUTE_INPUT_SCHEMA: JsonObject = {
  additionalProperties: false,
  properties: {
    arguments: { additionalProperties: true, default: {}, type: 'object' },
    confirmed: {
      default: false,
      description: 'Must be true together with mode=apply for write, costly, or destructive operations.',
      type: 'boolean',
    },
    mode: {
      default: 'preview',
      description: 'Read operations execute immediately. Mutations preview by default and run only with apply plus confirmed=true.',
      enum: ['preview', 'apply'],
      type: 'string',
    },
    operation: { description: 'Exact allowlisted operation name returned by search.', type: 'string' },
  },
  required: ['operation'],
  type: 'object',
};

const TOOLS: JsonObject[] = [
  {
    annotations: { destructiveHint: false, idempotentHint: true, openWorldHint: false, readOnlyHint: true },
    description: 'Search the FragForge operation catalog. Returns exact JSON input schemas plus live IDs/options from Studio. Call this before execute and pass partial arguments to resolve dependent inputs.',
    inputSchema: SEARCH_INPUT_SCHEMA,
    name: 'search',
    title: 'Search FragForge operations',
  },
  {
    annotations: { destructiveHint: true, idempotentHint: false, openWorldHint: true, readOnlyHint: false },
    description: 'Preview or execute one exact allowlisted FragForge operation. Validates against the schema returned by search; mutations require mode=apply and confirmed=true.',
    inputSchema: EXECUTE_INPUT_SCHEMA,
    name: 'execute',
    title: 'Execute a FragForge operation',
  },
];

export interface FragForgeMcpServerOptions {
  client: OrchestratorClient;
  diagnostics?: Writable;
  input: Readable;
  onClose?: (reason: Error) => void;
  output: Writable;
  serverVersion: string;
  elicitationTimeoutMs?: number;
  maxConcurrentRequests?: number;
}

export class FragForgeMcpServer {
  readonly #client: OrchestratorClient;
  readonly #connection: JsonRpcConnection;
  readonly #diagnostics: Writable | undefined;
  readonly #elicitationTimeoutMs: number;
  readonly #maxConcurrentRequests: number;
  readonly #operationGateway: OperationGateway;
  readonly #serverVersion: string;
  readonly #activeRequests = new Map<JsonRpcId, AbortController>();
  readonly #inFlightRequestIds = new Set<JsonRpcId>();
  #initializeNegotiated = false;
  #ready = false;
  #supportsFormElicitation = false;

  constructor(options: FragForgeMcpServerOptions) {
    this.#client = options.client;
    this.#operationGateway = new OperationGateway({ client: options.client });
    this.#diagnostics = options.diagnostics;
    this.#elicitationTimeoutMs = boundedInteger(
      options.elicitationTimeoutMs ?? DEFAULT_ELICITATION_TIMEOUT_MS,
      'elicitationTimeoutMs',
      10,
      300_000,
    );
    this.#maxConcurrentRequests = boundedInteger(
      options.maxConcurrentRequests ?? DEFAULT_MAX_CONCURRENT_REQUESTS,
      'maxConcurrentRequests',
      1,
      64,
    );
    this.#serverVersion = options.serverVersion;
    this.#connection = new JsonRpcConnection({
      closeHandler: (reason) => {
        this.#abortActiveRequests();
        options.onClose?.(reason);
      },
      errorHandler: (error) => this.#writeDiagnostic(error),
      input: options.input,
      notificationHandler: (notification) => this.#handleNotification(notification),
      output: options.output,
      requestHandler: (request) => this.#handleRequest(request),
    });
  }

  get closed(): boolean {
    return this.#connection.closed;
  }

  start(): void {
    this.#connection.start();
  }

  close(): void {
    this.#abortActiveRequests();
    this.#connection.close();
  }

  async #handleRequest(request: JsonRpcRequest): Promise<void> {
    if (this.#inFlightRequestIds.has(request.id)) {
      try {
        await this.#connection.sendError(request.id, INVALID_REQUEST, 'request id is already active');
      } finally {
        // Reusing an outstanding id makes any later response ambiguous. Close
        // after one protocol error so the original request cannot reply again.
        this.close();
      }
      return;
    }
    this.#inFlightRequestIds.add(request.id);
    try {
      if (request.method === 'initialize') {
        await this.#initialize(request);
        return;
      }
      if (request.method === 'ping') {
        await this.#connection.sendResult(request.id, {});
        return;
      }
      if (!this.#ready) {
        await this.#connection.sendError(request.id, INVALID_PARAMS, 'server is not initialized');
        return;
      }
      if (request.method === 'tools/list') {
        await this.#connection.sendResult(request.id, { tools: TOOLS });
        return;
      }
      if (request.method === 'tools/call') {
        await this.#callTool(request);
        return;
      }
      if (request.method === 'resources/list') {
        await this.#connection.sendResult(request.id, {
          resources: [
            { description: 'Static allowlisted operations and their schemas.', mimeType: 'application/json', name: 'FragForge operation catalog', uri: 'fragforge://catalog' },
            { description: 'Live local Studio health and media capabilities.', mimeType: 'application/json', name: 'FragForge Studio status', uri: 'fragforge://status' },
          ],
        });
        return;
      }
      if (request.method === 'resources/read') {
        await this.#readResource(request);
        return;
      }
      if (request.method === 'resources/templates/list') {
        await this.#connection.sendResult(request.id, { resourceTemplates: [] });
        return;
      }
      await this.#connection.sendError(request.id, METHOD_NOT_FOUND, 'Method not found');
    } catch (error: unknown) {
      if (this.#connection.closed) return;
      // MCP cancellation is fire-and-forget: once cancellation wins the race,
      // stop work and deliberately leave the original request unanswered.
      if (isCancellation(error)) return;
      this.#writeDiagnostic(error);
      await this.#connection.sendError(request.id, INTERNAL_ERROR, 'Internal error');
    } finally {
      this.#inFlightRequestIds.delete(request.id);
    }
  }

  async #initialize(request: JsonRpcRequest): Promise<void> {
    if (this.#initializeNegotiated) {
      await this.#connection.sendError(request.id, INVALID_PARAMS, 'initialize may only be called once');
      return;
    }
    if (!isJsonObject(request.params)) {
      await this.#connection.sendError(request.id, INVALID_PARAMS, 'initialize params must be an object');
      return;
    }
    const requestedVersion = request.params.protocolVersion;
    const capabilities = request.params.capabilities;
    const clientInfo = request.params.clientInfo;
    if (typeof requestedVersion !== 'string' || !isJsonObject(capabilities) || !isJsonObject(clientInfo)
      || typeof clientInfo.name !== 'string' || typeof clientInfo.version !== 'string') {
      await this.#connection.sendError(request.id, INVALID_PARAMS, 'initialize requires protocolVersion, capabilities, and clientInfo');
      return;
    }
    const protocolVersion = SUPPORTED_PROTOCOL_VERSIONS.has(requestedVersion)
      ? requestedVersion
      : LATEST_PROTOCOL_VERSION;
    const elicitation = isJsonObject(capabilities) ? capabilities.elicitation : undefined;
    this.#supportsFormElicitation = supportsFormElicitation(protocolVersion, elicitation);
    this.#initializeNegotiated = true;
    await this.#connection.sendResult(request.id, {
      capabilities: {
        resources: {},
        tools: {},
      },
      instructions: 'Search before execute. Use the returned input_schema and live dynamic_inputs; never invent job IDs, SteamIDs, segment IDs, variants, songs, or artifact names. Read operations execute directly. Every mutation defaults to preview and requires mode=apply with confirmed=true. Prefer studio.status before costly capture/render actions. Binary artifacts are returned as loopback URLs, never embedded in model context. After cancelling streams.create_from_file, call streams.list and then streams.get before retrying so a completed upload is not duplicated.',
      protocolVersion,
      serverInfo: {
        name: 'fragforge-studio',
        title: 'FragForge Studio',
        version: this.#serverVersion,
      },
    });
  }

  async #callTool(request: JsonRpcRequest): Promise<void> {
    if (!isJsonObject(request.params) || typeof request.params.name !== 'string') {
      await this.#connection.sendError(request.id, INVALID_PARAMS, 'tools/call requires a tool name');
      return;
    }
    const toolArguments = request.params.arguments === undefined ? {} : request.params.arguments;
    if (!isJsonObject(toolArguments)) {
      await this.#connection.sendError(request.id, INVALID_PARAMS, 'tool arguments must be an object');
      return;
    }
    if (request.params.name !== 'search' && request.params.name !== 'execute') {
      await this.#connection.sendError(request.id, INVALID_PARAMS, `Unknown tool: ${request.params.name}`);
      return;
    }
    if (this.#activeRequests.size >= this.#maxConcurrentRequests) {
      await this.#connection.sendError(request.id, SERVER_BUSY, 'FragForge MCP is busy; retry later');
      return;
    }
    const controller = new AbortController();
    this.#activeRequests.set(request.id, controller);
    try {
      const result = request.params.name === 'search'
        ? await this.#search(toolArguments, controller.signal)
        : await this.#execute(toolArguments, controller.signal);
      if (controller.signal.aborted) throw new JsonRpcRequestCancelledError();
      await this.#connection.sendResult(request.id, result);
    } finally {
      this.#activeRequests.delete(request.id);
    }
  }

  async #search(input: JsonObject, signal: AbortSignal): Promise<JsonObject> {
    try {
      validateJsonSchema(SEARCH_INPUT_SCHEMA, input, 'search');
      const result = await searchOperationCatalog(this.#client, parseSearchRequest(input), signal);
      return toolSuccess(result);
    } catch (error: unknown) {
      if (signal.aborted || isCancellation(error)) throw new JsonRpcRequestCancelledError();
      return toolError(errorMessage(error));
    }
  }

  async #execute(input: JsonObject, signal: AbortSignal): Promise<JsonObject> {
    try {
      validateJsonSchema(EXECUTE_INPUT_SCHEMA, input, 'execute');
      const operationName = input.operation;
      if (typeof operationName !== 'string' || operationName === '') throw new Error('operation is required');
      const suppliedArguments = input.arguments === undefined ? {} : input.arguments;
      if (!isJsonObject(suppliedArguments)) throw new Error('arguments must be an object');
      const mode = input.mode === undefined ? 'preview' : input.mode;
      if (mode !== 'preview' && mode !== 'apply') throw new Error('mode must be preview or apply');

      const outcome = await this.#operationGateway.execute({
        arguments: suppliedArguments,
        operation: operationName,
      }, {
        completeInput: (operation, completeInput, completeSignal) => this.#completeMissingInputs(operation, completeInput, completeSignal ?? signal),
        privileged: mode === 'apply' && input.confirmed === true,
        signal,
      });
      if (outcome.kind === 'preview') {
        if (mode === 'apply') {
          throw new Error(`${outcome.operation} is ${outcome.risk}; set mode=apply and confirmed=true only after user approval`);
        }
        return toolSuccess({
          operation: outcome.operation,
          preview: outcome.preview,
          requires_confirmation: true,
          risk: outcome.risk,
          status: 'preview',
        });
      }
      const response: JsonObject = { operation: outcome.operation, result: outcome.result, status: outcome.status };
      if (outcome.partialFailure) {
        response.error = outcome.error ?? 'operation was cancelled after a durable partial result was created';
        return toolFailure(response);
      }
      return toolSuccess(response);
    } catch (error: unknown) {
      if (signal.aborted || isCancellation(error)) throw new JsonRpcRequestCancelledError();
      return toolError(errorMessage(error));
    }
  }

  async #completeMissingInputs(operation: OperationDefinition, input: JsonObject, signal: AbortSignal): Promise<JsonObject> {
    const complete: JsonObject = { ...input };
    const elicitedFields = new Set<string>();
    while (true) {
      try {
        validateOperationInput(operation, complete);
        return complete;
      } catch (error: unknown) {
        if (!(error instanceof MissingOperationInputError) || !this.#supportsFormElicitation) throw error;
        const field = error.field;
        if (elicitedFields.has(field)) throw error;
        const properties = operation.inputSchema.properties;
        const fieldSchema = isJsonObject(properties) ? properties[field] : undefined;
        if (!isJsonObject(fieldSchema) || !isPrimitiveElicitationSchema(fieldSchema)) throw error;
        elicitedFields.add(field);
        const response = await this.#requestElicitation({
          message: `FragForge operation ${operation.name} needs ${field}.`,
          requestedSchema: {
            additionalProperties: false,
            properties: { [field]: fieldSchema },
            required: [field],
            type: 'object',
          },
        }, signal);
        if (!isJsonObject(response) || response.action !== 'accept' || !isJsonObject(response.content)
          || !Object.hasOwn(response.content, field)) {
          throw new Error(`${field} was not supplied`);
        }
        const value = response.content[field];
        if (value === undefined) throw new Error(`${field} was not supplied`);
        complete[field] = value;
      }
    }
  }

  async #readResource(request: JsonRpcRequest): Promise<void> {
    if (!isJsonObject(request.params) || typeof request.params.uri !== 'string') {
      await this.#connection.sendError(request.id, INVALID_PARAMS, 'resources/read requires uri');
      return;
    }
    let value: JsonValue;
    if (request.params.uri === 'fragforge://catalog') value = operationCatalogResource();
    else if (request.params.uri === 'fragforge://status') {
      if (this.#activeRequests.size >= this.#maxConcurrentRequests) {
        await this.#connection.sendError(request.id, SERVER_BUSY, 'FragForge MCP is busy; retry later');
        return;
      }
      const operation = operationNamed('studio.status');
      if (operation === undefined) throw new Error('studio.status operation is missing');
      const controller = new AbortController();
      this.#activeRequests.set(request.id, controller);
      try {
        value = await operation.run(this.#client, {}, controller.signal);
        if (controller.signal.aborted) throw new JsonRpcRequestCancelledError();
      } finally {
        this.#activeRequests.delete(request.id);
      }
    } else {
      await this.#connection.sendError(request.id, INVALID_PARAMS, 'unknown FragForge resource URI');
      return;
    }
    await this.#connection.sendResult(request.id, {
      contents: [{ mimeType: 'application/json', text: JSON.stringify(value), uri: request.params.uri }],
    });
  }

  async #requestElicitation(params: JsonObject, signal: AbortSignal): Promise<unknown> {
    const controller = new AbortController();
    let timedOut = false;
    const handleCallerAbort = (): void => controller.abort();
    if (signal.aborted) controller.abort();
    else signal.addEventListener('abort', handleCallerAbort, { once: true });
    const timeout = setTimeout(() => {
      timedOut = true;
      controller.abort();
    }, this.#elicitationTimeoutMs);
    timeout.unref();
    try {
      return await this.#connection.sendRequest('elicitation/create', params, controller.signal);
    } catch (error: unknown) {
      if (timedOut && !signal.aborted) {
        throw new Error(`elicitation timed out after ${this.#elicitationTimeoutMs}ms`);
      }
      throw error;
    } finally {
      clearTimeout(timeout);
      signal.removeEventListener('abort', handleCallerAbort);
    }
  }

  async #handleNotification(notification: JsonRpcNotification): Promise<void> {
    if (notification.method === 'notifications/initialized' && this.#initializeNegotiated) this.#ready = true;
    if (notification.method === 'notifications/cancelled' && isJsonObject(notification.params)) {
      const requestID = notification.params.requestId;
      if (typeof requestID === 'string' || typeof requestID === 'number') this.#activeRequests.get(requestID)?.abort();
    }
  }

  #writeDiagnostic(error: unknown): void {
    if (this.#diagnostics === undefined) return;
    const message = errorMessage(error).replace(/[\r\n]+/g, ' ');
    this.#diagnostics.write(`[fragforge-mcp] ${message}\n`);
  }

  #abortActiveRequests(): void {
    for (const controller of this.#activeRequests.values()) controller.abort();
    this.#activeRequests.clear();
    this.#inFlightRequestIds.clear();
  }
}

function toolSuccess(value: JsonObject): JsonObject {
  return {
    content: [{ text: JSON.stringify(value), type: 'text' }],
    structuredContent: value,
  };
}

function toolError(message: string): JsonObject {
  const structuredContent: JsonObject = { error: message };
  return toolFailure(structuredContent);
}

function toolFailure(structuredContent: JsonObject): JsonObject {
  return {
    content: [{ text: JSON.stringify(structuredContent), type: 'text' }],
    isError: true,
    structuredContent,
  };
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

function supportsFormElicitation(protocolVersion: string, value: JsonValue | undefined): boolean {
  if (!isJsonObject(value)) return false;
  if (protocolVersion === LATEST_PROTOCOL_VERSION) {
    return Object.keys(value).length === 0 || isJsonObject(value.form);
  }
  return protocolVersion === '2025-06-18';
}

function isPrimitiveElicitationSchema(schema: JsonObject): boolean {
  return schema.type === 'string'
    || schema.type === 'number'
    || schema.type === 'integer'
    || schema.type === 'boolean';
}

function isCancellation(error: unknown): boolean {
  return error instanceof JsonRpcRequestCancelledError
    || (error instanceof Error && (
      error.message === 'operation was cancelled'
      || error.message === 'FragForge operation was cancelled'
    ));
}

function boundedInteger(value: number, label: string, minimum: number, maximum: number): number {
  if (!Number.isSafeInteger(value) || value < minimum || value > maximum) {
    throw new Error(`${label} must be an integer from ${minimum} to ${maximum}`);
  }
  return value;
}
