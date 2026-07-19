'use client';

import { use, useEffect, useRef, useState, type ReactNode } from 'react';
import Link from 'next/link';
import { Layers, ChevronRight } from 'lucide-react';
import { api } from '@/lib/api';
import { SERVICE_UNAVAILABLE_CODE } from '@/lib/api/types';
import type { SeriesDemo, Video } from '@/lib/api/types';
import {
  isSeriesId,
  seriesReelIsActive,
  seriesReelLabel,
  seriesReelTone,
  seriesStatusLabel,
  seriesStatusTone,
  seriesStatusIsPending,
  seriesStatusIsForgeable,
  summarizeSeriesStatuses,
  seriesTitle,
  type SeriesStatusTone,
} from '@/lib/series-status';
import { prettyMapName } from '@/lib/format';
import { navSection } from '@/lib/nav';
import { groupSeriesDemos, representativeSeriesStatus, type SeriesGroup } from '@/lib/series-grouping';
import { startPollLoop } from '@/lib/poll-loop';
import { StudioEmptyState } from '@/components/studio/empty-state';
import { StudioPageHeader } from '@/components/studio/page-header';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { cn } from '@/lib/utils';

// A series detail belongs to the demo-upload journey, so it shares its number.
const UPLOAD_NAV = navSection('/upload');

/** Fast while any map is still working, relaxed once the series has settled. */
const FAST_MS = 2500;
const IDLE_MS = 8000;

/** True when an API error means the local analysis service is unreachable. */
function isServiceUnavailable(err: unknown): boolean {
  return (err as { code?: string } | null)?.code === SERVICE_UNAVAILABLE_CODE;
}

/** Each demo's headline: prettified map name, else file name, else its position. */
function demoTitle(demo: SeriesDemo, index: number): string {
  if (demo.match) return prettyMapName(demo.match.map);
  if (demo.fileName) return demo.fileName;
  return `Mapa ${index + 1}`;
}

/**
 * Header description built from the real status buckets, omitting empty ones:
 * "2 mapas con jugadas listas · 1 analizando · 1 fallido · 1 sin jugador".
 * The ready bucket spans every forgeable status (parsing done through done), so
 * its copy stays true whether a map is grabando, renderizando or completada.
 * Only genuinely pending maps are ever described as being analyzed; settled
 * ones (failed, or scanned without the chosen player) get their own bucket.
 */
function seriesDescription(statuses: readonly string[]): string {
  const { ready, pending, failed, skipped } = summarizeSeriesStatuses(statuses);
  const parts: string[] = [];
  if (ready > 0) parts.push(ready === 1 ? '1 mapa con jugadas listas' : `${ready} mapas con jugadas listas`);
  if (pending > 0) parts.push(`${pending} analizando`);
  if (failed > 0) parts.push(failed === 1 ? '1 fallido' : `${failed} fallidos`);
  if (skipped > 0) parts.push(`${skipped} sin jugador`);
  if (parts.length === 0) return 'Sin demos en la serie.';
  return `${parts.join(' · ')}.`;
}

/**
 * Each map's newest reel: the Library reels that belong to this series' jobs,
 * keyed by job. listVideos returns reels newest-first, so the first hit per
 * job is the one the map card should describe.
 */
function latestReelPerJob(demos: readonly SeriesDemo[], videos: readonly Video[]): ReadonlyMap<string, Video> {
  const jobIds = new Set(demos.map((d) => d.jobId));
  const byJob = new Map<string, Video>();
  for (const video of videos) {
    if (video.jobId === undefined || !jobIds.has(video.jobId)) continue;
    if (!byJob.has(video.jobId)) byJob.set(video.jobId, video);
  }
  return byJob;
}

/** Pill colours per status tone, matching the app's cyan/amber/destructive language. */
const TONE_CLASSES: Record<SeriesStatusTone, string> = {
  pending: 'border-border bg-muted/60 text-muted-foreground',
  ready: 'border-primary/40 bg-primary/15 text-primary',
  progress: 'border-amber-400/30 bg-amber-400/10 text-amber-400',
  done: 'border-emerald-400/30 bg-emerald-400/10 text-emerald-400',
  failed: 'border-destructive/30 bg-destructive/10 text-destructive',
};

