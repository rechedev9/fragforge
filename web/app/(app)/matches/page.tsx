'use client';

import { useEffect, useMemo, useState } from 'react';
import Link from 'next/link';
import { Clapperboard, UploadCloud } from 'lucide-react';
import { api } from '@/lib/api';
import type { Match } from '@/lib/api/types';
import { SectionEyebrow } from '@/components/brand/section-eyebrow';
import { MatchFilters, type MatchFilter } from '@/components/matches/match-filters';
import { MatchList } from '@/components/matches/match-list';
import { MatchListSkeleton } from '@/components/matches/match-list-skeleton';
import { isWin } from '@/components/matches/match-score';

/**
 * Landing state when there are no matches at all (not merely filtered out):
 * the dashboard is the first screen, so it must route into both content flows
 * instead of showing the filter-oriented empty state.
 */
function NoMatchesYet() {
  return (
    <div className="flex flex-col items-center gap-6 border border-dashed border-border bg-card/40 px-6 py-16 text-center">
      <div className="flex flex-col gap-1.5">
        <p className="text-sm font-medium text-foreground">Aún no hay partidas</p>
        <p className="text-xs text-muted-foreground">
          Analiza una demo de CS2 o corta clips de un stream para empezar.
        </p>
      </div>
      <div className="flex flex-wrap items-center justify-center gap-3">
        <Link
          href="/upload"
          className="neon-notch inline-flex h-10 items-center gap-2 bg-primary px-5 font-[family-name:var(--font-display)] text-sm font-bold tracking-[0.06em] text-primary-foreground transition-colors hover:bg-primary/90"
        >
          <UploadCloud className="size-4" />
          ANALIZAR UNA DEMO
        </Link>
        <Link
          href="/streams"
          className="inline-flex h-10 items-center gap-2 border border-destructive/40 bg-transparent px-5 font-[family-name:var(--font-display)] text-sm font-semibold tracking-[0.06em] text-foreground transition-colors hover:bg-destructive/10"
        >
          <Clapperboard className="size-4" />
          CLIPS DE STREAM
        </Link>
      </div>
    </div>
  );
}

export default function MatchesPage() {
  const [matches, setMatches] = useState<Match[] | null>(null);
  const [filter, setFilter] = useState<MatchFilter>('all');
  const [query, setQuery] = useState('');

  useEffect(() => {
    let active = true;
    (async () => {
      const next = await api.listMatches();
      if (active) setMatches(next);
    })();
    return () => {
      active = false;
    };
  }, []);

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

  return (
    <div className="flex flex-col gap-7">
      <header className="flex flex-col gap-2.5">
        <SectionEyebrow number={1} label="PARTIDAS" />
        <div className="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between lg:gap-6">
          <h1 className="font-[family-name:var(--font-display)] text-3xl font-bold leading-none tracking-tight text-foreground sm:text-[34px]">
            TUS PARTIDAS
          </h1>
          {matches !== null && matches.length === 0 ? null : (
            <MatchFilters
              filter={filter}
              onFilterChange={setFilter}
              query={query}
              onQueryChange={setQuery}
            />
          )}
        </div>
        <p className="max-w-xl text-sm text-muted-foreground">
          Tus últimas partidas de CS2. Elige una y forja sus highlights en un reel.
        </p>
      </header>

      {matches !== null && matches.length === 0 ? (
        <NoMatchesYet />
      ) : matches === null ? (
        <MatchListSkeleton />
      ) : (
        <MatchList matches={visible} />
      )}
    </div>
  );
}
