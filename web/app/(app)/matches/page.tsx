'use client';

import { useEffect, useMemo, useState } from 'react';
import { api } from '@/lib/api';
import type { Match } from '@/lib/api/types';
import { MatchFilters, type MatchFilter } from '@/components/matches/match-filters';
import { MatchList } from '@/components/matches/match-list';
import { MatchListSkeleton } from '@/components/matches/match-list-skeleton';
import { isWin } from '@/components/matches/match-score';

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
      rows = [...rows].sort((a, b) => b.stats.kd - a.stats.kd);
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

      <MatchFilters
        filter={filter}
        onFilterChange={setFilter}
        query={query}
        onQueryChange={setQuery}
      />

      {matches === null ? <MatchListSkeleton /> : <MatchList matches={visible} />}
    </div>
  );
}
