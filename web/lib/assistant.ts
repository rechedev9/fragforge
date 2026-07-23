/**
 * Renderer-side contract for the narrowly scoped FragForge agent bridge exposed by the
 * Studio Electron preload. The browser intentionally has no network fallback:
 * Codex OAuth, its local session, and approved actions all stay inside the
 * desktop process.
 */

export const ASSISTANT_AVAILABILITY = {
  sleeping: 'sleeping',
  starting: 'starting',
  ready: 'ready',
  unavailable: 'unavailable',
  error: 'error',
} as const;

export type AssistantAvailability = typeof ASSISTANT_AVAILABILITY[keyof typeof ASSISTANT_AVAILABILITY];

export const ASSISTANT_ACCOUNT_STATUSES = {
  checking: 'checking',
  error: 'error',
  signedIn: 'signed-in',
  signedOut: 'signed-out',
  signingIn: 'signing-in',
  unsupported: 'unsupported',
} as const;

export type AssistantAccountStatus = typeof ASSISTANT_ACCOUNT_STATUSES[keyof typeof ASSISTANT_ACCOUNT_STATUSES];

export interface AssistantAccount {
  planType?: string;
  status: AssistantAccountStatus;
}

export const ASSISTANT_MESSAGE_ROLES = {
  assistant: 'assistant',
  system: 'system',
  user: 'user',
} as const;

export type AssistantMessageRole = typeof ASSISTANT_MESSAGE_ROLES[keyof typeof ASSISTANT_MESSAGE_ROLES];

export const ASSISTANT_ACTION_RISKS = {
  costly: 'costly',
  destructive: 'destructive',
  read: 'read',
  write: 'write',
} as const;

export type AssistantActionRisk = typeof ASSISTANT_ACTION_RISKS[keyof typeof ASSISTANT_ACTION_RISKS];

export const ASSISTANT_ACTION_STATUSES = {
  approved: 'approved',
  completed: 'completed',
  expired: 'expired',
  failed: 'failed',
  pending: 'pending',
  rejected: 'rejected',
} as const;

export type AssistantActionStatus = typeof ASSISTANT_ACTION_STATUSES[keyof typeof ASSISTANT_ACTION_STATUSES];

export const ASSISTANT_CONTEXT_KINDS = {
  demo: 'demo',
  none: 'none',
  render: 'render',
  stream: 'stream',
} as const;

export type AssistantContextKind = typeof ASSISTANT_CONTEXT_KINDS[keyof typeof ASSISTANT_CONTEXT_KINDS];

/** The visible Studio location attached to a turn, never a local file path. */
export interface AssistantContextRef {
  kind: AssistantContextKind;
  label: string;
  pathname: string;
  jobId?: string;
  streamJobId?: string;
  variant?: string;
}

export interface AssistantMessage {
  id: string;
  role: AssistantMessageRole;
  content: string;
  createdAt: string;
  /** True while Codex is still sending text for this message. */
  streaming?: boolean;
}

export interface AssistantActionPreviewField {
  label: string;
  value: string;
}

/** Sanitized operation preview. It intentionally has no raw paths or secrets. */
export interface AssistantActionPreview {
  summary?: string;
  fields?: readonly AssistantActionPreviewField[];
}

/**
 * A server-created, opaque pending operation. Approval accepts only this id;
 * it never resubmits model-generated text or arbitrary arguments.
 */
export interface AssistantAction {
  id: string;
  title: string;
  operation: string;
  risk: AssistantActionRisk;
  preview?: AssistantActionPreview;
  description?: string;
  status?: AssistantActionStatus;
  expiresAt?: string;
  requiresApproval?: boolean;
}

