'use client';

import { useState } from 'react';
import { Crosshair } from 'lucide-react';
import type { DemoPlayer } from '@/lib/api/types';
import { cn } from '@/lib/utils';
import { ratingClass } from '@/lib/format';

export type PlayerPickerProps = {
  /** Roster from the scan, already sorted by kills desc. */
  players: DemoPlayer[];
  /** Fires when the user confirms a target by clicking a row. */
  onPick: (steamId: string) => void;
};

/** A scoreboard column: label, the value to render, and an optional colour tone. */
type Column = {
  key: string;
  label: string;
  value: (p: DemoPlayer) => string;
  tone?: (p: DemoPlayer) => string;
};

function signed(n: number): string {
  return n > 0 ? `+${n}` : `${n}`;
}

const TEAM_META = {
  T: { label: 'Terrorists', text: 'text-amber-400', chip: 'border-amber-400/30 bg-amber-400/10 text-amber-400' },
  CT: { label: 'Counter-Terrorists', text: 'text-sky-400', chip: 'border-sky-400/30 bg-sky-400/10 text-sky-400' },
  '': { label: 'Other', text: 'text-muted-foreground', chip: 'border-border bg-muted text-muted-foreground' },
} as const;

/**
 * PlayerPicker — pick whose POV to clip after a roster scan, shown as a CS-style
 * scoreboard split by team (Terrorists / Counter-Terrorists). Each team is its
 * own table with column headers; rows carry HLTV rating, K/D/A, +/-, ADR, KAST,
 * HS% (and MVP when the demo reports it). The list arrives sorted by kills so the
 * top fragger is auto-highlighted, but the user must click a row to confirm the
 * target, which is the whole point of this screen.
 */
export function PlayerPicker({ players, onPick }: PlayerPickerProps) {
  const [highlighted, setHighlighted] = useState<string | null>(players[0]?.steamId ?? null);

  const showMvp = players.some((p) => p.mvps > 0);
  const columns: Column[] = [
    { key: 'rating', label: 'RAT', value: (p) => p.rating.toFixed(2), tone: (p) => ratingClass(p.rating) },
    { key: 'k', label: 'K', value: (p) => `${p.kills}` },
    { key: 'd', label: 'D', value: (p) => `${p.deaths}` },
    { key: 'a', label: 'A', value: (p) => `${p.assists}` },
    { key: 'pm', label: '+/-', value: (p) => signed(p.kills - p.deaths), tone: (p) => (p.kills - p.deaths >= 0 ? 'text-foreground' : 'text-muted-foreground') },
    { key: 'adr', label: 'ADR', value: (p) => `${Math.round(p.adr)}` },
    { key: 'kast', label: 'KAST', value: (p) => `${Math.round(p.kast)}%` },
    { key: 'hs', label: 'HS', value: (p) => `${Math.round(p.hsPct)}%` },
    ...(showMvp ? [{ key: 'mvp', label: 'MVP', value: (p: DemoPlayer) => `${p.mvps}` }] : []),
  ];

  // A fixed flexible player column plus one compact, tabular column per stat.
  const gridStyle = { gridTemplateColumns: `minmax(0,1fr) repeat(${columns.length}, minmax(2.5rem,2.75rem))` };

  const sides: Array<DemoPlayer['team']> = ['T', 'CT', ''];
  const groups = sides
    .map((side) => players.filter((p) => p.team === side))
    .map((roster, i) => ({ side: sides[i], roster }))
    .filter((g) => g.roster.length > 0);

  return (
    <div className="flex flex-col gap-5">
      {groups.map(({ side, roster }) => {
        const meta = TEAM_META[side];
        const avg = roster.reduce((s, p) => s + p.rating, 0) / roster.length;
        return (
          <section key={side || 'other'}>
            <div className="mb-2 flex items-center justify-between px-1">
              <span className={cn('font-[family-name:var(--font-display)] text-xs font-bold uppercase tracking-widest', meta.text)}>
                {meta.label}
              </span>
              <span className="font-[family-name:var(--font-mono)] text-[0.7rem] uppercase tracking-wider text-muted-foreground">
                avg {avg.toFixed(2)}
              </span>
            </div>

            <div className="overflow-hidden rounded-xl border border-border">
              <div
                className="grid items-center gap-x-1 border-b border-border/70 bg-muted/30 px-3 py-2 font-[family-name:var(--font-mono)] text-[0.65rem] uppercase tracking-wider text-muted-foreground"
                style={gridStyle}
              >
                <span>Player</span>
                {columns.map((c) => (
                  <span key={c.key} className="text-right">
                    {c.label}
                  </span>
                ))}
              </div>

              {roster.map((p) => {
                const active = p.steamId === highlighted;
                return (
                  <button
                    key={p.steamId}
                    type="button"
                    onMouseEnter={() => setHighlighted(p.steamId)}
                    onFocus={() => setHighlighted(p.steamId)}
                    onClick={() => onPick(p.steamId)}
                    style={gridStyle}
                    className={cn(
                      'grid w-full items-center gap-x-1 border-b border-border/40 px-3 py-2.5 text-left transition-colors last:border-b-0',
                      'focus:outline-none focus-visible:bg-primary/10',
                      active ? 'bg-primary/10 ring-1 ring-inset ring-primary/60' : 'hover:bg-muted/40',
                    )}
                  >
                    <span className="flex min-w-0 items-center gap-2.5">
                      <span
                        className={cn(
                          'inline-flex size-7 shrink-0 items-center justify-center rounded-md border',
                          active ? 'border-primary/50 bg-primary/15 text-primary' : meta.chip,
                        )}
                      >
                        <Crosshair className="size-3.5" />
                      </span>
                      <span className="truncate text-sm font-medium text-foreground">{p.name}</span>
                    </span>
                    {columns.map((c) => (
                      <span
                        key={c.key}
                        className={cn('text-right font-[family-name:var(--font-mono)] text-sm tabular-nums', c.tone?.(p) ?? 'text-foreground')}
                      >
                        {c.value(p)}
                      </span>
                    ))}
                  </button>
                );
              })}
            </div>
          </section>
        );
      })}
    </div>
  );
}
