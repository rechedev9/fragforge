'use client';

import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group';

/** The library's aspect-ratio views: everything, or one render format. */
export type VideoFormatFilter = 'all' | 'short-9x16' | 'landscape-16x9';

export type VideoFiltersProps = {
  filter: VideoFormatFilter;
  onFilterChange: (filter: VideoFormatFilter) => void;
};

/** Square mono NEON HUD filter chip; the active one is solid cyan on dark text. */
const CHIP_CLASS =
  'h-auto border border-primary/25 bg-transparent px-3.5 py-1.5 font-[family-name:var(--font-mono)] text-[11px] uppercase tracking-[0.14em] text-muted-foreground rounded-none first:rounded-none last:rounded-none hover:bg-primary/10 hover:text-foreground data-[state=on]:border-primary data-[state=on]:bg-primary data-[state=on]:text-primary-foreground';

/**
 * Aspect-ratio filter chips for the library grid (Todos / 9:16 / 16:9), per
 * the NEON HUD mockup. Filters over `editConfig.format`, a field every reel
 * already carries — no new data, just a client-side view over it.
 */
export function VideoFilters({ filter, onFilterChange }: VideoFiltersProps) {
  return (
    <ToggleGroup
      type="single"
      value={filter}
      onValueChange={(value) => {
        if (value) onFilterChange(value as VideoFormatFilter);
      }}
      className="w-fit gap-2"
      aria-label="Filtrar reels por formato"
    >
      <ToggleGroupItem value="all" aria-label="Todos los formatos" className={CHIP_CLASS}>
        TODOS
      </ToggleGroupItem>
      <ToggleGroupItem value="short-9x16" aria-label="Formato vertical 9:16" className={CHIP_CLASS}>
        9:16
      </ToggleGroupItem>
      <ToggleGroupItem value="landscape-16x9" aria-label="Formato horizontal 16:9" className={CHIP_CLASS}>
        16:9
      </ToggleGroupItem>
    </ToggleGroup>
  );
}
