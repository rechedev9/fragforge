'use client';

import { Crosshair } from 'lucide-react';
import type { Play } from '@/lib/api/types';
import { ReelCover } from '@/components/brand';
import { cn } from '@/lib/utils';

export type PlayTileProps = {
  play: Play;
  selected: boolean;
  onSelect: () => void;
};

/**
 * PlayTile — a single selectable tile in the highlights Filmstrip. Shows the
 * play thumbnail, the round/tick in mono, a kills pip, and the weapon. The
 * selected tile gets the lime signal ring; everything else stays neutral.
 */
export function PlayTile({ play, selected, onSelect }: PlayTileProps) {
  return (
    <button
      type="button"
      onClick={onSelect}
      aria-pressed={selected}
      className={cn(
        'group relative w-[220px] shrink-0 overflow-hidden rounded-xl border bg-card text-left transition-all',
        selected
          ? 'border-primary ring-2 ring-primary'
          : 'border-border hover:border-muted-foreground/40',
      )}
    >
      <div className="relative aspect-video w-full overflow-hidden bg-muted">
        <ReelCover
          seed={play.id}
          className="transition-transform duration-200 group-hover:scale-[1.03]"
        />
        <span className="pointer-events-none absolute inset-0 bg-gradient-to-t from-black/70 via-black/10 to-transparent" />

        <span className="absolute left-2 top-2 inline-flex items-center gap-1 rounded-full bg-black/60 px-2 py-0.5 font-[family-name:var(--font-mono)] text-[0.7rem] font-semibold tabular-nums text-foreground backdrop-blur-sm">
          R{String(play.round).padStart(2, '0')}
        </span>

        <span className="absolute right-2 top-2 inline-flex items-center gap-1 rounded-full bg-primary px-2 py-0.5 font-[family-name:var(--font-mono)] text-[0.7rem] font-semibold tabular-nums text-primary-foreground">
          <Crosshair className="size-3" />
          {play.kills}K
        </span>
      </div>

      <div className="flex flex-col gap-0.5 px-3 py-2.5">
        <span className="truncate text-sm font-medium text-foreground">{play.label}</span>
        {play.weapon ? (
          <span className="font-[family-name:var(--font-mono)] text-xs uppercase tracking-wide text-muted-foreground">
            {play.weapon}
          </span>
        ) : null}
      </div>
    </button>
  );
}