/**
 * Series view (/series/[id]) — the demos uploaded together as one bo3/bo5. It
 * lists every map with its map/score and live status, links each ready map into
 * its highlight picker, and polls the local orchestrator until every map has
 * settled. Reached from the /upload series flow after the picked player is
 * parsed on each map.
 */
export default function SeriesPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = use(params);
  const valid = isSeriesId(id);

  const [demos, setDemos] = useState<SeriesDemo[] | null>(null);
  const [reelByJob, setReelByJob] = useState<ReadonlyMap<string, Video>>(new Map());
  const [loaded, setLoaded] = useState(false);
  const [loadError, setLoadError] = useState<'offline' | 'generic' | null>(null);
  // Read the latest demos inside the poll tick without re-subscribing the loop:
  // a transient poll failure must not wipe an already-loaded list.
  const demosRef = useRef<SeriesDemo[] | null>(null);

  useEffect(() => {
    if (!valid) return;
    // The App Router reuses this page instance across dynamic-param changes, so
    // the effect must reset every piece of series state before polling the new
    // id; otherwise the previous series' demos linger and the loading state
    // never re-renders when switching series.
    setDemos(null);
    setReelByJob(new Map());
    setLoaded(false);
    setLoadError(null);
    demosRef.current = null;

    let active = true;
    let stopLoop: (() => void) | undefined;
    const stop = startPollLoop({
      tick: async () => {
        try {
          // listVideos also runs the reel reconcile tick, so keeping this page
          // open is enough to drive every queued reel through record → render;
          // the user never has to visit the Library for the queue to advance.
          const [list, videos] = await Promise.all([api.getSeries(id), api.listVideos()]);
          if (!active) return 'idle';
          const reels = latestReelPerJob(list, videos);
          demosRef.current = list;
          setDemos(list);
          setReelByJob(reels);
          setLoadError(null);
          setLoaded(true);
          const pending =
            list.some((d) => seriesStatusIsPending(d.status)) ||
            Array.from(reels.values()).some((v) => seriesReelIsActive(v.status));
          // A settled series (no map still working, no reel still forging) does
          // one fetch, renders, and stops: keep polling only while something is
          // pending. stopLoop is assigned before this async tick can reach here
          // (the tick suspends on the awaits above), so the call is safe.
          if (!pending) stopLoop?.();
          return pending ? 'fast' : 'idle';
        } catch (err) {
          if (!active) return 'idle';
          setLoaded(true);
          // Only surface an error screen before the first successful load; once
          // demos are on screen, keep them and let the next tick recover.
          if (demosRef.current === null) {
            setLoadError(isServiceUnavailable(err) ? 'offline' : 'generic');
          }
          return 'idle';
        }
      },
      fastMs: FAST_MS,
      idleMs: IDLE_MS,
    });
    stopLoop = stop;
    return () => {
      active = false;
      stop();
    };
  }, [id, valid]);

  if (!valid) {
    return (
      <StudioEmptyState
        icon={Layers}
        title="Serie no encontrada"
        description="Ese enlace de serie no es válido. Sube tus demos para empezar una serie nueva."
        actions={
          <Button asChild className="font-[family-name:var(--font-display)] tracking-[0.06em]">
            <Link href="/upload">SUBIR DEMOS</Link>
          </Button>
        }
      />
    );
  }

  if (!loaded) {
    return <LoadingState />;
  }

  if (demos === null && loadError) {
    const offline = loadError === 'offline';
    return (
      <StudioEmptyState
        icon={Layers}
        title={offline ? 'Servicio de análisis offline' : 'No se pudo cargar la serie'}
        description={
          offline
            ? 'Arranca el servicio de análisis local y vuelve a intentarlo.'
            : 'Hubo un problema al cargar esta serie. Recarga la página para reintentar.'
        }
        actions={
          <Button asChild variant="secondary" className="font-[family-name:var(--font-display)] tracking-[0.06em]">
            <Link href="/upload">SUBIR DEMOS</Link>
          </Button>
        }
      />
    );
  }

  const list = demos ?? [];
  if (list.length === 0) {
    return (
      <StudioEmptyState
        icon={Layers}
        title="Esta serie está vacía"
        description="No hay demos en esta serie. Sube las demos de tu bo3/bo5 para forjar sus highlights."
        actions={
          <Button asChild className="font-[family-name:var(--font-display)] tracking-[0.06em]">
            <Link href="/upload">SUBIR DEMOS</Link>
          </Button>
        }
      />
    );
  }

  // HLTV-style downloads split one map into several .dem parts; fold them back
  // into one logical map card so a bo3 reads "SERIE DE 3 MAPAS", not 4.
  const groups = groupSeriesDemos(list);

  return (
    <div className="flex flex-col gap-8 sm:gap-10">
      <StudioPageHeader
        number={Number(UPLOAD_NAV.number)}
        label="SERIE"
        title={seriesTitle(groups.length)}
        description={seriesDescription(groups.map((g) => representativeSeriesStatus(g.demos.map((d) => d.status))))}
      />

      <div className="flex flex-col gap-3">
        {groups.map((group, i) =>
          group.demos.length === 1 ? (
            <SeriesDemoCard
              key={group.demos[0].jobId}
              demo={group.demos[0]}
              index={i}
              seriesId={id}
              reel={reelByJob.get(group.demos[0].jobId)}
            />
          ) : (
            <SeriesMultiPartCard key={group.key} group={group} index={i} seriesId={id} reelByJob={reelByJob} />
          ),
        )}
      </div>
    </div>
  );
}

