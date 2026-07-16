'use client';

import { useEffect, useRef, useState } from 'react';
import { Loader2, Trash2 } from 'lucide-react';
import { deleteErrorMessage } from '@/lib/delete-error';

/** How long the armed "¿BORRAR?" state waits before reverting on its own. */
const REVERT_MS = 8000;

/**
 * Two-step inline delete for a Partidas row (match or series). The first click
 * arms a destructive "¿BORRAR?" button; the second confirms. Blur or a short
 * timeout disarms it, so there is no native confirm() and no modal. While
 * deleting the button shows a spinner and is disabled; on success `onDeleted`
 * lets the page re-fetch, and on failure the row shows an inline Spanish message
 * (offline hint or the orchestrator's 409 explanation) instead of crashing.
 */
export function DeleteMatchButton({
  label,
  onConfirm,
  onDeleted,
}: {
  label: string;
  onConfirm: () => Promise<void>;
  onDeleted: () => void;
}) {
  const [armed, setArmed] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);

  function clearTimer() {
    if (timer.current) {
      clearTimeout(timer.current);
      timer.current = null;
    }
  }

  useEffect(() => clearTimer, []);

  function arm() {
    setError(null);
    setArmed(true);
    clearTimer();
    timer.current = setTimeout(() => setArmed(false), REVERT_MS);
  }

  function disarm() {
    clearTimer();
    setArmed(false);
  }

  async function confirm() {
    if (deleting) return;
    clearTimer();
    setDeleting(true);
    setError(null);
    try {
      await onConfirm();
      setArmed(false);
      onDeleted();
    } catch (err) {
      setError(deleteErrorMessage(err));
      setArmed(false);
    } finally {
      setDeleting(false);
    }
  }

  return (
    <div className="flex shrink-0 flex-col items-end gap-1">
      {armed || deleting ? (
        <button
          type="button"
          autoFocus
          onClick={confirm}
          onBlur={disarm}
          disabled={deleting}
          aria-label={`Confirmar borrar ${label}`}
          className="inline-flex h-11 items-center justify-center gap-1.5 rounded-md border border-destructive/60 bg-destructive/15 px-3 font-[family-name:var(--font-mono)] text-xs uppercase tracking-[0.14em] text-destructive transition-colors hover:bg-destructive/25 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background disabled:opacity-60"
        >
          {deleting ? <Loader2 className="size-3.5 animate-spin" aria-hidden /> : <Trash2 className="size-3.5" aria-hidden />}
          {deleting ? 'BORRANDO…' : '¿BORRAR?'}
        </button>
      ) : (
        <button
          type="button"
          onClick={arm}
          aria-label={`Borrar ${label}`}
          className="inline-flex size-11 items-center justify-center rounded-md border border-border-strong bg-background/45 text-muted-foreground transition-colors hover:border-destructive/60 hover:bg-destructive/10 hover:text-destructive focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background"
        >
          <Trash2 className="size-4" aria-hidden />
        </button>
      )}
      {error ? (
        <p
          role="status"
          className="max-w-[13rem] text-right font-[family-name:var(--font-mono)] text-[10px] leading-tight text-destructive"
        >
          {error}
        </p>
      ) : null}
    </div>
  );
}
