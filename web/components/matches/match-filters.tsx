'use client';

import { Search } from 'lucide-react';
import { Input } from '@/components/ui/input';
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group';

/** The three scoreboard views: everything, only wins, or highest-frag first. */
export type MatchFilter = 'all' | 'wins' | 'frags';

export type MatchFiltersProps = {
  filter: MatchFilter;
  onFilterChange: (filter: MatchFilter) => void;
  query: string;
  onQueryChange: (query: string) => void;
};

/** Square mono NEON HUD filter chip; the active one is solid cyan on dark text. */
const CHIP_CLASS =
  'h-auto border border-primary/25 bg-transparent px-3.5 py-1.5 font-[family-name:var(--font-mono)] text-[11px] uppercase tracking-[0.14em] text-muted-foreground rounded-none first:rounded-none last:rounded-none hover:bg-primary/10 hover:text-foreground data-[state=on]:border-primary data-[state=on]:bg-primary data-[state=on]:text-primary-foreground';

/**
 * Filter controls for the matches scoreboard: square mono chips (Todas /
 * Victorias / Mejores frags) plus a map search box, per the NEON HUD mockup.
 * Cyan stays a signal — the active chip is the only filled element here.
 */
export function MatchFilters({
  filter,
  onFilterChange,
  query,
  onQueryChange,
}: MatchFiltersProps) {
  return (
    <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-end">
      <ToggleGroup
        type="single"
        value={filter}
        onValueChange={(value) => {
          if (value) onFilterChange(value as MatchFilter);
        }}
        className="w-fit gap-2"
        aria-label="Filtrar partidas"
      >
        <ToggleGroupItem value="all" aria-label="Todas las partidas" className={CHIP_CLASS}>
          TODAS
        </ToggleGroupItem>
        <ToggleGroupItem value="wins" aria-label="Solo victorias" className={CHIP_CLASS}>
          VICTORIAS
        </ToggleGroupItem>
        <ToggleGroupItem value="frags" aria-label="Mejores frags primero" className={CHIP_CLASS}>
          MEJORES FRAGS
        </ToggleGroupItem>
      </ToggleGroup>

      <div className="relative w-full sm:max-w-[220px]">
        <Search
          size={15}
          aria-hidden
          className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-muted-foreground"
        />
        <Input
          type="search"
          value={query}
          onChange={(event) => onQueryChange(event.target.value)}
          placeholder="Buscar mapa…"
          aria-label="Buscar por mapa"
          className="border-primary/25 pl-9 font-[family-name:var(--font-mono)] text-sm"
        />
      </div>
    </div>
  );
}