export function assistantActionStatusLabel(
  action: Pick<AssistantAction, 'operation' | 'risk'>,
  status: AssistantActionStatus,
): string {
  switch (status) {
    case ASSISTANT_ACTION_STATUSES.approved:
      return 'Aprobada';
    case ASSISTANT_ACTION_STATUSES.completed:
      if (action.operation === 'studio.confirm_creative_brief') return 'Brief aprobado';
      return action.risk === ASSISTANT_ACTION_RISKS.costly ? 'Solicitud enviada' : 'Completada';
    case ASSISTANT_ACTION_STATUSES.expired:
      return 'Caducada';
    case ASSISTANT_ACTION_STATUSES.failed:
      return 'Fallida';
    case ASSISTANT_ACTION_STATUSES.rejected:
      return 'Rechazada';
    default:
      return 'Pendiente';
  }
}

export function assistantActionsSectionLabel(actions: readonly AssistantAction[]): string {
  return actions.some((action) => (action.status ?? ASSISTANT_ACTION_STATUSES.pending) === ASSISTANT_ACTION_STATUSES.pending)
    ? 'Acciones para revisar'
    : 'Historial de acciones';
}

export type AssistantActionsSectionState = 'attention' | 'completed' | 'history' | 'pending';

export function assistantActionsSectionState(actions: readonly AssistantAction[]): AssistantActionsSectionState {
  const statuses = actions.map((action) => action.status ?? ASSISTANT_ACTION_STATUSES.pending);
  if (statuses.includes(ASSISTANT_ACTION_STATUSES.pending)) return 'pending';
  if (statuses.length > 0 && statuses.every((status) => status === ASSISTANT_ACTION_STATUSES.completed)) return 'completed';
  if (statuses.some((status) => status === ASSISTANT_ACTION_STATUSES.failed
    || status === ASSISTANT_ACTION_STATUSES.rejected
    || status === ASSISTANT_ACTION_STATUSES.expired)) return 'attention';
  return 'history';
}

export interface AssistantSnapshot {
  account: AssistantAccount;
  availability: AssistantAvailability;
  error?: string;
  messages: readonly AssistantMessage[];
  pendingActions: readonly AssistantAction[];
  busy: boolean;
  revision: number;
  threadId?: string;
}

export type AssistantCommandResult =
  | { ok: true; snapshot: AssistantSnapshot }
  | { error: string; ok: false; snapshot?: AssistantSnapshot };

export interface AssistantCommandState {
  commandPendingCount: number;
  controlError?: string;
  snapshot: AssistantSnapshot;
}

export interface AssistantSendRequest {
  message: string;
  context: AssistantContextRef;
}

export type AssistantEvent =
  | { type: 'snapshot'; snapshot: AssistantSnapshot }
  | { type: 'availability'; availability: AssistantAvailability; error?: string }
  | { type: 'busy'; busy: boolean }
  | { type: 'message'; message: AssistantMessage }
  | { type: 'message_delta'; messageId: string; delta: string; createdAt?: string }
  | { type: 'message_complete'; messageId: string }
  | { type: 'action'; action: AssistantAction }
  | { type: 'action_remove'; actionId: string };

export type AssistantEventListener = (event: AssistantEvent) => void;

export interface FragforgeAssistantBridge {
  status(): Promise<AssistantCommandResult>;
  wake(): Promise<AssistantCommandResult>;
  send(request: AssistantSendRequest): Promise<AssistantCommandResult>;
  cancel(): Promise<AssistantCommandResult>;
  approve(actionId: string): Promise<AssistantCommandResult>;
  reject(actionId: string): Promise<AssistantCommandResult>;
  newConversation(): Promise<AssistantCommandResult>;
  clearHistory(): Promise<AssistantCommandResult>;
  login(): Promise<AssistantCommandResult>;
  logout(): Promise<AssistantCommandResult>;
  subscribe(listener: AssistantEventListener): () => void;
}

/** Creates the renderer's deterministic first state while the bridge loads. */
export function initialAssistantSnapshot(availability: AssistantAvailability = ASSISTANT_AVAILABILITY.starting): AssistantSnapshot {
  return {
    account: { status: ASSISTANT_ACCOUNT_STATUSES.checking },
    availability,
    busy: false,
    messages: [],
    pendingActions: [],
    revision: -1,
  };
}

export function beginAssistantCommand(state: AssistantCommandState): AssistantCommandState {
  return {
    commandPendingCount: state.commandPendingCount + 1,
    snapshot: state.snapshot,
  };
}

