'use client';

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Film } from 'lucide-react';
import type { Video, VideoStatus } from '@/lib/api/types';
import { api } from '@/lib/api';
import { SectionEyebrow } from '@/components/brand/section-eyebrow';
import { PipelineSteps } from '@/components/brand/pipeline-steps';
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

/**
 * The busiest pipeline stage across the whole library, for the page-level
 * footer: capturing beats composing beats queued beats ready, so the footer
 * always points at wherever the real bottleneck is right now instead of
 * fabricating a per-item percentage the API does not report.
 */
function busiestStatus(videos: Video[]): VideoStatus {
  if (videos.some((v) => v.status === 'recording')) return 'recording';
  if (videos.some((v) => v.status === 'composing')) return 'composing';
  if (videos.some((v) => v.status === 'queued')) return 'queued';
  return 'ready';
}

function matchesFormat(video: Video, filter: VideoFormatFilter): boolean {
  if (filter === 'all') return true;
  return video.editConfig?.format === filter;
}

export default function VideosPage() {
  const [videos, setVideos] = useState<Video[] | null>(null);
  const [filter, setFilter] = useState<VideoFormatFilter>('all');

  // Guards against overlapping listVideos() calls if one is still in flight
  // when the next interval tick fires.
  const inFlight = useRef(false);
  // The pending poll timer, tracked in a ref so unmount always clears the most
  // recently scheduled one (the tick reschedules across an await boundary).
  const pollTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);

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

    const tick = async () => {
      const next = await reload();
      if (!active) return;
      if (next) setVideos(next);
      // `next` is undefined only if a manual refresh raced this tick; treat that
      // as "keep polling fast" so a just-created reel is never stranded.
      const delay = next === undefined || hasActiveReel(next) ? FAST_POLL_MS : IDLE_POLL_MS;
      pollTimer.current = setTimeout(() => void tick(), delay);
    };

    void tick();
    return () => {
      active = false;
      if (pollTimer.current) clearTimeout(pollTimer.current);
    };
  }, [reload]);

  const visible = useMemo(() => (videos ?? []).filter((v) => matchesFormat(v, filter)), [videos, filter]);

  return (
    <div className="flex flex-col gap-10">
      <header className="flex flex-col gap-2.5">
        <SectionEyebrow number={4} label="BIBLIOTECA" />
        <div className="flex flex-col gap-3 sm:flex-row sm:items-baseline sm:justify-between sm:gap-6">
          <h1 className="font-[family-name:var(--font-display)] text-3xl font-bold leading-none tracking-tight text-foreground sm:text-[34px]">
            TUS REELS
          </h1>
          {videos !== null && videos.length > 0 ? <VideoFilters filter={filter} onFilterChange={setFilter} /> : null}
        </div>
        <p className="max-w-2xl text-sm text-muted-foreground">
          Renderizan en tu propio rig. Publica los que valgan la pena.
        </p>
      </header>

      {videos === null ? (
        <LibrarySkeleton />
      ) : videos.length === 0 ? (
        <EmptyState />
      ) : (
        <LibrarySections videos={visible} allVideos={videos} onChange={() => void refresh()} />
      )}
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
  onChange: () => void;
}) {
  const failed = videos.filter((v) => v.status === 'failed');
  const active = videos.filter((v) => v.status !== 'failed');

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

      {active.length > 0 ? (
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
          {active.map((v) =>
            v.status === 'ready' ? (
              <ReadyCard key={v.id} video={v} onChange={onChange} />
            ) : (
              <RenderingCard key={v.id} video={v} />
            ),
          )}
        </div>
      ) : (
        <p className="text-sm text-muted-foreground">
          Todavía no hay reels terminados. Los renders aterrizan aquí al salir del pipeline.
        </p>
      )}

      <div className="mt-auto flex items-center justify-center pt-6">
        <PipelineSteps status={busiestStatus(allVideos)} className="gap-x-3 text-[11.5px]" />
      </div>
    </>
  );
}

function LibrarySkeleton() {
  return (
    <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
      {Array.from({ length: 4 }).map((_, i) => (
        <div key={i} className="space-y-3">
          <Skeleton className="aspect-video w-full rounded-none" />
          <Skeleton className="h-4 w-2/3 rounded-none" />
          <Skeleton className="h-3 w-1/3 rounded-none" />
        </div>
      ))}
    </div>
  );
}

function EmptyState() {
  return (
    <div className="border border-dashed border-border bg-card/40 px-6 py-16 text-center">
      <div className="mx-auto grid size-14 place-items-center border border-border bg-secondary/60">
        <Film className="size-6 text-muted-foreground" />
      </div>
      <h3 className="mt-4 font-[family-name:var(--font-display)] text-lg font-bold uppercase tracking-tight text-foreground">
        Todavía no hay nada en la biblioteca
      </h3>
      <p className="mx-auto mt-1 max-w-sm text-sm text-muted-foreground">
        Encuentra una jugada en una de tus partidas y la capturaremos en un reel en tu rig.
      </p>
      <div className="mt-6 flex justify-center">
        <a
          href="/matches"
          className="neon-notch inline-flex h-9 items-center bg-primary px-4 font-[family-name:var(--font-display)] text-sm font-bold tracking-[0.06em] text-primary-foreground transition-colors hover:bg-primary/90"
        >
          BUSCAR JUGADAS
        </a>
      </div>
    </div>
  );
}
