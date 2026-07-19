'use client';

import { useState } from 'react';
import { AlertTriangle, RotateCcw } from 'lucide-react';
import type { Video } from '@/lib/api/types';
import { api } from '@/lib/api';
import { parseFailureReason } from '@/lib/api/failure-reason';
import { Button } from '@/components/ui/button';
import { DeleteVideoButton } from '@/components/videos/delete-video-button';

/**
 * A reel whose pipeline failed on the rig. Shows the orchestrator's failure
 * reason and a Retry that re-drives the failed stage (re-record or re-render);
 * on success the reel rejoins the Rendering then Ready sections via the
 * reconcile loop. Without this, a failed reel used to vanish from the Library.
 * When the reel is unrecoverable (its orchestrator job is gone), Retry could
 * never succeed, so the card hides it and points the user to delete + re-forge.
 */
export function FailedCard({ video, onChange }: { video: Video; onChange: () => void }) {
  const [retrying, setRetrying] = useState(false);
  const unrecoverable = video.unrecoverable ?? false;
  const failure = parseFailureReason(video.failureReason);
  // A demo-incompatible failure is deterministic in the .dem itself: retry can
  // never help, so we hide Retry and show the Spanish explanation. Unrecoverable
  // reels keep their own branch.
  const demoIncompatible = !unrecoverable && failure.kind === 'demo-incompatible';
  const canRetry = !unrecoverable && !demoIncompatible;

  function footerHint(): string {
    if (unrecoverable) return 'Elimina la tarjeta y sube la demo otra vez para forjarla de nuevo.';
    if (demoIncompatible) {
      return 'Este fallo es determinista: reintentar no ayudará. Elimina la tarjeta o forja con una demo reciente.';
    }
    return 'Reintenta para retomar desde la etapa que falló';
  }

  async function onRetry() {
    if (retrying) return;
    setRetrying(true);
    try {
      await api.retryVideo(video.id);
      onChange();
    } finally {
      // Always re-arm the button: a retry can resolve while the reel stays
      // failed (e.g. capture still unconfigured), and a card stuck at
      // "Reintentando…" would need a reload to try again.
      setRetrying(false);
    }
  }

  return (
    <div className="studio-panel flex flex-col gap-4 border-destructive/45 px-4 py-4 sm:flex-row sm:items-center sm:px-5">
      <div className="grid size-12 shrink-0 place-items-center border border-destructive/45 bg-destructive/10">
        <AlertTriangle className="size-5 text-destructive" aria-hidden />
      </div>

      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-2">
          <span className="inline-flex min-h-7 items-center border border-destructive/35 bg-destructive/10 px-2.5 font-[family-name:var(--font-mono)] text-[10px] uppercase tracking-[0.14em] text-destructive">
            ERROR DE PIPELINE
          </span>
          <p className="font-[family-name:var(--font-mono)] text-xs uppercase tracking-[0.1em] text-muted-foreground">
            {video.map}
            {video.score ? ` · ${video.score}` : ''}
          </p>
        </div>
        <p className="mt-2 truncate font-[family-name:var(--font-display)] text-base font-bold text-foreground">
          {video.title}
        </p>
        <p className="mt-1 line-clamp-2 text-sm leading-5 text-destructive">
          {/* Unrecoverable reels carry an internal English reason; show the
              Spanish explanation instead of leaking it into the UI. A
              demo-incompatible reason is machine-readable, so we surface its
              parsed Spanish message rather than the raw prefix. */}
          {unrecoverable
            ? 'El orquestador ya no tiene esta captura (puede haberse reiniciado).'
            : failure.message}
        </p>
        <p className="mt-3 border-t border-destructive/20 pt-3 font-[family-name:var(--font-mono)] text-[10px] uppercase tracking-[0.12em] text-muted-foreground">
          {footerHint()}
        </p>
      </div>

      <div className="flex w-full shrink-0 items-center gap-2 sm:w-auto">
        {canRetry && (
          <Button className="flex-1 sm:flex-none" variant="secondary" size="sm" onClick={onRetry} disabled={retrying}>
            <RotateCcw className="size-4" />
            {retrying ? 'Reintentando…' : 'Reintentar'}
          </Button>
        )}
        <DeleteVideoButton video={video} onDeleted={onChange} />
      </div>
    </div>
  );
}