export function finishAssistantCommand(
  state: AssistantCommandState,
  result: AssistantCommandResult,
): AssistantCommandState {
  const nextSnapshot = result.snapshot === undefined
    ? state.snapshot
    : newerSnapshot(state.snapshot, result.snapshot);
  return {
    commandPendingCount: Math.max(0, state.commandPendingCount - 1),
    ...(result.ok ? {} : { controlError: result.error }),
    snapshot: nextSnapshot,
  };
}

export function parseAssistantCommandResult(value: unknown): AssistantCommandResult {
  if (!isRecord(value) || typeof value.ok !== 'boolean') throw new Error('invalid assistant command result');
  if (value.ok) {
    return { ok: true, snapshot: parseAssistantSnapshot(value.snapshot) };
  }
  if (typeof value.error !== 'string' || value.error.trim() === '') throw new Error('invalid assistant command result');
  return {
    error: value.error,
    ok: false,
    ...(value.snapshot === undefined ? {} : { snapshot: parseAssistantSnapshot(value.snapshot) }),
  };
}

export function parseAssistantSnapshotEvent(value: unknown): Extract<AssistantEvent, { type: 'snapshot' }> {
  if (!isRecord(value) || value.type !== 'snapshot') throw new Error('invalid assistant snapshot event');
  return { snapshot: parseAssistantSnapshot(value.snapshot), type: 'snapshot' };
}

function parseAssistantSnapshot(value: unknown): AssistantSnapshot {
  if (!isRecord(value)
    || !isAssistantAccount(value.account)
    || !isAssistantAvailability(value.availability)
    || typeof value.busy !== 'boolean'
    || typeof value.revision !== 'number'
    || !Number.isSafeInteger(value.revision)
    || value.revision < 0
    || !Array.isArray(value.messages)
    || !value.messages.every(isAssistantMessage)
    || !Array.isArray(value.pendingActions)
    || !value.pendingActions.every(isAssistantAction)
    || (value.error !== undefined && typeof value.error !== 'string')
    || (value.threadId !== undefined && typeof value.threadId !== 'string')) {
    throw new Error('invalid assistant snapshot');
  }
  return {
    account: value.account,
    availability: value.availability,
    busy: value.busy,
    ...(value.error === undefined ? {} : { error: value.error }),
    messages: value.messages,
    pendingActions: value.pendingActions,
    revision: value.revision,
    ...(value.threadId === undefined ? {} : { threadId: value.threadId }),
  };
}

/**
 * Detects the complete Electron preload surface. It deliberately rejects
 * partial globals so normal-browser development renders the unavailable state
 * instead of attempting an HTTP or browser-storage fallback.
 */
export function getFragforgeAssistantBridge(scope: unknown = globalThis): FragforgeAssistantBridge | null {
  if (!isRecord(scope)) return null;
  const candidate = scope.fragforgeAssistant;
  return isFragforgeAssistantBridge(candidate) ? candidate : null;
}

/**
 * Derives a safe, human-readable context chip from App Router's pathname. IDs
 * are retained as opaque references for the desktop process but never shown in
 * the chip itself.
 */
