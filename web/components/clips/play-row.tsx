'use client';

import { Check, Crosshair, Play as PlayIcon } from 'lucide-react';
import type { Play } from '@/lib/api/types';
import { ReelCover } from '@/components/brand';
import { cn } from '@/lib/utils';

export type PlayRowProps = {
  play: Play;
  selected: boolean;
  onToggle: () => void;
};

/** Kill badge styling by frag count: 1K stays quiet, 2K+ goes lime, 5K reads ACE. */
function killBadge(kills: number): { label: string; className: string } {
  if (kills >= 5) return { label: 'ACE', className: 'bg-primary text-primary-foreground ring-2 ring-primary/40' };
  if (kills >= 3) return { label: `${kills}K`, className: 'bg-primary text-primary-foreground ring-2 ring-primary/30' };
  if (kills >= 2) return { label: `${kills}K`, className: 'bg-primary text-primary-foreground' };
  return { label: `${kills}K`, className: 'bg-black/60 text-foreground' };
}

/**
 * PlayRow — a single selectable row in the highlights PlayList (the vertical
 * successor to the horizontal-filmstrip PlayTile). The whole row toggles
 * selection on click, mirroring PlayerPicker's row pattern; a check affordance
 * on the left mirrors the row's `aria-pressed` state, a compact thumbnail
 * carries the same hover play affordance as the old card, and the kill badge
 * stays on the right. Multiple rows can be selected at once to build a
 * concatenated reel.
 */
export function PlayRow({ play, selected, onToggle }: PlayRowProps) {
  const badge = killBadge(play.kills);
  return (
    <button
      type="button"
      onClick={onToggle}
      aria-pressed={selected}
      className={cn(
        'group flex w-full items-center gap-3 border-b border-border/60 bg-card px-3 py-2.5 text-left transition-colors last:border-b-0',
        'focus:outline-none focus-visible:bg-primary/10',
        selected ? 'bg-primary/10 ring-1 ring-inset ring-primary' : 'hover:bg-muted/40 hover:ring-1 hover:ring-inset hover:ring-border',
      )}
    >
      {/* Check affordance — reflects the row's selected state. */}
      <span
        aria-hidden
        className={cn(
          'flex size-5 shrink-0 items-center justify-center rounded-md border transition-colors',
          selected ? 'border-primary bg-primary text-primary-foreground' : 'border-border bg-muted/40 text-transparent',
        )}
      >
        <Check className="size-3.5" strokeWidth={3} />
      </span>

      {/* Compact thumbnail with the same hover play affordance as the old card. */}
      <span className="relative aspect-video w-20 shrink-0 overflow-hidden rounded-lg bg-muted sm:w-24">
        <ReelCover seed={play.id} plain className="transition-transform duration-300 group-hover:scale-105" />
        <span className="pointer-events-none absolute inset-0 bg-gradient-to-t from-black/60 via-black/10 to-transparent" />
        <span className="pointer-events-none absolute inset-0 flex items-center justify-center">
          <span className="flex size-7 items-center justify-center rounded-full bg-primary/90 text-primary-foreground opacity-0 shadow-lg backdrop-blur-sm transition-all duration-200 group-hover:scale-100 group-hover:opacity-100 scale-75">
            <PlayIcon className="size-3.5 translate-x-px fill-current" />
          </span>
        </span>
      </span>

      {/* Round / weapon meta. min-w-0 lets it shrink instead of forcing horizontal scroll. */}
      <span className="flex min-w-0 flex-1 flex-col gap-0.5">
        <span className="truncate text-sm font-medium text-foreground">Round {play.round}</span>
        <span className="truncate font-[family-name:var(--font-mono)] text-xs uppercase tracking-wide text-muted-foreground">
          {play.weapon ?? `${play.kills} ${play.kills === 1 ? 'kill' : 'kills'}`}
        </span>
      </span>

      {/* Kill badge */}
      <span
        className={cn(
          'ml-auto inline-flex shrink-0 items-center gap-1 rounded-full px-2 py-0.5 font-[family-name:var(--font-mono)] text-[0.7rem] font-bold tabular-nums',
          badge.className,
        )}
      >
        <Crosshair className="size-3" />
        {badge.label}
      </span>
    </button>
  );
}
