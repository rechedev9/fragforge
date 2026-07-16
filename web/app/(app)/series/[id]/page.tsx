'use client';

import { use, useEffect, useRef, useState, type ReactNode } from 'react';
import Link from 'next/link';
import { Layers, ChevronRight } from 'lucide-react';
import { api } from '@/lib/api';
import { SERVICE_UNAVAILABLE_CODE } from '@/lib/api/types';
import type { SeriesDemo } from '@/lib/api/types';
import {
  seriesStatusLabel,
  seriesStatusTone,
  seriesStatusIsPending,
  seriesStatusIsForgeable,
  summarizeSeriesStatuses,
  type SeriesStatusTone,
} from '@/lib/series-status';
import { startPollLoop } from '@/lib/poll-loop';
import { StudioEmptyState } from '@/components/studio/empty-state';
import { StudioPageHeader } from '@/components/studio/page-header';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { cn } from '@/lib/utils';

/** Series ids are client-minted UUIDs; anything else is a bad/guessed URL. */
const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

/** Fast while any map is still working, relaxed once the series has settled. */
const FAST_MS = 2500;
const IDLE_MS = 8000;

/** True when an API error means the local analysis service is unreachable. */
function isServiceUnavailable(err: unknown): boolean {
  return (err as { code?: string } | null)?.code === SERVICE_UNAVAILABLE_CODE;
}

/** "de_dust2" -> "Dust2", "cs_office" -> "Office"; passes through anything unprefixed. */
function prettyMapName(map: string): string {
  const stripped = map.replace(/^(de|cs)_/, '');
  return stripped.charAt(0).toUpperCase() + stripped.slice(1);
}

/** Each demo's headline: prettified map name, else file name, else its position. */
function demoTitle(demo: SeriesDemo, index: number): string {
  if (demo.match) return prettyMapName(demo.match.map);
  if (demo.fileName) return demo.fileName;
  return `Mapa ${index + 1}`;
}

/**
 * Header description built from the real status buckets, omitting empty ones:
 * "2 mapas listos para forjar · 1 analizando · 1 fallido · 1 sin jugador".
 * Only genuinely pending maps are ever described as being analyzed; settled
 * ones (failed, or scanned without the chosen player) get their own bucket.
 */
function seriesDescription(statuses: readonly string[]): string {
  const { ready, pending, failed, skipped } = summarizeSeriesStatuses(statuses);
  const parts: string[] = [];
  if (ready > 0) parts.push(ready === 1 ? '1 mapa listo para forjar' : `${ready} mapas listos para forjar`);
  if (pending > 0) parts.push(`${pending} analizando`);
  if (failed > 0) parts.push(failed === 1 ? '1 fallido' : `${failed} fallidos`);
  if (skipped > 0) parts.push(`${skipped} sin jugador`);
  if (parts.length === 0) return 'Sin demos en la serie.';
  return `${parts.join(' · ')}.`;
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
  const valid = UUID_RE.test(id);

  const [demos, setDemos] = useState<SeriesDemo[] | null>(null);
  const [loaded, setLoaded] = useState(false);
  const [loadError, setLoadError] = useState<'offline' | 'generic' | null>(null);
  // Read the latest demos inside the poll tick without re-subscribing the loop:
  // a transient poll failure must not wipe an already-loaded list.
  const demosRef = useRef<SeriesDemo[] | null>(null);

  useEffect(() => {
    if (!valid) return;
    let active = true;
    const stop = startPollLoop({
      tick: async () => {
        try {
          const list = await api.getSeries(id);
          if (!active) return 'idle';
          demosRef.current = list;
          setDemos(list);
          setLoadError(null);
          setLoaded(true);
          return list.some((d) => seriesStatusIsPending(d.status)) ? 'fast' : 'idle';
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

  return (
    <div className="flex flex-col gap-8 sm:gap-10">
      <StudioPageHeader
        number={2}
        label="SERIE"
        title={list.length === 1 ? 'SERIE DE 1 MAPA' : `SERIE DE ${list.length} MAPAS`}
        description={seriesDescription(list.map((d) => d.status))}
      />

      <div className="flex flex-col gap-3">
        {list.map((demo, i) => (
          <SeriesDemoCard key={demo.jobId} demo={demo} index={i} />
        ))}
      </div>
    </div>
  );
}

/** One map's row: title + score/status, plus a CTA into its picker when ready. */
function SeriesDemoCard({ demo, index }: { demo: SeriesDemo; index: number }) {
  const tone = seriesStatusTone(demo.status);
  const label = seriesStatusLabel(demo.status);
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
          <Button asChild size="sm" className="font-[family-name:var(--font-display)] tracking-[0.05em]">
            <Link href={'/matches/' + demo.jobId}>
              ELEGIR JUGADAS
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
