'use client';

import {
  useCallback,
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
  type FormEvent,
  type KeyboardEvent,
  type ReactElement,
} from 'react';
import { usePathname } from 'next/navigation';
import {
  Check,
  CircleCheck,
  Clock3,
  LoaderCircle,
  MessageCircle,
  Plus,
  Send,
  ShieldAlert,
  Square,
  Trash2,
  TriangleAlert,
  X,
} from 'lucide-react';
import { Button } from '@/components/ui/button';
import { useAssistantContext } from '@/components/assistant/assistant-provider';
import {
  assistantContextFromPathname,
  ASSISTANT_ACTION_STATUSES,
  ASSISTANT_AVAILABILITY,
  ASSISTANT_MESSAGE_ROLES,
  type AssistantAction,
  type AssistantActionRisk,
  type AssistantAvailability,
  type AssistantMessage,
  type AssistantSnapshot,
} from '@/lib/assistant';
import { cn } from '@/lib/utils';

const ASSISTANT_MESSAGE_MAX_LENGTH = 4_000;
const TIME_FORMATTER = new Intl.DateTimeFormat('es-ES', { hour: '2-digit', minute: '2-digit' });

const RISK_COPY: Readonly<Record<AssistantActionRisk, { label: string; className: string }>> = {
  costly: { label: 'Consume recursos', className: 'border-warning/40 bg-warning/10 text-warning' },
  destructive: { label: 'Acción destructiva', className: 'border-destructive/45 bg-destructive/10 text-destructive' },
  read: { label: 'Solo lectura', className: 'border-primary/30 bg-primary/10 text-primary' },
  write: { label: 'Cambia Studio', className: 'border-primary/40 bg-primary/10 text-primary' },
};

type AssistantPanelProps = {
  /** Lets the app shell choose a fixed rail or a mobile drawer without changing panel behavior. */
  className?: string;
};

/**
 * Persistent-looking global Codex conversation surface. It deliberately owns
 * no filesystem, network, or tool access: every operation goes through the
 * narrow Electron preload bridge and pending actions stay server-created.
 */