/**
 * One map's row: title + score/status, plus a CTA into its picker when ready.
 * When the map already has a reel, the pill describes the reel (en cola →
 * grabando → renderizando → listo) instead of the raw job status, so queueing
 * plays on every map of the series reads as one visible capture queue.
 */
function SeriesDemoCard({
  demo,
  index,
  seriesId,
  reel,
}: {
  demo: SeriesDemo;
  index: number;
  seriesId: string;
  reel?: Video;
}) {
  const tone = reel ? seriesReelTone(reel.status) : seriesStatusTone(demo.status);
  const label = reel ? seriesReelLabel(reel.status) : seriesStatusLabel(demo.status);
  const forgeable = seriesStatusIsForgeable(demo.status);
  const failed = demo.status === 'failed';

  return (
    <div className="studio-panel studio-panel-raised flex flex-col gap-4 rounded-xl p-5 sm:flex-row sm:items-center sm:justify-between sm:gap-6">
      <div className="flex min-w-0 items-center gap-4">
        <span className="grid size-10 shrink-0 place-items-center rounded-lg border border-primary/25 bg-primary/10 font-[family-name:var(--font-mono)] text-sm text-primary">
          {index + 1}
        </span>
        <div className="flex min-w-0 flex-col gap-1">
          <h2 className="truncate font-[family-name:var(--font-display)] text-lg font-bold uppercase tracking-tight text-foreground">
            {demoTitle(demo, index)}
          </h2>
          <div className="flex flex-wrap items-center gap-x-3 gap-y-1 font-[family-name:var(--font-mono)] text-[0.7rem] uppercase tracking-wider text-muted-foreground">
            {demo.match ? (
              <span className="tabular-nums">
                {demo.match.scoreT}-{demo.match.scoreCt} · {demo.match.rounds} rondas
              </span>
            ) : null}
            {failed && demo.failureReason ? (
              <span className="normal-case tracking-normal text-destructive">{demo.failureReason}</span>
            ) : null}
          </div>
        </div>
      </div>

      <div className="flex shrink-0 items-center gap-3">
        <span
          className={cn(
            'inline-flex items-center rounded-full border px-2.5 py-0.5 font-[family-name:var(--font-mono)] text-[0.65rem] font-semibold uppercase tracking-wider',
            TONE_CLASSES[tone],
          )}
        >
          {label}
        </span>
        {forgeable ? (
          <Button
            asChild
            size="sm"
            variant={reel ? 'secondary' : 'default'}
            className="font-[family-name:var(--font-display)] tracking-[0.05em]"
          >
            <Link href={`/matches/${demo.jobId}?series=${seriesId}`}>
              {reel ? 'OTRO REEL' : 'ELEGIR JUGADAS'}
              <ChevronRight className="size-4" />
            </Link>
          </Button>
        ) : null}
      </div>
    </div>
  );
}

/**
 * One logical map split across several .dem parts: a single card titled by the
 * map (from the first part that carries a match), with one compact row per part.
 * The parts share the map header but each keeps its own status/reel pill and its
 * own picker link, so a half that failed to parse or already has a reel reads
 * independently while the map still counts as one entry in the series.
 */
