'use client';

import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group';
import { STUDIO_FILTER_CHIP_CLASS } from '@/components/studio/filter-chip';

/** The library's aspect-ratio views: everything, or one render format. */
export type VideoFormatFilter = 'all' | 'short-9x16' | 'landscape-16x9';

export type VideoFiltersProps = {
  filter: VideoFormatFilter;
  onFilterChange: (filter: VideoFormatFilter) => void;
};

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
      className="w-fit max-w-full flex-wrap justify-start gap-2"
      aria-label="Filtrar reels por formato"
    >
      <ToggleGroupItem value="all" aria-label="Todos los formatos" className={STUDIO_FILTER_CHIP_CLASS}>
        TODOS
      </ToggleGroupItem>
      <ToggleGroupItem value="short-9x16" aria-label="Formato vertical 9:16" className={STUDIO_FILTER_CHIP_CLASS}>
        9:16
      </ToggleGroupItem>
      <ToggleGroupItem value="landscape-16x9" aria-label="Formato horizontal 16:9" className={STUDIO_FILTER_CHIP_CLASS}>
        16:9
      </ToggleGroupItem>
    </ToggleGroup>
  );
}
