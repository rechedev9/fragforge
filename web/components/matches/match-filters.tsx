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

/**
 * Filter controls for the matches scoreboard: a segmented ToggleGroup (All /
 * Wins / Best frags) and a map search box. Lime stays a signal — the active
 * toggle is the only tinted element here.
 */
export function MatchFilters({
  filter,
  onFilterChange,
  query,
  onQueryChange,
}: MatchFiltersProps) {
  return (
    <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
      <ToggleGroup
        type="single"
        value={filter}
        onValueChange={(value) => {
          if (value) onFilterChange(value as MatchFilter);
        }}
        variant="outline"
        className="w-fit"
        aria-label="Filter matches"
      >
        <ToggleGroupItem
          value="all"
          aria-label="All matches"
          className="data-[state=on]:bg-primary data-[state=on]:text-primary-foreground"
        >
          All
        </ToggleGroupItem>
        <ToggleGroupItem
          value="wins"
          aria-label="Wins only"
          className="data-[state=on]:bg-primary data-[state=on]:text-primary-foreground"
        >
          Wins
        </ToggleGroupItem>
        <ToggleGroupItem
          value="frags"
          aria-label="Best frags first"
          className="data-[state=on]:bg-primary data-[state=on]:text-primary-foreground"
        >
          Best frags
        </ToggleGroupItem>
      </ToggleGroup>

      <div className="relative w-full sm:max-w-xs">
        <Search
          size={15}
          aria-hidden
          className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-muted-foreground"
        />
        <Input
          type="search"
          value={query}
          onChange={(event) => onQueryChange(event.target.value)}
          placeholder="Search map…"
          aria-label="Search by map"
          className="pl-9"
        />
      </div>
    </div>
  );
}
