import { SearchX } from 'lucide-react';
import type { Match } from '@/lib/api/types';
import { StudioEmptyState } from '@/components/studio/empty-state';
import { MatchRow } from './match-row';

export type MatchListProps = {
  matches: Match[];
};

/** The scoreboard: one MatchRow per match (the first one featured), or an empty state when filtered out. */
export function MatchList({ matches }: MatchListProps) {
  if (matches.length === 0) {
    return (
      <StudioEmptyState
        icon={SearchX}
        title="Sin resultados"
        description="Prueba otro mapa u otro filtro."
        compact
        className="max-w-2xl"
      />
    );
  }

  return (
    <div className="flex flex-col gap-3" aria-label="Partidas disponibles">
      {matches.map((match, index) => (
        <MatchRow key={match.id} match={match} featured={index === 0} />
      ))}
    </div>
  );
}
