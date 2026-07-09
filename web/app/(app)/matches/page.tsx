'use client';

import { useEffect, useMemo, useState, type ReactNode } from 'react';
import Link from 'next/link';
import { Clapperboard, Swords, UploadCloud } from 'lucide-react';
import { api } from '@/lib/api';
import type { Match } from '@/lib/api/types';
import { MatchFilters, type MatchFilter } from '@/components/matches/match-filters';
import { MatchList } from '@/components/matches/match-list';
import { MatchListSkeleton } from '@/components/matches/match-list-skeleton';
import { isWin } from '@/components/matches/match-score';
import { StudioEmptyState } from '@/components/studio/empty-state';
import { StudioPageHeader } from '@/components/studio/page-header';
import { Button } from '@/components/ui/button';

/**
 * Landing state when there are no matches at all (not merely filtered out):
 * the dashboard is the first screen, so it must route into both content flows
 * instead of showing the filter-oriented empty state.
 */
function NoMatchesYet() {
  return (
    <StudioEmptyState
      icon={Swords}
      title="Aún no hay partidas"
      description="Analiza una demo de CS2 o corta clips de un stream para empezar."
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

  let content: ReactNode;
  if (matches !== null && matches.length === 0) {
    content = <NoMatchesYet />;
  } else if (matches === null) {
    content = <MatchListSkeleton />;
  } else {
    content = <MatchList matches={visible} />;
  }

  return (
    <div className="flex flex-col gap-8 sm:gap-10">
      <StudioPageHeader
        number={1}
        label="PARTIDAS"
        title="TUS PARTIDAS"
        description="Tus últimas partidas de CS2. Elige una y forja sus highlights en un reel."
        actions={
          matches !== null && matches.length === 0 ? null : (
            <MatchFilters
              filter={filter}
              onFilterChange={setFilter}
              query={query}
              onQueryChange={setQuery}
            />
          )
        }
      />

      {content}
    </div>
  );
}
