import { SearchX } from 'lucide-react';
import type { Match } from '@/lib/api/types';
import { MatchRow } from './match-row';

export type MatchListProps = {
  matches: Match[];
};

/** The scoreboard: one MatchRow per match (the first one featured), or an empty state when filtered out. */
export function MatchList({ matches }: MatchListProps) {
  if (matches.length === 0) {
    return (
      <div className="flex flex-col items-center gap-3 border border-dashed border-border bg-card/40 px-6 py-16 text-center">
        <SearchX size={28} aria-hidden className="text-muted-foreground" />
        <p className="text-sm font-medium text-foreground">Sin resultados</p>
        <p className="text-xs text-muted-foreground">
          Prueba otro mapa u otro filtro.
        </p>
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-2.5">
      {matches.map((match, index) => (
        <MatchRow key={match.id} match={match} featured={index === 0} />
      ))}
    </div>
  );
}