function SeriesMultiPartCard({
  group,
  index,
  seriesId,
  reelByJob,
}: {
  group: SeriesGroup<SeriesDemo>;
  index: number;
  seriesId: string;
  reelByJob: ReadonlyMap<string, Video>;
}) {
  // Title from the first part that has a roster match; fall back to the first
  // part's own title so a still-scanning map still names itself.
  const headDemo = group.demos.find((d) => d.match) ?? group.demos[0];

  return (
    <div className="studio-panel studio-panel-raised flex flex-col gap-4 rounded-xl p-5">
      <div className="flex min-w-0 items-center gap-4">
        <span className="grid size-10 shrink-0 place-items-center rounded-lg border border-primary/25 bg-primary/10 font-[family-name:var(--font-mono)] text-sm text-primary">
          {index + 1}
        </span>
        <h2 className="truncate font-[family-name:var(--font-display)] text-lg font-bold uppercase tracking-tight text-foreground">
          {demoTitle(headDemo, index)}
        </h2>
      </div>

      <div className="flex flex-col divide-y divide-border/60 overflow-hidden rounded-lg border border-border/60 bg-muted/20">
        {group.demos.map((demo, partIndex) => (
          <SeriesPartRow
            key={demo.jobId}
            demo={demo}
            partIndex={partIndex}
            seriesId={seriesId}
            reel={reelByJob.get(demo.jobId)}
          />
        ))}
      </div>
    </div>
  );
}

/**
 * One part of a multi-part map: "PARTE N" plus that part's score/status and its
 * own picker CTA. Mirrors {@link SeriesDemoCard}'s pill/CTA logic so a part reads
 * exactly like a single-map card, just scoped to one half of the split demo.
 */
function SeriesPartRow({
  demo,
  partIndex,
  seriesId,
  reel,
}: {
  demo: SeriesDemo;
  partIndex: number;
  seriesId: string;
  reel?: Video;
}) {
  const tone = reel ? seriesReelTone(reel.status) : seriesStatusTone(demo.status);
  const label = reel ? seriesReelLabel(reel.status) : seriesStatusLabel(demo.status);
  const forgeable = seriesStatusIsForgeable(demo.status);
  const failed = demo.status === 'failed';

  return (
    <div className="flex flex-col gap-3 px-4 py-3 sm:flex-row sm:items-center sm:justify-between sm:gap-6">
      <div className="flex min-w-0 flex-col gap-1">
        <div className="flex flex-wrap items-center gap-x-3 gap-y-1 font-[family-name:var(--font-mono)] text-[0.7rem] uppercase tracking-wider text-muted-foreground">
          <span className="font-semibold text-foreground/70">PARTE {partIndex + 1}</span>
          {demo.match ? (
            <span className="tabular-nums">
              {demo.match.scoreT}-{demo.match.scoreCt} · {demo.match.rounds} rondas
            </span>
          ) : null}
        </div>
        {failed && demo.failureReason ? (
          <span className="font-[family-name:var(--font-mono)] text-[0.7rem] normal-case tracking-normal text-destructive">
            {demo.failureReason}
          </span>
        ) : null}
      </div>

      <div className="flex shrink-0 items-center gap-3">
        <span
          className={cn(
            'inline-flex items-center rounded-full border px-2.5 py-0.5 font-[family-name:var(--font-mono)] text-[0.65rem] font-semibold uppercase tracking-wider',
            TONE_CLASSES[tone],
          )}
        >
          {label}
        </span>
        {forgeable ? (
          <Button
            asChild
            size="sm"
            variant={reel ? 'secondary' : 'default'}
            className="font-[family-name:var(--font-display)] tracking-[0.05em]"
          >
            <Link href={`/matches/${demo.jobId}?series=${seriesId}`}>
              {reel ? 'OTRO REEL' : 'ELEGIR JUGADAS'}
              <ChevronRight className="size-4" />
            </Link>
          </Button>
        ) : null}
      </div>
    </div>
  );
}

/** Skeleton while the first series poll is in flight. */
function LoadingState(): ReactNode {
  return (
    <div className="flex flex-col gap-8 sm:gap-10">
      <div className="flex flex-col gap-3">
        <Skeleton className="h-4 w-24" />
        <Skeleton className="h-10 w-72" />
        <Skeleton className="h-5 w-96 max-w-full" />
      </div>
      <div className="flex flex-col gap-3">
        {[0, 1, 2].map((i) => (
          <Skeleton key={i} className="h-[92px] w-full rounded-xl" />
        ))}
      </div>
    </div>
  );
}