export function assistantContextFromPathname(pathname: string): AssistantContextRef {
  const segments = pathname.split('/').filter((segment) => segment.length > 0);
  const root = segments[0];
  const identifier = segments[1];
  const route = segments[2];
  const variant = segments[3];

  if (root === 'matches') {
    if (route === 'renders' && identifier !== undefined && variant !== undefined) {
      return { kind: ASSISTANT_CONTEXT_KINDS.render, jobId: identifier, label: 'Render de demo', pathname, variant };
    }
    if (identifier !== undefined) {
      return { kind: ASSISTANT_CONTEXT_KINDS.demo, jobId: identifier, label: 'Demo actual', pathname };
    }
    return { kind: ASSISTANT_CONTEXT_KINDS.none, label: 'Partidas', pathname };
  }

  if (root === 'upload') {
    return { kind: ASSISTANT_CONTEXT_KINDS.demo, label: 'Nueva demo', pathname };
  }

  if (root === 'streams') {
    if (route === 'renders' && identifier !== undefined && variant !== undefined) {
      return {
        kind: ASSISTANT_CONTEXT_KINDS.render,
        label: 'Render de stream',
        pathname,
        streamJobId: identifier,
        variant,
      };
    }
    if (identifier !== undefined) {
      return { kind: ASSISTANT_CONTEXT_KINDS.stream, label: 'Stream actual', pathname, streamJobId: identifier };
    }
    return { kind: ASSISTANT_CONTEXT_KINDS.none, label: 'Clips de stream', pathname };
  }

  if (root === 'videos') {
    return { kind: ASSISTANT_CONTEXT_KINDS.none, label: 'Biblioteca', pathname };
  }

  if (root === 'news') {
    return { kind: ASSISTANT_CONTEXT_KINDS.none, label: 'Noticias', pathname };
  }

  if (root === 'feed') {
    return { kind: ASSISTANT_CONTEXT_KINDS.none, label: 'Feed', pathname };
  }

  if (root === 'series' && identifier !== undefined) {
    return { kind: ASSISTANT_CONTEXT_KINDS.demo, jobId: identifier, label: 'Serie actual', pathname };
  }

  if (root === 'settings') {
    return { kind: ASSISTANT_CONTEXT_KINDS.none, label: 'Ajustes', pathname };
  }

  return { kind: ASSISTANT_CONTEXT_KINDS.none, label: 'Studio', pathname };
}

/**
 * Applies both complete snapshots and fine-grained app-server streaming events
 * without forcing the renderer to know how the Electron process transports
 * them. The preload can use snapshots exclusively if that remains simpler.
 */
export function applyAssistantEvent(snapshot: AssistantSnapshot, event: AssistantEvent): AssistantSnapshot {
  switch (event.type) {
    case 'snapshot':
      return newerSnapshot(snapshot, event.snapshot);
    case 'availability':
      return { ...snapshot, availability: event.availability, error: event.error };
    case 'busy':
      return { ...snapshot, busy: event.busy };
    case 'message':
      return { ...snapshot, messages: replaceMessage(snapshot.messages, event.message) };
    case 'message_delta':
      return {
        ...snapshot,
        busy: true,
        messages: appendMessageDelta(snapshot.messages, event),
      };
    case 'message_complete':
      return {
        ...snapshot,
        busy: false,
        messages: snapshot.messages.map((message) => (
          message.id === event.messageId ? { ...message, streaming: false } : message
        )),
      };
    case 'action':
      return { ...snapshot, pendingActions: replaceAction(snapshot.pendingActions, event.action) };
    case 'action_remove':
      return {
        ...snapshot,
        pendingActions: snapshot.pendingActions.filter((action) => action.id !== event.actionId),
      };
  }
}

function newerSnapshot(current: AssistantSnapshot, candidate: AssistantSnapshot): AssistantSnapshot {
  return candidate.revision > current.revision ? candidate : current;
}

function replaceMessage(messages: readonly AssistantMessage[], next: AssistantMessage): readonly AssistantMessage[] {
  const index = messages.findIndex((message) => message.id === next.id);
  if (index === -1) return [...messages, next];
  return messages.map((message) => (message.id === next.id ? next : message));
}

function appendMessageDelta(
  messages: readonly AssistantMessage[],
  event: Extract<AssistantEvent, { type: 'message_delta' }>,
): readonly AssistantMessage[] {
  const existing = messages.find((message) => message.id === event.messageId);
  if (existing === undefined) {
    return [
      ...messages,
      {
        content: event.delta,
        createdAt: event.createdAt ?? new Date().toISOString(),
        id: event.messageId,
        role: ASSISTANT_MESSAGE_ROLES.assistant,
        streaming: true,
      },
    ];
  }
  return messages.map((message) => (
    message.id === event.messageId
      ? { ...message, content: `${message.content}${event.delta}`, streaming: true }
      : message
  ));
}

