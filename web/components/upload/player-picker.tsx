'use client';

import { useState } from 'react';
import { Crosshair } from 'lucide-react';
import type { DemoPlayer } from '@/lib/api/types';
import { StatMono } from '@/components/brand/stat-mono';
import { cn } from '@/lib/utils';

export type PlayerPickerProps = {
  /** Roster from the scan, already sorted by kills desc. */
  players: DemoPlayer[];
  /** Fires when the user confirms a target by clicking a row. */
  onPick: (steamId: string) => void;
};

/**
 * PlayerPicker — pick whose POV to clip after a roster scan. The top fragger is
 * auto-highlighted (the list arrives sorted by kills) but the user must click to
 * confirm. K/D/A render in mono tabular-nums; the highlighted/hovered row gets
 * the lime selection ring per the design discipline.
 */
export function PlayerPicker({ players, onPick }: PlayerPickerProps) {
  const [highlighted, setHighlighted] = useState<string | null>(players[0]?.steamId ?? null);

  return (
    <div className="flex flex-col gap-2">
      {players.map((player) => {
        const active = player.steamId === highlighted;
        return (
          <button
            key={player.steamId}
            type="button"
            onMouseEnter={() => setHighlighted(player.steamId)}
            onFocus={() => setHighlighted(player.steamId)}
            onClick={() => onPick(player.steamId)}
            className={cn(
              'flex items-center gap-4 rounded-xl border bg-card px-4 py-3 text-left transition-all',
              active ? 'border-primary ring-2 ring-primary' : 'border-border hover:border-muted-foreground/40',
            )}
          >
            <span
              className={cn(
                'inline-flex size-9 shrink-0 items-center justify-center rounded-lg border transition-colors',
                active ? 'border-primary/40 bg-primary/10 text-primary' : 'border-border bg-muted text-muted-foreground',
              )}
            >
              <Crosshair className="size-4" />
            </span>

            <span className="flex min-w-0 flex-col gap-1">
              <span className="truncate text-sm font-medium text-foreground">{player.name}</span>
              {player.team ? (
                <span className="font-[family-name:var(--font-mono)] text-[0.7rem] uppercase tracking-wider text-muted-foreground">
                  {player.team}
                </span>
              ) : null}
            </span>

            <span className="ml-auto flex items-center gap-x-5">
              <StatMono label="K" value={player.kills} />
              <StatMono label="D" value={player.deaths} />
              <StatMono label="A" value={player.assists} />
            </span>
          </button>
        );
      })}
    </div>
  );
}
