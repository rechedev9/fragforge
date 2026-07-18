/**
 * Renderer-side contract for the narrowly scoped Codex bridge exposed by the
 * FragForge Studio Electron preload. The browser intentionally has no network
 * fallback: Codex, its local session, and approved actions all stay inside the
 * desktop process.
 */

export const ASSISTANT_AVAILABILITY = {
  starting: 'starting',
  ready: 'ready',
  unavailable: 'unavailable',
  error: 'error',
} as const;

export type AssistantAvailability = typeof ASSISTANT_AVAILABILITY[keyof typeof ASSISTANT_AVAILABILITY];

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

export interface AssistantSnapshot {
  availability: AssistantAvailability;
  error?: string;
  messages: readonly AssistantMessage[];
  pendingActions: readonly AssistantAction[];
  busy: boolean;
  threadId?: string;
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
  status(): Promise<AssistantSnapshot>;
  send(request: AssistantSendRequest): Promise<void>;
  cancel(): Promise<void>;
  approve(actionId: string): Promise<void>;
  reject(actionId: string): Promise<void>;
  newConversation(): Promise<void>;
  clearHistory(): Promise<void>;
  subscribe(listener: AssistantEventListener): () => void;
}

/** Creates the renderer's deterministic first state while the bridge loads. */
export function initialAssistantSnapshot(availability: AssistantAvailability = ASSISTANT_AVAILABILITY.starting): AssistantSnapshot {
  return {
    availability,
    busy: false,
    messages: [],
    pendingActions: [],
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
      return event.snapshot;
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
    && typeof value.send === 'function'
    && typeof value.cancel === 'function'
    && typeof value.approve === 'function'
    && typeof value.reject === 'function'
    && typeof value.newConversation === 'function'
    && typeof value.clearHistory === 'function'
    && typeof value.subscribe === 'function';
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null;
}
