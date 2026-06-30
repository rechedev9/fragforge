'use client';

import { Crosshair, Play as PlayIcon, Check } from 'lucide-react';
import type { Play } from '@/lib/api/types';
import { ReelCover } from '@/components/brand';
import { cn } from '@/lib/utils';

export type PlayTileProps = {
  play: Play;
  selected: boolean;
  onSelect: () => void;
};

/** Kill badge styling by frag count: 1K stays quiet, 2K+ goes lime, 5K reads ACE. */
function killBadge(kills: number): { label: string; className: string } {
  if (kills >= 5) return { label: 'ACE', className: 'bg-primary text-primary-foreground ring-2 ring-primary/40' };
  if (kills >= 3) return { label: `${kills}K`, className: 'bg-primary text-primary-foreground ring-2 ring-primary/30' };
  if (kills >= 2) return { label: `${kills}K`, className: 'bg-primary text-primary-foreground' };
  return { label: `${kills}K`, className: 'bg-black/60 text-foreground' };
}

/**
 * PlayTile — a single selectable tile in the highlights Filmstrip. The thumbnail
 * carries the round and a frag-tiered kills badge (ACE for 5K); a play affordance
 * fades in on hover. The selected tile gets the lime ring plus a "Picked" footer
 * marker so the chosen clip is unmistakable.
 */
export function PlayTile({ play, selected, onSelect }: PlayTileProps) {
  const badge = killBadge(play.kills);
  return (
    <button
      type="button"
      onClick={onSelect}
      aria-pressed={selected}
      className={cn(
        'group relative w-[228px] shrink-0 overflow-hidden rounded-2xl border bg-card text-left transition-all',
        selected
          ? 'border-primary ring-2 ring-primary'
          : 'border-border hover:border-muted-foreground/40 hover:shadow-lg hover:shadow-black/20',
      )}
    >
      <div className="relative aspect-video w-full overflow-hidden bg-muted">
        <ReelCover seed={play.id} className="transition-transform duration-300 group-hover:scale-105" />
        <span className="pointer-events-none absolute inset-0 bg-gradient-to-t from-black/75 via-black/15 to-transparent" />

        {/* Play affordance — communicates that the tile is a clip. */}
        <span className="pointer-events-none absolute inset-0 flex items-center justify-center">
          <span className="flex size-11 items-center justify-center rounded-full bg-primary/90 text-primary-foreground opacity-0 shadow-lg backdrop-blur-sm transition-all duration-200 group-hover:scale-100 group-hover:opacity-100 scale-75">
            <PlayIcon className="size-5 translate-x-px fill-current" />
          </span>
        </span>

        <span className="absolute left-2 top-2 rounded-full bg-black/55 px-2 py-0.5 font-[family-name:var(--font-mono)] text-[0.7rem] font-semibold tabular-nums text-foreground backdrop-blur-sm">
          R{String(play.round).padStart(2, '0')}
        </span>

        <span
          className={cn(
            'absolute right-2 top-2 inline-flex items-center gap-1 rounded-full px-2 py-0.5 font-[family-name:var(--font-mono)] text-[0.7rem] font-bold tabular-nums',
            badge.className,
          )}
        >
          <Crosshair className="size-3" />
          {badge.label}
        </span>
      </div>

      <div className="flex flex-col gap-0.5 px-3 py-2.5">
        <div className="flex items-center justify-between gap-2">
          <span className="truncate text-sm font-medium text-foreground">Round {play.round}</span>
          {selected ? (
            <span className="inline-flex shrink-0 items-center gap-1 font-[family-name:var(--font-mono)] text-[0.65rem] font-semibold uppercase tracking-wider text-primary">
              <Check className="size-3.5" />
              Picked
            </span>
          ) : null}
        </div>
        <span className="truncate font-[family-name:var(--font-mono)] text-xs uppercase tracking-wide text-muted-foreground">
          {play.weapon ?? `${play.kills} ${play.kills === 1 ? 'kill' : 'kills'}`}
        </span>
      </div>
    </button>
  );
}
