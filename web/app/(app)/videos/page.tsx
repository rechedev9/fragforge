'use client';

import { useCallback, useEffect, useRef, useState } from 'react';
import { Film } from 'lucide-react';
import type { Video } from '@/lib/api/types';
import { api } from '@/lib/api';
import { SectionEyebrow } from '@/components/brand';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { RenderingCard } from '@/components/videos/rendering-card';
import { ReadyCard } from '@/components/videos/ready-card';
import { FailedCard } from '@/components/videos/failed-card';

const POLL_MS = 1500;

export default function VideosPage() {
  const [videos, setVideos] = useState<Video[] | null>(null);

  // Guards against overlapping listVideos() calls if one is still in flight
  // when the next interval tick fires.
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

    const tick = async () => {
      const next = await reload();
      if (active && next) setVideos(next);
    };

    void tick();
    const id = setInterval(() => void tick(), POLL_MS);
    return () => {
      active = false;
      clearInterval(id);
    };
  }, [reload]);

  return (
    <div className="flex flex-col gap-10">
      <header className="space-y-2">
        <h1 className="font-[family-name:var(--font-display)] text-3xl font-bold uppercase tracking-tight text-foreground sm:text-4xl">
          Library
        </h1>
        <p className="max-w-2xl text-sm text-muted-foreground">
          Your reels render on your own rig. Watch them advance through the
          pipeline, then publish the ones worth posting.
        </p>
      </header>

      {videos === null ? (
        <LibrarySkeleton />
      ) : videos.length === 0 ? (
        <EmptyState />
      ) : (
        <LibrarySections videos={videos} onChange={() => void refresh()} />
      )}
    </div>
  );
}

function LibrarySections({ videos, onChange }: { videos: Video[]; onChange: () => void }) {
  const failed = videos.filter((v) => v.status === 'failed');
  const rendering = videos.filter((v) => v.status !== 'ready' && v.status !== 'failed');
  const ready = videos.filter((v) => v.status === 'ready');

  return (
    <>
      {failed.length > 0 ? (
        <section className="space-y-4">
          <SectionEyebrow label="Needs attention" count={failed.length} />
          <div className="space-y-3">
            {failed.map((v) => (
              <FailedCard key={v.id} video={v} onChange={onChange} />
            ))}
          </div>
        </section>
      ) : null}

      {rendering.length > 0 ? (
        <section className="space-y-4">
          <SectionEyebrow label="Rendering" count={rendering.length} />
          <div className="space-y-3">
            {rendering.map((v) => (
              <RenderingCard key={v.id} video={v} />
            ))}
          </div>
        </section>
      ) : null}

      <section className="space-y-4">
        <SectionEyebrow label="Ready" count={ready.length} />
        {ready.length > 0 ? (
          <div className="grid gap-5 sm:grid-cols-2 lg:grid-cols-3">
            {ready.map((v) => (
              <ReadyCard key={v.id} video={v} onChange={onChange} />
            ))}
          </div>
        ) : (
          <p className="text-sm text-muted-foreground">
            No finished reels yet. Renders land here once they leave the pipeline.
          </p>
        )}
      </section>
    </>
  );
}

function LibrarySkeleton() {
  return (
    <div className="space-y-10">
      <section className="space-y-4">
        <SectionEyebrow label="Rendering" />
        <Skeleton className="h-28 w-full rounded-xl" />
      </section>
      <section className="space-y-4">
        <SectionEyebrow label="Ready" />
        <div className="grid gap-5 sm:grid-cols-2 lg:grid-cols-3">
          {Array.from({ length: 3 }).map((_, i) => (
            <div key={i} className="space-y-3">
              <Skeleton className="aspect-video w-full rounded-xl" />
              <Skeleton className="h-4 w-2/3" />
              <Skeleton className="h-3 w-1/3" />
            </div>
          ))}
        </div>
      </section>
    </div>
  );
}

function EmptyState() {
  return (
    <div className="rounded-xl border border-dashed border-border bg-card/40 px-6 py-16 text-center">
      <div className="mx-auto grid size-14 place-items-center rounded-xl border border-border bg-secondary/60">
        <Film className="size-6 text-muted-foreground" />
      </div>
      <h3 className="mt-4 font-[family-name:var(--font-display)] text-lg font-semibold text-foreground">
        Nothing in the library yet
      </h3>
      <p className="mx-auto mt-1 max-w-sm text-sm text-muted-foreground">
        Find a highlight in one of your matches and we&apos;ll capture it into a
        reel on your rig.
      </p>
      <div className="mt-6 flex justify-center">
        <Button asChild>
          <a href="/matches">Find highlights</a>
        </Button>
      </div>
    </div>
  );
}
