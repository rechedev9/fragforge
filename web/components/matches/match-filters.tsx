'use client';

import { Search } from 'lucide-react';
import { STUDIO_FILTER_CHIP_CLASS } from '@/components/studio/filter-chip';
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
    <div className="flex w-full flex-col gap-3 lg:w-auto lg:items-end">
      <div className="w-full overflow-x-auto pb-1 lg:w-auto lg:pb-0">
        <ToggleGroup
          type="single"
          value={filter}
          onValueChange={(value) => {
            if (value) onFilterChange(value as MatchFilter);
          }}
          className="w-max gap-2"
          aria-label="Filtrar partidas"
        >
          <ToggleGroupItem value="all" aria-label="Todas las partidas" className={STUDIO_FILTER_CHIP_CLASS}>
            TODAS
          </ToggleGroupItem>
          <ToggleGroupItem value="wins" aria-label="Solo victorias" className={STUDIO_FILTER_CHIP_CLASS}>
            VICTORIAS
          </ToggleGroupItem>
          <ToggleGroupItem value="frags" aria-label="Mejores frags primero" className={STUDIO_FILTER_CHIP_CLASS}>
            MEJORES FRAGS
          </ToggleGroupItem>
        </ToggleGroup>
      </div>

      <div className="relative w-full lg:max-w-[260px]">
        <Search
          size={16}
          aria-hidden
          className="pointer-events-none absolute left-3.5 top-1/2 -translate-y-1/2 text-muted-foreground"
        />
        <Input
          type="search"
          value={query}
          onChange={(event) => onQueryChange(event.target.value)}
          placeholder="Buscar mapa…"
          aria-label="Buscar por mapa"
          className="h-11 border-primary/25 bg-background/55 pl-10 font-[family-name:var(--font-mono)] text-sm"
        />
      </div>
    </div>
  );
}
