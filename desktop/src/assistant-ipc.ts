/**
 * The deliberately small IPC contract for the embedded Codex rail.
 *
 * It is kept separate from the settings bridge so the sandboxed renderer
 * cannot gain access to arbitrary Electron or child-process capabilities.
 */
export const ASSISTANT_CHANNEL = 'fragforge:assistant';
export const ASSISTANT_EVENT_CHANNEL = 'fragforge:assistant-event';

const MAX_ACTION_ID_LENGTH = 128;
const MAX_CONTEXT_LABEL_LENGTH = 200;
const MAX_MESSAGE_LENGTH = 8_000;
const MAX_PATHNAME_LENGTH = 512;

export const ASSISTANT_ACTION = {
  approve: 'approve',
  cancel: 'cancel',
  clear: 'clear',
  newConversation: 'new',
  reject: 'reject',
  send: 'send',
  status: 'status',
} as const;

export type AssistantActionName = typeof ASSISTANT_ACTION[keyof typeof ASSISTANT_ACTION];

/** Page-only context. File paths, arbitrary API payloads, and secrets never cross this boundary. */
export interface AssistantContext {
  jobId?: string;
  kind: AssistantContextKind;
  label: string;
  pathname: string;
  streamJobId?: string;
  variant?: string;
}

export type AssistantContextKind = 'demo' | 'none' | 'render' | 'stream';

export interface AssistantSendRequest {
  action: typeof ASSISTANT_ACTION.send;
  context: AssistantContext;
  message: string;
}

export type AssistantRequest =
  | { action: typeof ASSISTANT_ACTION.status }
  | { action: typeof ASSISTANT_ACTION.cancel }
  | { action: typeof ASSISTANT_ACTION.clear }
  | { action: typeof ASSISTANT_ACTION.newConversation }
  | { action: typeof ASSISTANT_ACTION.approve; actionId: string }
  | { action: typeof ASSISTANT_ACTION.reject; actionId: string }
  | AssistantSendRequest;

export type AssistantAvailability = 'starting' | 'ready' | 'unavailable' | 'error';
export type AssistantMessageRole = 'assistant' | 'system' | 'user';
export type AssistantOperationRisk = 'costly' | 'destructive' | 'read' | 'write';
export type AssistantActionStatus = 'approved' | 'completed' | 'expired' | 'failed' | 'pending' | 'rejected';

export interface AssistantMessage {
  content: string;
  createdAt: string;
  id: string;
  role: AssistantMessageRole;
  streaming?: boolean;
}

/** A preview only. The exact operation and arguments stay main-process-only until approval. */
export interface AssistantActionPreviewField {
  label: string;
  value: string;
}

export interface AssistantActionPreview {
  fields?: readonly AssistantActionPreviewField[];
  summary?: string;
}

export interface AssistantAction {
  description?: string;
  expiresAt?: string;
  id: string;
  operation: string;
  preview?: AssistantActionPreview;
  risk: AssistantOperationRisk;
  requiresApproval?: boolean;
  status?: AssistantActionStatus;
  title: string;
}

export interface AssistantSnapshot {
  availability: AssistantAvailability;
  busy: boolean;
  error?: string;
  messages: readonly AssistantMessage[];
  pendingActions: readonly AssistantAction[];
  revision: number;
  threadId?: string;
}

export type AssistantCommandResult =
  | { ok: true; snapshot: AssistantSnapshot }
  | { error: string; ok: false; snapshot?: AssistantSnapshot };

export type AssistantIPCResponse = AssistantCommandResult;

export type AssistantEvent =
  | { snapshot: AssistantSnapshot; type: 'snapshot' }
  | { availability: AssistantAvailability; error?: string; type: 'availability' }
  | { busy: boolean; type: 'busy' }
  | { message: AssistantMessage; type: 'message' }
  | { createdAt?: string; delta: string; messageId: string; type: 'message_delta' }
  | { messageId: string; type: 'message_complete' }
  | { action: AssistantAction; type: 'action' }
  | { actionId: string; type: 'action_remove' };