export function AssistantPanel({ className }: AssistantPanelProps): ReactElement {
  const pathname = usePathname();
  const context = useMemo(() => assistantContextFromPathname(pathname), [pathname]);
  const {
    bridge,
    cancelPending,
    commandPendingCount,
    controlError,
    draft,
    runCommand,
    setCancelPending,
    setDraft,
    snapshot,
  } = useAssistantContext();
  const [activeActionId, setActiveActionId] = useState<string>();
  const [clearConfirmationVisible, setClearConfirmationVisible] = useState(false);
  const endRef = useRef<HTMLDivElement>(null);
  const composerId = useId();
  const actionsTitleId = useId();

  useEffect(() => {
    endRef.current?.scrollIntoView({ behavior: snapshot.busy ? 'smooth' : 'auto', block: 'end' });
  }, [snapshot.busy, snapshot.messages, snapshot.pendingActions]);

  const isReady = snapshot.availability === ASSISTANT_AVAILABILITY.ready;
  const isBusy = snapshot.busy || commandPendingCount > 0;
  const canSend = isReady && !isBusy && bridge !== null && draft.trim().length > 0;

  const sendMessage = useCallback(async (): Promise<void> => {
    const message = draft.trim();
    if (bridge === null || !isReady || isBusy || message.length === 0) return;
    const accepted = await runCommand(
      (activeBridge) => activeBridge.send({ context, message }),
      'No se pudo enviar el mensaje. Inténtalo de nuevo.',
    );
    if (accepted) setDraft('');
  }, [bridge, context, draft, isBusy, isReady, runCommand, setDraft]);

  const cancelTurn = useCallback(async (): Promise<void> => {
    if (bridge === null || !snapshot.busy || cancelPending) return;
    setCancelPending(true);
    try {
      await runCommand(
        (activeBridge) => activeBridge.cancel(),
        'No se pudo cancelar el turno actual.',
      );
    } finally {
      setCancelPending(false);
    }
  }, [bridge, cancelPending, runCommand, setCancelPending, snapshot.busy]);

  const decideAction = useCallback(async (actionId: string, decision: 'approve' | 'reject'): Promise<void> => {
    if (bridge === null || isBusy) return;
    setActiveActionId(actionId);
    try {
      await runCommand(
        (activeBridge) => decision === 'approve'
          ? activeBridge.approve(actionId)
          : activeBridge.reject(actionId),
        'No se pudo registrar esta decisión. La operación no se ha aplicado.',
      );
    } finally {
      setActiveActionId(undefined);
    }
  }, [bridge, isBusy, runCommand]);

  const startNewConversation = useCallback(async (): Promise<void> => {
    if (bridge === null || isBusy) return;
    await runCommand(
      (activeBridge) => activeBridge.newConversation(),
      'No se pudo crear una conversación nueva.',
    );
  }, [bridge, isBusy, runCommand]);

  const clearHistory = useCallback(async (): Promise<void> => {
    if (bridge === null || isBusy) return;
    const cleared = await runCommand(
      (activeBridge) => activeBridge.clearHistory(),
      'No se pudo borrar el historial mostrado.',
    );
    if (cleared) {
      setClearConfirmationVisible(false);
    }
  }, [bridge, isBusy, runCommand]);

  function submit(event: FormEvent<HTMLFormElement>): void {
    event.preventDefault();
    void sendMessage();
  }

  function onComposerKeyDown(event: KeyboardEvent<HTMLTextAreaElement>): void {
    if (event.key !== 'Enter' || event.shiftKey || event.nativeEvent.isComposing) return;
    event.preventDefault();
    void sendMessage();
  }

  return (
    <aside
      aria-label="Asistente de Codex"
      className={cn(
        'studio-panel neon-brackets flex min-h-[34rem] w-full min-w-0 flex-col overflow-hidden bg-surface/95',
        className,
      )}
      data-assistant-panel
    >
      <header className="border-b border-border bg-background/35 px-4 py-3.5">
        <div className="flex items-start justify-between gap-3">
          <div className="min-w-0">
            <div className="flex items-center gap-2">
              <span className="flex size-7 shrink-0 items-center justify-center border border-primary/35 bg-primary/10 text-primary">
                <MessageCircle className="size-4" aria-hidden />
              </span>
              <h2 className="truncate font-[family-name:var(--font-display)] text-base font-semibold uppercase tracking-[0.05em] text-foreground">
                Asistente
              </h2>
            </div>
            <p className="mt-1.5 flex items-center gap-1.5 text-[11px] text-muted-foreground" aria-live="polite">
              <AvailabilityDot availability={snapshot.availability} />
              {availabilityLabel(snapshot.availability)}
            </p>
          </div>
          <div className="flex shrink-0 items-center gap-1">
            <Button
              type="button"
              variant="ghost"
              size="icon-xs"
              onClick={() => void startNewConversation()}
              disabled={bridge === null || isBusy}
              aria-label="Nueva conversación"
              title="Nueva conversación"
            >
              <Plus aria-hidden />
            </Button>
            <Button
              type="button"
              variant={clearConfirmationVisible ? 'destructive' : 'ghost'}
              size="icon-xs"
              onClick={() => setClearConfirmationVisible((visible) => !visible)}
              disabled={bridge === null || isBusy}
              aria-label="Borrar historial de Studio"
              title="Borrar historial de Studio"
            >
              <Trash2 aria-hidden />
            </Button>
          </div>
        </div>

        <div className="mt-3 flex items-center justify-between gap-2">
          <span
            className="max-w-full truncate border border-primary/25 bg-primary/[0.07] px-2 py-1 font-[family-name:var(--font-mono)] text-[10px] uppercase tracking-[0.1em] text-primary"
            title={context.pathname}
          >
            Contexto · {context.label}
          </span>
          {snapshot.threadId ? (
            <span className="shrink-0 font-[family-name:var(--font-mono)] text-[10px] text-muted-foreground/65">HILO ACTIVO</span>
          ) : null}
        </div>

        {clearConfirmationVisible ? (
          <div className="mt-3 flex items-center justify-between gap-2 border border-destructive/35 bg-destructive/[0.06] p-2.5">
            <p className="min-w-0 text-xs leading-4 text-muted-foreground">¿Borrar solo el historial guardado en Studio?</p>
            <div className="flex shrink-0 gap-1">
              <Button type="button" variant="ghost" size="xs" onClick={() => setClearConfirmationVisible(false)}>
                Cancelar
              </Button>
              <Button type="button" variant="destructive" size="xs" onClick={() => void clearHistory()} disabled={isBusy}>
                Borrar
              </Button>
            </div>
          </div>
        ) : null}
      </header>

      <div className="min-h-0 flex-1 overflow-y-auto px-3 py-4" aria-live="polite" aria-relevant="additions text">
        <ConversationContent snapshot={snapshot} />

        {snapshot.pendingActions.length > 0 ? (
          <section className="mt-5 space-y-2.5" aria-labelledby={actionsTitleId}>
            <div className="flex items-center gap-2 px-1">
              <ShieldAlert className="size-3.5 text-warning" aria-hidden />
              <h3 id={actionsTitleId} className="font-[family-name:var(--font-mono)] text-[10px] font-semibold uppercase tracking-[0.13em] text-muted-foreground">
                Acciones para revisar
              </h3>
            </div>
            {snapshot.pendingActions.map((action) => (
              <AssistantActionCard
                key={action.id}
                action={action}
                busy={activeActionId === action.id}
                disabled={bridge === null || isBusy}
                onApprove={() => void decideAction(action.id, 'approve')}
                onReject={() => void decideAction(action.id, 'reject')}
              />
            ))}
          </section>
        ) : null}
        <div ref={endRef} />
      </div>

      <footer className="border-t border-border bg-background/45 p-3">
        {controlError ? <p className="mb-2 text-xs leading-4 text-destructive" role="alert">{controlError}</p> : null}
        {snapshot.error ? <p className="mb-2 text-xs leading-4 text-warning" role="status">{snapshot.error}</p> : null}

        <form onSubmit={submit}>
          <label className="sr-only" htmlFor={composerId}>Escribe a Codex</label>
          <div className="relative">
            <textarea
              id={composerId}
              value={draft}
              onChange={(event) => setDraft(event.currentTarget.value)}
              onKeyDown={onComposerKeyDown}
              disabled={!isReady || isBusy || bridge === null}
              maxLength={ASSISTANT_MESSAGE_MAX_LENGTH}
              rows={3}
              placeholder={composerPlaceholder(snapshot.availability, isBusy)}
              className="min-h-22 w-full resize-none border border-input bg-surface/80 px-3 py-2.5 pr-11 text-sm leading-5 text-foreground shadow-xs outline-none transition-[border-color,box-shadow,background-color] placeholder:text-muted-foreground/75 focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/40 disabled:cursor-not-allowed disabled:opacity-60"
            />
            {snapshot.busy ? (
              <Button
                type="button"
                variant="outline"
                size="icon-xs"
                className="absolute right-2 bottom-2"
                onClick={() => void cancelTurn()}
                disabled={cancelPending}
                aria-label="Cancelar respuesta"
                title="Cancelar respuesta"
              >
                <Square aria-hidden />
              </Button>
            ) : (
              <Button
                type="submit"
                size="icon-xs"
                className="absolute right-2 bottom-2"
                disabled={!canSend}
                aria-label="Enviar mensaje"
                title="Enviar mensaje"
              >
                <Send aria-hidden />
              </Button>
            )}
          </div>
          <p className="mt-1.5 flex items-center justify-between gap-2 font-[family-name:var(--font-mono)] text-[10px] text-muted-foreground/80">
            <span>ENTER ENVÍA · MAYÚS+ENTER SALTO</span>
            <span>{draft.length}/{ASSISTANT_MESSAGE_MAX_LENGTH}</span>
          </p>
        </form>
      </footer>
    </aside>
  );
}

