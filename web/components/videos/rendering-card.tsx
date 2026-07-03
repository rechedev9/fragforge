'use client';

import { Film, Music } from 'lucide-react';
import type { Video } from '@/lib/api/types';
import { cn } from '@/lib/utils';
import { Card } from '@/components/ui/card';
import { PipelineSteps, RecDot, ReelCover } from '@/components/brand';

/**
 * A render still in flight (queued / recording / composing). Shows a dimmed
 * thumbnail, the PipelineSteps stepper, a subtle shimmer sweep, and a
 * "LIVE ON YOUR RIG" REC indicator while capturing on the player's rig.
 */
export function RenderingCard({ video }: { video: Video }) {
  const ModeIcon = video.mode === 'music' ? Music : Film;
  const isCapturing = video.status === 'recording';

  return (
    <Card
      className={cn(
        'neon-shimmer relative flex-row items-center gap-4 overflow-hidden py-4 pr-5 pl-4',
      )}
    >
      <div className="relative aspect-video w-28 shrink-0 overflow-hidden rounded-lg border border-border bg-muted sm:w-36">
        <ReelCover seed={video.id} plain className="size-full opacity-50" />
        <div className="absolute inset-0 grid place-items-center">
          <ModeIcon className="size-5 text-muted-foreground" />
        </div>
      </div>

      <div className="flex min-w-0 flex-1 flex-col gap-3">
        <div className="flex min-w-0 items-start justify-between gap-3">
          <div className="min-w-0">
            <p className="truncate font-semibold text-foreground">{video.title}</p>
            <p className="mt-0.5 font-[family-name:var(--font-mono)] text-sm tabular-nums text-muted-foreground">
              {video.map} · {video.score}
            </p>
          </div>
          {isCapturing ? <RecDot className="shrink-0" /> : null}
        </div>

        <PipelineSteps status={video.status} />
      </div>
    </Card>
  );
}