/** Parses only exact, bounded requests from the sandboxed preload bridge. */
export function parseAssistantRequest(value: unknown): AssistantRequest {
  if (!isRecord(value) || !Object.hasOwn(value, 'action') || typeof value.action !== 'string') {
    throw new Error('invalid assistant request');
  }
  const action = value.action;
  if (action === ASSISTANT_ACTION.status
    || action === ASSISTANT_ACTION.cancel
    || action === ASSISTANT_ACTION.clear
    || action === ASSISTANT_ACTION.newConversation) {
    requireExactKeys(value, ['action']);
    return { action };
  }
  if (action === ASSISTANT_ACTION.approve || action === ASSISTANT_ACTION.reject) {
    requireExactKeys(value, ['action', 'actionId']);
    if (!isSafeOpaqueID(value.actionId, MAX_ACTION_ID_LENGTH)) throw new Error('invalid assistant request');
    return { action, actionId: value.actionId };
  }
  if (action === ASSISTANT_ACTION.send) {
    requireExactKeys(value, ['action', 'context', 'message']);
    if (typeof value.message !== 'string' || value.message.trim() === '' || value.message.length > MAX_MESSAGE_LENGTH) {
      throw new Error('invalid assistant request');
    }
    if (containsSensitiveUserContent(value.message)) throw new Error('invalid assistant request');
    return {
      action,
      context: parseContext(value.context),
      message: value.message.trim(),
    };
  }
  throw new Error('invalid assistant request');
}

function parseContext(value: unknown): AssistantContext {
  if (!isRecord(value)) throw new Error('invalid assistant request');
  const allowedKeys = new Set(['jobId', 'kind', 'label', 'pathname', 'streamJobId', 'variant']);
  const keys = Object.keys(value);
  if (keys.some((key) => !allowedKeys.has(key))) throw new Error('invalid assistant request');
  if (typeof value.pathname !== 'string'
    || value.pathname.length === 0
    || value.pathname.length > MAX_PATHNAME_LENGTH
    || !/^\/[A-Za-z0-9/_-]*$/.test(value.pathname)) {
    throw new Error('invalid assistant request');
  }
  if (!isContextKind(value.kind)
    || typeof value.label !== 'string'
    || value.label.trim() === ''
    || value.label.length > MAX_CONTEXT_LABEL_LENGTH
    || /[\r\n]/.test(value.label)
    || !isOptionalReference(value.jobId)
    || !isOptionalReference(value.streamJobId)
    || !isOptionalReference(value.variant)) {
    throw new Error('invalid assistant request');
  }
  if (value.kind === 'demo' && value.streamJobId !== undefined) throw new Error('invalid assistant request');
  if (value.kind === 'stream' && (value.jobId !== undefined || value.variant !== undefined)) throw new Error('invalid assistant request');
  if (value.kind === 'render' && value.jobId === undefined && value.streamJobId === undefined) throw new Error('invalid assistant request');
  if (value.kind === 'none' && (value.jobId !== undefined || value.streamJobId !== undefined || value.variant !== undefined)) {
    throw new Error('invalid assistant request');
  }
  return {
    kind: value.kind,
    label: value.label.trim(),
    pathname: value.pathname,
    ...(value.jobId === undefined ? {} : { jobId: value.jobId }),
    ...(value.streamJobId === undefined ? {} : { streamJobId: value.streamJobId }),
    ...(value.variant === undefined ? {} : { variant: value.variant }),
  };
}

function isContextKind(value: unknown): value is AssistantContextKind {
  return value === 'demo' || value === 'none' || value === 'render' || value === 'stream';
}

function isOptionalReference(value: unknown): value is string | undefined {
  return value === undefined || isSafeOpaqueID(value, MAX_ACTION_ID_LENGTH);
}

function isSafeOpaqueID(value: unknown, maximumLength: number): value is string {
  return typeof value === 'string'
    && value.length > 0
    && value.length <= maximumLength
    && /^[A-Za-z0-9_-]+$/.test(value);
}

function containsSensitiveUserContent(value: string): boolean {
  return /\b(?:https?|file):\/\//i.test(value)
    || /(?:^|[\s"'(])[A-Za-z]:[\\/]/i.test(value)
    || /(?:^|[\s"'(])\\\\[^\\/\s]/.test(value)
    || /(?:api[_-]?key|authorization|bearer|credential|password|secret|token)\s*[:=]/i.test(value);
}

function requireExactKeys(value: Record<string, unknown>, expected: string[]): void {
  const keys = Object.keys(value).sort();
  const wanted = [...expected].sort();
  if (keys.length !== wanted.length || keys.some((key, index) => key !== wanted[index])) {
    throw new Error('invalid assistant request');
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}