function ConversationContent({ snapshot }: { snapshot: AssistantSnapshot }): ReactElement {
  if (snapshot.messages.length === 0) {
    return (
      <div className="flex min-h-44 flex-col items-center justify-center px-6 text-center">
        {snapshot.busy ? (
          <LoaderCircle className="size-5 animate-spin text-primary" aria-hidden />
        ) : (
          <MessageCircle className="size-6 text-primary/75" aria-hidden />
        )}
        <p className="mt-3 text-sm font-medium text-foreground">
          {snapshot.busy ? 'Codex está preparando la respuesta…' : '¿En qué trabajamos?'}
        </p>
        <p className="mt-1 max-w-64 text-xs leading-5 text-muted-foreground">
          El contexto visible de Studio se adjunta a cada mensaje; los cambios siempre se revisan antes de aplicarse.
        </p>
      </div>
    );
  }

  return (
    <div className="space-y-3">
      {snapshot.messages.map((message) => <AssistantMessageBubble key={message.id} message={message} />)}
    </div>
  );
}

function AssistantMessageBubble({ message }: { message: AssistantMessage }): ReactElement {
  const isUser = message.role === ASSISTANT_MESSAGE_ROLES.user;
  const isSystem = message.role === ASSISTANT_MESSAGE_ROLES.system;
  const content = message.content || (message.streaming ? 'Pensando…' : '');

  return (
    <article className={cn('flex', isUser ? 'justify-end' : 'justify-start')}>
      <div
        className={cn(
          'max-w-[92%] border px-3 py-2.5 text-sm leading-5 shadow-xs',
          isUser && 'border-primary/35 bg-primary/12 text-foreground',
          !isUser && !isSystem && 'border-border bg-card/75 text-foreground',
          isSystem && 'border-border/80 bg-muted/55 text-muted-foreground',
        )}
      >
        <div className="mb-1 flex items-center justify-between gap-3 font-[family-name:var(--font-mono)] text-[9px] uppercase tracking-[0.12em] text-muted-foreground">
          <span>{messageRoleLabel(message.role)}</span>
          <span>{timestampLabel(message.createdAt)}</span>
        </div>
        <p className="whitespace-pre-wrap break-words">{content}</p>
        {message.streaming ? (
          <span className="mt-2 inline-flex items-center gap-1.5 font-[family-name:var(--font-mono)] text-[10px] uppercase tracking-[0.1em] text-primary">
            <LoaderCircle className="size-3 animate-spin" aria-hidden /> Generando
          </span>
        ) : null}
      </div>
    </article>
  );
}

