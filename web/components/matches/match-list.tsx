import { SearchX } from 'lucide-react';
import type { Match } from '@/lib/api/types';
import { StudioEmptyState } from '@/components/studio/empty-state';
import { MatchRow } from './match-row';

export type MatchListProps = {
  matches: Match[];
  /** Deletes a match by job id; when set, each row shows a trash button. */
  onDelete?: (jobId: string) => Promise<void>;
  /** Called after a successful delete so the page can re-fetch its lists. */
  onDeleted?: () => void;
};

/** The scoreboard: one MatchRow per match (the first one featured), or an empty state when filtered out. */
export function MatchList({ matches, onDelete, onDeleted }: MatchListProps) {
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
    <section className="flex flex-col gap-3" aria-label="Partidas disponibles">
      {matches.map((match, index) => (
        <MatchRow key={match.id} match={match} featured={index === 0} onDelete={onDelete} onDeleted={onDeleted} />
      ))}
      <div className="flex items-center justify-center gap-4 py-3 text-muted-foreground/60" aria-hidden>
        <span className="h-px w-12 bg-border" />
        <span className="font-[family-name:var(--font-mono)] text-[10px] uppercase tracking-[0.18em]">Fin de la lista</span>
        <span className="h-px w-12 bg-border" />
      </div>
    </section>
  );
}
