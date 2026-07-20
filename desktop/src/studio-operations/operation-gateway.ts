import { isJsonObject, type JsonObject, type JsonValue } from '../mcp/json.ts';
import {
  operationNamed,
  validateLiveOperationInput,
  validateOperationInput,
  type OperationDefinition,
  type OperationRisk,
} from '../mcp/operations.ts';
import { OrchestratorClient } from '../mcp/orchestrator-client.ts';

type MutationRisk = Exclude<OperationRisk, 'read'>;

/**
 * Resolves any missing operation inputs before the gateway validates them.
 *
 * A caller may use this to elicit primitive values. Other callers can omit it and
 * provide a complete argument object themselves.
 */
export type OperationInputCompleter = (
  operation: OperationDefinition,
  input: JsonObject,
  signal?: AbortSignal,
) => Promise<JsonObject>;

export interface OperationGatewayOptions {
  client: OrchestratorClient;
}

export interface OperationRequest {
  arguments?: JsonObject;
  operation: string;
}

export interface OperationExecutionOptions {
  /** Optional caller-owned input completion, such as an agent approval form. */
  completeInput?: OperationInputCompleter;
  /**
   * Explicit authority to run a non-read operation. Omit this (the default)
   * for a non-mutating preview, including when a caller supplied mutation-like
   * fields in its own request.
   */
  privileged?: boolean;
  signal?: AbortSignal;
}

interface OperationGatewayOutcomeBase {
  /** Fully completed and schema-validated arguments used for this decision. */
  arguments: JsonObject;
  operation: string;
}

export interface OperationPreview extends OperationGatewayOutcomeBase {
  kind: 'preview';
  preview: JsonObject;
  requiresConfirmation: true;
  risk: MutationRisk;
}

export interface OperationExecuted extends OperationGatewayOutcomeBase {
  /** True only when a cancelled operation created a durable partial result. */
  partialFailure: boolean;
  result: JsonValue;
  status: 'completed' | 'partial';
  kind: 'executed';
  error?: string;
}

export type OperationGatewayOutcome = OperationPreview | OperationExecuted;

/**
 * One safe execution boundary for the allowlisted FragForge Studio operations.
 *
 * Reads always run after validation. Every other risk class remains a local
 * preview unless the caller opts in with `privileged: true`; that call still
 * validates live inputs immediately before the operation is dispatched.
 */
export class OperationGateway {
  readonly #client: OrchestratorClient;

  constructor(options: OperationGatewayOptions) {
    this.#client = options.client;
  }

  async execute(
    request: OperationRequest,
    options: OperationExecutionOptions = {},
  ): Promise<OperationGatewayOutcome> {
    const operation = resolveOperation(request.operation);
    const suppliedArguments = request.arguments ?? {};
    if (!isJsonObject(suppliedArguments)) throw new Error('arguments must be an object');

    const completeArguments = options.completeInput === undefined
      ? { ...suppliedArguments }
      : await options.completeInput(operation, { ...suppliedArguments }, options.signal);
    if (!isJsonObject(completeArguments)) throw new Error('completed arguments must be an object');
    validateOperationInput(operation, completeArguments);

    if (operation.risk !== 'read' && options.privileged !== true) {
      return {
        arguments: completeArguments,
        kind: 'preview',
        operation: operation.name,
        preview: operation.preview(completeArguments),
        requiresConfirmation: true,
        risk: operation.risk,
      };
    }

    await validateLiveOperationInput(this.#client, operation, completeArguments, options.signal);
    const value = await operation.run(this.#client, completeArguments, options.signal);
    const partial = isJsonObject(value) && value.partial === true;
    const partialFailure = partial && value.cancelled === true;
    return {
      arguments: completeArguments,
      error: partialFailure ? partialFailureError(value) : undefined,
      kind: 'executed',
      operation: operation.name,
      partialFailure,
      result: value,
      status: partial ? 'partial' : 'completed',
    };
  }
}

function partialFailureError(value: JsonValue): string {
  if (isJsonObject(value) && typeof value.error === 'string') return value.error;
  return 'operation was cancelled after a durable partial result was created';
}

function resolveOperation(name: string): OperationDefinition {
  if (name === '') throw new Error('operation is required');
  const operation = operationNamed(name);
  if (operation === undefined) throw new Error(`unknown operation ${name}; call search first`);
  return operation;
}
