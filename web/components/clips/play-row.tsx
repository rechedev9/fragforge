'use client';

import { Check, Crosshair } from 'lucide-react';
import type { Play } from '@/lib/api/types';
import { ReelCover } from '@/components/brand/reel-cover';
import { cn } from '@/lib/utils';

export type PlayRowProps = {
  play: Play;
  selected: boolean;
  onToggle: () => void;
};

/**
 * Kill badge styling by frag count, per the NEON HUD mockup: ACE (5K) is the
 * solid cyan chip, 2K-4K the neutral chip, and 1K stays quiet. CLUTCH's magenta
 * chip exists in the mockup only; the plan has no clutch data to drive it.
 */
function killBadge(kills: number): { label: string; className: string } {
  if (kills >= 5) return { label: 'ACE', className: 'bg-primary text-primary-foreground' };
  if (kills >= 2) return { label: `${kills}K`, className: 'bg-foreground/15 text-foreground' };
  return { label: `${kills}K`, className: 'bg-foreground/10 text-muted-foreground' };
}

/**
 * PlayRow — a single selectable row in the highlights PlayList (the vertical
 * layout the e2e suite pins down; the mockup's tile treatments — cyan 1.5px
 * border, square corner check, italic display round label, mono badge — are
 * applied to each row). The whole row toggles selection on click; a square
 * check affordance mirrors the row's `aria-pressed` state. Multiple rows can
 * be selected at once to build a concatenated reel.
 */
export function PlayRow({ play, selected, onToggle }: PlayRowProps) {
  const badge = killBadge(play.kills);
  return (
    <button
      type="button"
      onClick={onToggle}
      aria-pressed={selected}
      className={cn(
        'group flex w-full items-center gap-3 border-b border-primary/10 bg-card px-3 py-2.5 text-left transition-colors last:border-b-0',
        'focus:outline-none focus-visible:bg-primary/10',
        selected
          ? 'bg-primary/[0.08] ring-[1.5px] ring-inset ring-primary'
          : 'hover:bg-muted/40 hover:ring-1 hover:ring-inset hover:ring-primary/25',
      )}
    >
      {/* Square check affordance — reflects the row's selected state. */}
      <span
        aria-hidden
        className={cn(
          'flex size-5 shrink-0 items-center justify-center border transition-colors',
          selected
            ? 'border-primary bg-primary text-primary-foreground'
            : 'border-foreground/20 bg-muted/40 text-transparent',
        )}
      >
        <Check className="size-3.5" strokeWidth={3} />
      </span>

      {/* Compact thumbnail with the mockup's big italic round label. */}
      <span className="relative aspect-video w-20 shrink-0 overflow-hidden bg-muted sm:w-24">
        <ReelCover seed={play.id} plain />
        <span className="pointer-events-none absolute inset-0 bg-gradient-to-t from-black/60 via-black/10 to-transparent" />
        <span
          aria-hidden
          className={cn(
            'pointer-events-none absolute inset-0 flex items-center justify-center font-[family-name:var(--font-display)] text-lg font-bold italic',
            selected ? 'text-foreground' : 'text-muted-foreground',
          )}
        >
          R{play.round}
        </span>
      </span>

      {/* Round / weapon meta. min-w-0 lets it shrink instead of forcing horizontal scroll. */}
      <span className="flex min-w-0 flex-1 flex-col gap-0.5">
        <span
          className={cn(
            'truncate font-[family-name:var(--font-display)] text-sm font-bold italic tracking-wide',
            selected ? 'text-foreground' : 'text-muted-foreground',
          )}
        >
          RONDA {play.round}
        </span>
        <span className="truncate font-[family-name:var(--font-mono)] text-xs uppercase tracking-wide text-muted-foreground/70">
          {play.weapon ?? `${play.kills} ${play.kills === 1 ? 'kill' : 'kills'}`}
        </span>
      </span>

      {/* Kill badge — square mono chip per the mockup. The Crosshair icon also
          anchors the e2e pick-a-clip selector (button:has(.lucide-crosshair)). */}
      <span
        className={cn(
          'ml-auto inline-flex shrink-0 items-center gap-1 px-2 py-0.5 font-[family-name:var(--font-mono)] text-[10px] tracking-[0.16em] tabular-nums',
          badge.className,
        )}
      >
        <Crosshair className="size-3" />
        {badge.label}
      </span>
    </button>
  );
}