function replaceAction(actions: readonly AssistantAction[], next: AssistantAction): readonly AssistantAction[] {
  const index = actions.findIndex((action) => action.id === next.id);
  if (index === -1) return [...actions, next];
  return actions.map((action) => (action.id === next.id ? next : action));
}

function isFragforgeAssistantBridge(value: unknown): value is FragforgeAssistantBridge {
  if (!isRecord(value)) return false;
  return typeof value.status === 'function'
    && typeof value.wake === 'function'
    && typeof value.send === 'function'
    && typeof value.cancel === 'function'
    && typeof value.approve === 'function'
    && typeof value.reject === 'function'
    && typeof value.newConversation === 'function'
    && typeof value.clearHistory === 'function'
    && typeof value.login === 'function'
    && typeof value.logout === 'function'
    && typeof value.subscribe === 'function';
}

function isAssistantAccount(value: unknown): value is AssistantAccount {
  return isRecord(value)
    && (value.planType === undefined || typeof value.planType === 'string')
    && (value.status === ASSISTANT_ACCOUNT_STATUSES.checking
      || value.status === ASSISTANT_ACCOUNT_STATUSES.error
      || value.status === ASSISTANT_ACCOUNT_STATUSES.signedIn
      || value.status === ASSISTANT_ACCOUNT_STATUSES.signedOut
      || value.status === ASSISTANT_ACCOUNT_STATUSES.signingIn
      || value.status === ASSISTANT_ACCOUNT_STATUSES.unsupported);
}

function isAssistantAvailability(value: unknown): value is AssistantAvailability {
  return value === ASSISTANT_AVAILABILITY.sleeping
    || value === ASSISTANT_AVAILABILITY.starting
    || value === ASSISTANT_AVAILABILITY.ready
    || value === ASSISTANT_AVAILABILITY.unavailable
    || value === ASSISTANT_AVAILABILITY.error;
}

function isAssistantMessage(value: unknown): value is AssistantMessage {
  return isRecord(value)
    && typeof value.id === 'string'
    && typeof value.content === 'string'
    && typeof value.createdAt === 'string'
    && (value.role === ASSISTANT_MESSAGE_ROLES.assistant
      || value.role === ASSISTANT_MESSAGE_ROLES.system
      || value.role === ASSISTANT_MESSAGE_ROLES.user)
    && (value.streaming === undefined || typeof value.streaming === 'boolean');
}

function isAssistantAction(value: unknown): value is AssistantAction {
  return isRecord(value)
    && typeof value.id === 'string'
    && typeof value.title === 'string'
    && typeof value.operation === 'string'
    && (value.risk === ASSISTANT_ACTION_RISKS.costly
      || value.risk === ASSISTANT_ACTION_RISKS.destructive
      || value.risk === ASSISTANT_ACTION_RISKS.read
      || value.risk === ASSISTANT_ACTION_RISKS.write)
    && (value.description === undefined || typeof value.description === 'string')
    && (value.status === undefined || isAssistantActionStatus(value.status))
    && (value.expiresAt === undefined || typeof value.expiresAt === 'string')
    && (value.requiresApproval === undefined || typeof value.requiresApproval === 'boolean')
    && (value.preview === undefined || isAssistantActionPreview(value.preview));
}

function isAssistantActionPreview(value: unknown): value is AssistantActionPreview {
  return isRecord(value)
    && (value.summary === undefined || typeof value.summary === 'string')
    && (value.fields === undefined || (Array.isArray(value.fields) && value.fields.every((field) => (
      isRecord(field) && typeof field.label === 'string' && typeof field.value === 'string'
    ))));
}

function isAssistantActionStatus(value: unknown): value is AssistantActionStatus {
  return value === ASSISTANT_ACTION_STATUSES.approved
    || value === ASSISTANT_ACTION_STATUSES.completed
    || value === ASSISTANT_ACTION_STATUSES.expired
    || value === ASSISTANT_ACTION_STATUSES.failed
    || value === ASSISTANT_ACTION_STATUSES.pending
    || value === ASSISTANT_ACTION_STATUSES.rejected;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null;
}
