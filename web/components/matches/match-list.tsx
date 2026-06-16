import { SearchX } from 'lucide-react';
import type { Match } from '@/lib/api/types';
import { MatchRow } from './match-row';

export type MatchListProps = {
  matches: Match[];
};

/** The scoreboard: one MatchRow per match, or an empty state when filtered out. */
export function MatchList({ matches }: MatchListProps) {
  if (matches.length === 0) {
    return (
      <div className="flex flex-col items-center gap-3 rounded-xl border border-dashed border-border bg-card/40 px-6 py-16 text-center">
        <SearchX size={28} aria-hidden className="text-muted-foreground" />
        <p className="text-sm font-medium text-foreground">No matches here</p>
        <p className="text-xs text-muted-foreground">
          Try a different map or filter.
        </p>
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-3">
      {matches.map((match) => (
        <MatchRow key={match.id} match={match} />
      ))}
    </div>
  );
}
