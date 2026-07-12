'use client';

import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import Link from 'next/link';
import { Film } from 'lucide-react';
import type { Video } from '@/lib/api/types';
import { api } from '@/lib/api';
import { startPollLoop } from '@/lib/poll-loop';
import { SectionEyebrow } from '@/components/brand/section-eyebrow';
import { StudioEmptyState } from '@/components/studio/empty-state';
import { StudioPageHeader } from '@/components/studio/page-header';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { RenderingCard } from '@/components/videos/rendering-card';
import { ReadyCard } from '@/components/videos/ready-card';
import { FailedCard } from '@/components/videos/failed-card';
import { VideoFilters, type VideoFormatFilter } from '@/components/videos/video-filters';

// Poll fast while a reel is advancing through the pipeline; once every reel is
// terminal (ready/failed) there is nothing to drive, so back off to an idle
// cadence to stop hammering the orchestrator. A newly created reel resumes fast
// polling on the next tick.
const FAST_POLL_MS = 1500;
const IDLE_POLL_MS = 10000;

function hasActiveReel(list: Video[] | undefined): boolean {
  return !!list && list.some((v) => v.status !== 'ready' && v.status !== 'failed');
}

function matchesFormat(video: Video, filter: VideoFormatFilter): boolean {
  if (filter === 'all') return true;
  return video.editConfig?.format === filter;
}

export default function VideosPage() {
  const [videos, setVideos] = useState<Video[] | null>(null);
  const [filter, setFilter] = useState<VideoFormatFilter>('all');

  // Guards against overlapping listVideos() calls if a manual refresh is still
  // in flight when the next poll tick fires (the poll loop itself never overlaps
  // its own ticks).
  const inFlight = useRef(false);

  const reload = useCallback(async (): Promise<Video[] | undefined> => {
    if (inFlight.current) return undefined;
    inFlight.current = true;
    try {
      return await api.listVideos();
    } finally {
      inFlight.current = false;
    }
  }, []);

  const refresh = useCallback(async () => {
    const next = await reload();
    if (next) setVideos(next);
  }, [reload]);

  useEffect(() => {
    let active = true;

    // A tick that throws (transient proxy/orchestrator hiccup) must not kill the
    // loop: startPollLoop catches it and reschedules at the idle cadence, so the
    // page keeps polling instead of freezing every reel in its last state.
    const stop = startPollLoop({
      tick: async () => {
        const next = await reload();
        if (active && next) setVideos(next);
        // `next` is undefined only if a manual refresh raced this tick; treat
        // that as "keep polling fast" so a just-created reel is never stranded.
        return next === undefined || hasActiveReel(next) ? 'fast' : 'idle';
      },
      fastMs: FAST_POLL_MS,
      idleMs: IDLE_POLL_MS,
    });

    return () => {
      active = false;
      stop();
    };
  }, [reload]);

  const visible = useMemo(() => (videos ?? []).filter((v) => matchesFormat(v, filter)), [videos, filter]);

  let content: ReactNode;
  if (videos === null) {
    content = <LibrarySkeleton />;
  } else if (videos.length === 0) {
    content = <EmptyState />;
  } else {
    content = (
      <LibrarySections videos={visible} allVideos={videos} onChange={() => void refresh()} />
    );
  }

  return (
    <div className="flex flex-col gap-8">
      <StudioPageHeader
        number={4}
        label="BIBLIOTECA"
        title="TUS REELS"
        description="Sigue cada captura desde la cola hasta el MP4 y publica solo lo que merece salir del rig."
        actions={
          videos !== null && videos.length > 0 ? (
            <VideoFilters filter={filter} onFilterChange={setFilter} />
          ) : undefined
        }
      />

      {content}
    </div>
  );
}

/**
 * All non-failed reels share one flat grid regardless of pipeline stage — the
 * mockup's BIBLIOTECA shows queued/capturing/editing/ready cards side by side
 * at equal width; each card already carries its own stage treatment (REC dot,
 * progress bar, "LISTO" tag), so a stage-grouping header on top of that is
 * redundant. Failed reels stay in their own "needs attention" alert row above
 * the grid since they need a distinct, actionable presentation, not just a
 * stage label.
 */
function LibrarySections({
  videos,
  allVideos,
  onChange,
}: {
  videos: Video[];
  allVideos: Video[];
  onChange(): void;
}) {
  const failed = videos.filter((v) => v.status === 'failed');
  const active = videos.filter((v) => v.status !== 'failed');
  let activeContent: ReactNode = null;
  if (active.length > 0) {
    activeContent = (
      <div className="grid grid-cols-[repeat(auto-fill,minmax(min(100%,250px),300px))] justify-start gap-5">
        {active.map((v) =>
          v.status === 'ready' ? (
            <ReadyCard key={v.id} video={v} onDeleted={onChange} />
          ) : (
            <RenderingCard key={v.id} video={v} />
          ),
        )}
      </div>
    );
  } else if (failed.length === 0 && allVideos.length > 0) {
    activeContent = (
      <div className="studio-panel flex max-w-xl items-center gap-4 px-5 py-4" role="status">
        <span className="grid size-10 shrink-0 place-items-center border border-border-strong bg-background/45 text-primary">
          <Film className="size-4" aria-hidden />
        </span>
        <div>
          <p className="font-[family-name:var(--font-display)] text-sm font-bold uppercase text-foreground">
            No hay reels en este formato
          </p>
          <p className="mt-1 text-sm text-muted-foreground">
            Cambia el filtro para volver a ver el resto de la biblioteca.
          </p>
        </div>
      </div>
    );
  }

  return (
    <>
      {failed.length > 0 ? (
        <section className="space-y-4">
          <SectionEyebrow label="NECESITA ATENCIÓN" count={failed.length} />
          <div className="space-y-3">
            {failed.map((v) => (
              <FailedCard key={v.id} video={v} onChange={onChange} />
            ))}
          </div>
        </section>
      ) : null}

      {activeContent}
    </>
  );
}

function LibrarySkeleton() {
  return (
    <div className="grid grid-cols-[repeat(auto-fill,minmax(min(100%,250px),300px))] justify-start gap-5">
      {Array.from({ length: 4 }).map((_, i) => (
        <div key={i} className="studio-panel space-y-4 p-4">
          <Skeleton className="aspect-video w-full rounded-lg" />
          <Skeleton className="h-4 w-2/3" />
          <Skeleton className="h-3 w-1/2" />
          <Skeleton className="h-9 w-full" />
        </div>
      ))}
    </div>
  );
}

function EmptyState() {
  return (
    <StudioEmptyState
      icon={Film}
      title="Todavía no hay reels"
      description="Elige una jugada y FragForge seguirá la captura, la edición y el render desde esta biblioteca."
      compact
      className="max-w-2xl"
      actions={
        <Button asChild>
          <Link href="/matches">BUSCAR JUGADAS</Link>
        </Button>
      }
      note="CAPTURA Y EDICIÓN EN TU RIG"
    />
  );
}
