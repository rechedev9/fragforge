'use client';

import { useState } from 'react';
import { AlertTriangle, RotateCcw } from 'lucide-react';
import type { Video } from '@/lib/api/types';
import { api } from '@/lib/api';
import { Card } from '@/components/ui/card';
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
    <Card className="flex-row items-center gap-4 border-destructive/40 py-4 pr-5 pl-4">
      <div className="grid size-10 shrink-0 place-items-center rounded-lg border border-destructive/40 bg-destructive/10">
        <AlertTriangle className="size-5 text-destructive" />
      </div>

      <div className="flex min-w-0 flex-1 flex-col gap-1">
        <p className="truncate font-semibold text-foreground">{video.title}</p>
        <p className="font-[family-name:var(--font-mono)] text-sm tabular-nums text-muted-foreground">
          {video.map}
          {video.score ? ` · ${video.score}` : ''}
        </p>
        <p className="mt-1 line-clamp-2 text-sm text-destructive">
          {video.failureReason ?? 'The reel failed on your rig.'}
        </p>
      </div>

      <div className="flex shrink-0 items-center gap-1">
        <Button variant="secondary" size="sm" onClick={onRetry} disabled={retrying}>
          <RotateCcw className="size-4" />
          {retrying ? 'Retrying…' : 'Retry'}
        </Button>
        <DeleteVideoButton video={video} onDeleted={onChange} />
      </div>
    </Card>
  );
}
