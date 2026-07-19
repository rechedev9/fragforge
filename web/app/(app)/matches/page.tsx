'use client';

import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react';
import Link from 'next/link';
import { ChevronRight, Clapperboard, Layers, Swords, UploadCloud } from 'lucide-react';
import { api } from '@/lib/api';
import { SERVICE_UNAVAILABLE_CODE } from '@/lib/api/types';
import type { Match } from '@/lib/api/types';
import type { SeriesSummary } from '@/lib/api/jobs-index';
import { MatchFilters, type MatchFilter } from '@/components/matches/match-filters';
import { MatchList } from '@/components/matches/match-list';
import { MatchListSkeleton } from '@/components/matches/match-list-skeleton';
import { DeleteMatchButton } from '@/components/matches/delete-match-button';
import { isWin } from '@/components/matches/match-score';
import { StudioEmptyState } from '@/components/studio/empty-state';
import { StudioPageHeader } from '@/components/studio/page-header';
import { Button } from '@/components/ui/button';
import { seriesTitle } from '@/lib/series-status';
import { navSection } from '@/lib/nav';
import { timeAgo } from '@/lib/format';

const NAV = navSection('/matches');

/** True when an API error means the local analysis service is unreachable. */
function isServiceUnavailable(err: unknown): boolean {
  return (err as { code?: string } | null)?.code === SERVICE_UNAVAILABLE_CODE;
}

/**
 * Landing state when there are no matches and no series at all (not merely
 * filtered out): the dashboard is the first screen, so it must route into both
 * content flows instead of showing the filter-oriented empty state. When the
 * local analysis service is offline it says so, since that (not "nothing
 * uploaded") is why the list came back empty.
 */
function NoMatchesYet({ offline }: { offline: boolean }) {
  return (
    <StudioEmptyState
      icon={Swords}
      title="Aún no hay partidas"
      description={
        offline
          ? 'No se pudo contactar con el servicio de análisis local. Arráncalo y recarga, o analiza una demo para empezar.'
          : 'Analiza una demo de CS2 o corta clips de un stream para empezar.'
      }
      compact
      actions={
        <>
          <Button asChild className="font-[family-name:var(--font-display)] tracking-[0.06em]">
            <Link href="/upload">
              <UploadCloud aria-hidden />
              ANALIZAR UNA DEMO
            </Link>
          </Button>
          <Button
            asChild
            variant="outline"
            className="border-stream/45 font-[family-name:var(--font-display)] tracking-[0.06em] hover:border-stream/70 hover:bg-stream/10"
          >
            <Link href="/streams">
              <Clapperboard aria-hidden />
              CLIPS DE STREAM
            </Link>
          </Button>
        </>
      }
    />
  );
}

/**
 * The SERIES section above the matches list: one compact row per uploaded
 * bo3/bo5 series, linking into its /series/{id} view. The maps of a series still
 * list individually below (that is the Partidas model); this row is the way to
 * reach the series as a whole after a restart.
 */
function SeriesSection({
  series,
  onDelete,
  onDeleted,
}: {
  series: SeriesSummary[];
  onDelete: (seriesId: string) => Promise<void>;
  onDeleted: () => void;
}) {
  return (
    <section className="flex flex-col gap-3" aria-label="Series">
      <h2 className="font-[family-name:var(--font-mono)] text-xs uppercase tracking-[0.2em] text-muted-foreground">
        SERIES
      </h2>
      {series.map((s) => (
        // The trash button can't live inside the row's <Link>, so the row is a
        // flex container with the link and the delete control as siblings.
        <div key={s.seriesId} className="flex items-center gap-3">
          <Link
            href={`/series/${s.seriesId}`}
            className="studio-panel studio-panel-interactive flex flex-1 items-center justify-between gap-4 rounded-xl px-4 py-4 transition-colors sm:px-5"
          >
            <div className="flex min-w-0 items-center gap-4">
              <span className="grid size-10 shrink-0 place-items-center rounded-lg border border-primary/25 bg-primary/10 text-primary">
                <Layers className="size-5" aria-hidden />
              </span>
              <div className="flex min-w-0 flex-col gap-1">
                <span className="truncate font-[family-name:var(--font-display)] text-lg font-bold uppercase leading-tight tracking-tight text-foreground">
                  {seriesTitle(s.mapCount)}
                </span>
                <span className="font-[family-name:var(--font-mono)] text-xs uppercase tracking-[0.1em] text-muted-foreground">
                  {timeAgo(s.createdAt)}
                </span>
              </div>
            </div>
            <ChevronRight className="size-4 shrink-0 text-muted-foreground" aria-hidden />
          </Link>
          <DeleteMatchButton
            label={seriesTitle(s.mapCount)}
            onConfirm={() => onDelete(s.seriesId)}
            onDeleted={onDeleted}
          />
        </div>
      ))}
    </section>
  );
}