function AssistantActionCard({
  action,
  busy,
  disabled,
  onApprove,
  onReject,
}: {
  action: AssistantAction;
  busy: boolean;
  disabled: boolean;
  onApprove(): void;
  onReject(): void;
}): ReactElement {
  const status = action.status ?? ASSISTANT_ACTION_STATUSES.pending;
  const risk = RISK_COPY[action.risk];
  const needsApproval = action.requiresApproval !== false && status === ASSISTANT_ACTION_STATUSES.pending;

  return (
    <article className="border border-warning/35 bg-warning/[0.045] p-3 shadow-xs">
      <div className="flex items-start gap-2.5">
        <ShieldAlert className="mt-0.5 size-4 shrink-0 text-warning" aria-hidden />
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-start justify-between gap-2">
            <h4 className="text-sm font-semibold text-foreground">{action.title}</h4>
            <span className={cn('border px-1.5 py-0.5 font-[family-name:var(--font-mono)] text-[9px] uppercase tracking-[0.08em]', risk.className)}>
              {risk.label}
            </span>
          </div>
          {action.description ? <p className="mt-1 text-xs leading-5 text-muted-foreground">{action.description}</p> : null}
          <p className="mt-2 break-all font-[family-name:var(--font-mono)] text-[10px] text-primary/85">{action.operation}</p>
        </div>
      </div>

      {action.preview?.summary || action.preview?.fields?.length ? (
        <div className="mt-3 border border-border/70 bg-background/45 p-2.5">
          {action.preview.summary ? <p className="text-xs leading-5 text-foreground">{action.preview.summary}</p> : null}
          {action.preview.fields?.length ? (
            <dl className={cn('space-y-1.5 text-[11px]', action.preview.summary && 'mt-2')}>
              {action.preview.fields.map((field) => (
                <div key={`${field.label}-${field.value}`} className="grid grid-cols-[minmax(0,0.85fr)_minmax(0,1.15fr)] gap-2">
                  <dt className="truncate text-muted-foreground">{field.label}</dt>
                  <dd className="break-words text-foreground">{field.value}</dd>
                </div>
              ))}
            </dl>
          ) : null}
        </div>
      ) : null}

      <div className="mt-3 flex flex-wrap items-center justify-between gap-2">
        <ActionMeta action={action} status={status} />
        {needsApproval ? (
          <div className="flex gap-1.5">
            <Button type="button" variant="ghost" size="xs" onClick={onReject} disabled={disabled || busy}>
              <X aria-hidden /> Rechazar
            </Button>
            <Button type="button" size="xs" onClick={onApprove} disabled={disabled || busy}>
              {busy ? <LoaderCircle className="animate-spin" aria-hidden /> : <Check aria-hidden />}
              Aprobar
            </Button>
          </div>
        ) : null}
      </div>
    </article>
  );
}

