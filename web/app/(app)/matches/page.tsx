'use client';

import { useEffect, useMemo, useState } from 'react';
import Link from 'next/link';
import { Clapperboard, UploadCloud } from 'lucide-react';
import { api } from '@/lib/api';
import type { Match } from '@/lib/api/types';
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
    <div className="flex flex-col items-center gap-6 rounded-xl border border-dashed border-border bg-card/40 px-6 py-16 text-center">
      <div className="flex flex-col gap-1.5">
        <p className="text-sm font-medium text-foreground">No matches yet</p>
        <p className="text-xs text-muted-foreground">
          Analyze a CS2 demo or cut clips from a stream to get started.
        </p>
      </div>
      <div className="flex flex-wrap items-center justify-center gap-3">
        <Link
          href="/upload"
          className="inline-flex h-10 items-center gap-2 rounded-md bg-primary px-5 text-sm font-medium text-primary-foreground transition-colors hover:bg-primary/90"
        >
          <UploadCloud className="size-4" />
          Analyze a demo
        </Link>
        <Link
          href="/streams"
          className="inline-flex h-10 items-center gap-2 rounded-md border border-border bg-transparent px-5 text-sm font-medium text-foreground transition-colors hover:bg-accent"
        >
          <Clapperboard className="size-4" />
          Stream Clips
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
      // "Best frags" = most kills first; K/D only breaks ties.
      rows = [...rows].sort((a, b) => b.stats.kills - a.stats.kills || b.stats.kd - a.stats.kd);
    }

    return rows;
  }, [matches, filter, query]);

  return (
    <div className="flex flex-col gap-8">
      <header className="flex flex-col gap-1.5">
        <h1 className="font-[family-name:var(--font-display)] text-3xl font-bold tracking-tight text-foreground sm:text-4xl">
          Matches
        </h1>
        <p className="max-w-xl text-sm text-muted-foreground">
          Your recent CS2 matches. Pick one and forge the highlights into a reel.
        </p>
      </header>

      {matches !== null && matches.length === 0 ? (
        <NoMatchesYet />
      ) : (
        <>
          <MatchFilters
            filter={filter}
            onFilterChange={setFilter}
            query={query}
            onQueryChange={setQuery}
          />
          {matches === null ? <MatchListSkeleton /> : <MatchList matches={visible} />}
        </>
      )}
    </div>
  );
}