export default function MatchesPage() {
  const [matches, setMatches] = useState<Match[] | null>(null);
  const [series, setSeries] = useState<SeriesSummary[]>([]);
  const [offline, setOffline] = useState(false);
  const [filter, setFilter] = useState<MatchFilter>('all');
  const [query, setQuery] = useState('');

  const load = useCallback(async () => {
    try {
      const [nextMatches, nextSeries] = await Promise.all([api.listMatches(), api.listSeriesSummaries()]);
      setMatches(nextMatches);
      setSeries(nextSeries);
      setOffline(false);
    } catch (err) {
      // Offline (or any load failure) must not crash the page: fall to the
      // empty state, flagging offline so its copy explains the empty list.
      setMatches([]);
      setSeries([]);
      setOffline(isServiceUnavailable(err));
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  // A match delete throws (409 busy / 503 offline) so the row surfaces it; a
  // success re-fetches both lists so the deleted entry drops (and a deleted
  // series' member matches vanish, since the server is the source of truth).
  const deleteMatch = useCallback((jobId: string) => api.deleteMatch(jobId), []);
  const deleteSeries = useCallback((seriesId: string) => api.deleteSeries(seriesId), []);
  const refresh = useCallback(() => {
    void load();
  }, [load]);

  const visible = useMemo(() => {
    if (!matches) return [];

    const q = query.trim().toLowerCase();
    let rows = q ? matches.filter((m) => m.map.toLowerCase().includes(q)) : matches;

    if (filter === 'wins') {
      rows = rows.filter((m) => isWin(m.score));
    }

    if (filter === 'frags') {
      // "Mejores frags" = most kills first; K/D only breaks ties.
      rows = [...rows].sort((a, b) => b.stats.kills - a.stats.kills || b.stats.kd - a.stats.kd);
    }

    return rows;
  }, [matches, filter, query]);

  const hasContent = (matches !== null && matches.length > 0) || series.length > 0;

  let content: ReactNode;
  if (matches === null) {
    content = <MatchListSkeleton />;
  } else if (!hasContent) {
    content = <NoMatchesYet offline={offline} />;
  } else {
    content = (
      <div className="flex flex-col gap-8 sm:gap-10">
        {series.length > 0 ? (
          <SeriesSection series={series} onDelete={deleteSeries} onDeleted={refresh} />
        ) : null}
        {matches.length > 0 ? (
          <MatchList matches={visible} onDelete={deleteMatch} onDeleted={refresh} />
        ) : null}
      </div>
    );
  }

  // The filters act on the matches list, so show them only when matches exist.
  const showFilters = matches !== null && matches.length > 0;

  return (
    <div className="flex flex-col gap-8 sm:gap-10">
      <StudioPageHeader
        number={Number(NAV.number)}
        label={NAV.label.toUpperCase()}
        title="TUS PARTIDAS"
        description="Tus últimas partidas de CS2. Elige una y forja sus highlights en un reel."
        actions={
          showFilters ? (
            <MatchFilters
              filter={filter}
              onFilterChange={setFilter}
              query={query}
              onQueryChange={setQuery}
            />
          ) : null
        }
      />

      {content}
    </div>
  );
}