function ActionMeta({ action, status }: { action: AssistantAction; status: string }): ReactElement {
  if (status !== ASSISTANT_ACTION_STATUSES.pending) {
    return <span className="font-[family-name:var(--font-mono)] text-[10px] uppercase tracking-[0.08em] text-muted-foreground">{actionStatusLabel(status)}</span>;
  }
  if (action.expiresAt) {
    return (
      <span className="inline-flex items-center gap-1 font-[family-name:var(--font-mono)] text-[10px] text-muted-foreground">
        <Clock3 className="size-3" aria-hidden /> Caduca {timestampLabel(action.expiresAt)}
      </span>
    );
  }
  return <span className="text-[11px] text-muted-foreground">Requiere tu aprobación exacta.</span>;
}

function AvailabilityDot({ availability }: { availability: AssistantAvailability }): ReactElement {
  if (availability === ASSISTANT_AVAILABILITY.ready) {
    return <CircleCheck className="size-3.5 text-success" aria-hidden />;
  }
  if (availability === ASSISTANT_AVAILABILITY.starting) {
    return <LoaderCircle className="size-3.5 animate-spin text-primary" aria-hidden />;
  }
  return <TriangleAlert className="size-3.5 text-warning" aria-hidden />;
}

function availabilityLabel(availability: AssistantAvailability): string {
  switch (availability) {
    case ASSISTANT_AVAILABILITY.ready:
      return 'Codex conectado';
    case ASSISTANT_AVAILABILITY.starting:
      return 'Conectando con Codex';
    case ASSISTANT_AVAILABILITY.unavailable:
      return 'No disponible en el navegador';
    case ASSISTANT_AVAILABILITY.error:
      return 'Codex necesita atención';
  }
}

function composerPlaceholder(availability: AssistantAvailability, busy: boolean): string {
  if (busy) return 'Codex está respondiendo…';
  if (availability === ASSISTANT_AVAILABILITY.ready) return 'Pregunta sobre este contexto…';
  if (availability === ASSISTANT_AVAILABILITY.starting) return 'Conectando con Codex…';
  return 'El asistente no está disponible.';
}

function messageRoleLabel(role: AssistantMessage['role']): string {
  if (role === ASSISTANT_MESSAGE_ROLES.user) return 'Tú';
  if (role === ASSISTANT_MESSAGE_ROLES.system) return 'Studio';
  return 'Codex';
}

function actionStatusLabel(status: string): string {
  switch (status) {
    case ASSISTANT_ACTION_STATUSES.approved:
      return 'Aprobada';
    case ASSISTANT_ACTION_STATUSES.completed:
      return 'Completada';
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

function timestampLabel(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '';
  return TIME_FORMATTER.format(date);
}
