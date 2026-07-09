'use client';

import { useState } from 'react';
import { AlertTriangle, RotateCcw } from 'lucide-react';
import type { Video } from '@/lib/api/types';
import { api } from '@/lib/api';
import { PipelineSteps } from '@/components/brand/pipeline-steps';
import { Button } from '@/components/ui/button';
import { DeleteVideoButton } from '@/components/videos/delete-video-button';

/**
 * A reel whose pipeline failed on the rig. Shows the orchestrator's failure
 * reason and a Retry that re-drives the failed stage (re-record or re-render);
 * on success the reel rejoins the Rendering then Ready sections via the
 * reconcile loop. Without this, a failed reel used to vanish from the Library.
 */
export function FailedCard({ video, onChange }: { video: Video; onChange: () => void }) {
  const [retrying, setRetrying] = useState(false);

  async function onRetry() {
    if (retrying) return;
    setRetrying(true);
    try {
      await api.retryVideo(video.id);
      onChange();
    } catch {
      setRetrying(false); // surface the button again so the user can try once more
    }
  }

  return (
    <div className="studio-panel neon-brackets flex flex-col gap-4 border-destructive/45 px-4 py-4 [--neon-bracket-color:var(--destructive)] sm:flex-row sm:items-center sm:px-5">
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
          {video.failureReason ?? 'El reel falló en tu equipo.'}
        </p>
        <div className="mt-3 border-t border-destructive/20 pt-3">
          <PipelineSteps status={video.status} className="gap-x-2 text-[10px]" />
        </div>
      </div>

      <div className="flex w-full shrink-0 items-center gap-2 sm:w-auto">
        <Button className="flex-1 sm:flex-none" variant="secondary" size="sm" onClick={onRetry} disabled={retrying}>
          <RotateCcw className="size-4" />
          {retrying ? 'Reintentando…' : 'Reintentar'}
        </Button>
        <DeleteVideoButton video={video} onDeleted={onChange} />
      </div>
    </div>
  );
}
