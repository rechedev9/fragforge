'use client';

import { useState, type ReactNode } from 'react';
import type { DemoPlayer, RosterMatch } from '@/lib/api/types';
import { cn } from '@/lib/utils';
import { ratingBarClass, ratingBarPct, ratingClass } from '@/lib/format';
import { Badge } from '@/components/ui/badge';

export type PlayerPickerProps = {
  /** Roster from the scan, already sorted by kills desc. */
  players: DemoPlayer[];
  /** Fires when the user confirms a target by clicking a row. */
  onPick: (steamId: string) => void;
  /** Match-level context (map, score, rounds) shown above the tables, when the scan has it. */
  match?: RosterMatch;
};

/** Tooltip copy for the abbreviated stat column headers. */
const STAT_TOOLTIPS: Record<string, string> = {
  rating: 'Rating HLTV 1.0',
  adr: 'Daño medio por ronda',
  kast: '% de rondas con kill/asistencia/sobrevivir/trade',
  hs: '% de kills por headshot',
};

/** "de_dust2" -> "Dust2", "cs_office" -> "Office"; passes through anything unprefixed. */
function prettyMapName(map: string): string {
  const stripped = map.replace(/^(de|cs)_/, '');
  return stripped.charAt(0).toUpperCase() + stripped.slice(1);
}

/** First one or two glyphs of a name, uppercased, for the row's monogram avatar. */
function initials(name: string): string {
  return Array.from(name.trim()).slice(0, 2).join('').toUpperCase();
}

/**
 * Clip-worthiness score for the "Recommended" pick: multi-kill rounds are the
 * strongest signal a player's POV makes a good reel, weighted by kill count.
 */
function clipScore(p: DemoPlayer): number {
  return 3 * (p.rounds5k ?? 0) + 2 * (p.rounds4k ?? 0) + 1 * (p.rounds3k ?? 0);
}

/** Highest clip score in the roster, tiebroken by rating; undefined for an empty roster. */
function pickRecommended(players: DemoPlayer[]): DemoPlayer | undefined {
  return players.reduce<DemoPlayer | undefined>((best, p) => {
    if (!best) return p;
    const bestScore = clipScore(best);
    const score = clipScore(p);
    return score > bestScore || (score === bestScore && p.rating > best.rating) ? p : best;
  }, undefined);
}

type HighlightChip = { key: string; label: string; className: string };

/** Nonzero multi-kill chips in ACE -> 4K -> 3K order; ACE gets the strongest (cyan) treatment. */
function highlightChips(p: DemoPlayer): HighlightChip[] {
  const chips: HighlightChip[] = [];
  if (p.rounds5k) chips.push({ key: 'ace', label: `ACE ×${p.rounds5k}`, className: 'border-primary/40 bg-primary/15 text-primary' });
  if (p.rounds4k) chips.push({ key: '4k', label: `4K ×${p.rounds4k}`, className: 'border-amber-400/30 bg-amber-400/10 text-amber-400' });
  if (p.rounds3k) chips.push({ key: '3k', label: `3K ×${p.rounds3k}`, className: 'border-border bg-muted/60 text-muted-foreground' });
  return chips;
}

/** A scoreboard column: label, the value to render, and an optional colour tone. */
type Column = {
  key: string;
  label: string;
  value: (p: DemoPlayer) => string;
  tone?: (p: DemoPlayer) => string;
  /** Secondary columns hide below the sm breakpoint so the player name never collapses. */
  secondary?: boolean;
  /** Overrides the default text cell, e.g. the rating column's value-plus-bar. */
  render?: (p: DemoPlayer) => ReactNode;
};

/** Rating cell: the number plus a bar showing it against a 2.0 (elite-pace) ceiling. */
function RatingCell({ rating }: { rating: number }) {
  return (
    <span className="flex flex-col items-end gap-1">
      <span className={ratingClass(rating)}>{rating.toFixed(2)}</span>
      <span className="h-[3px] w-10 bg-muted">
        <span
          className={cn('block h-full', ratingBarClass(rating))}
          style={{ width: `${ratingBarPct(rating)}%` }}
        />
      </span>
    </span>
  );
}

function signed(n: number): string {
  return n > 0 ? `+${n}` : `${n}`;
}

const TEAM_META = {
  T: { label: 'Terroristas', text: 'text-amber-400', chip: 'border-amber-400/30 bg-amber-400/10 text-amber-400' },
  CT: { label: 'Antiterroristas', text: 'text-primary', chip: 'border-primary/30 bg-primary/10 text-primary' },
  '': { label: 'Otros', text: 'text-muted-foreground', chip: 'border-border bg-muted text-muted-foreground' },
} as const;

/** Compact match summary (map, final score, rounds) shown above the roster tables. */
function MatchHeader({ match }: { match: RosterMatch }) {
  const tWon = match.scoreT > match.scoreCt;
  const ctWon = match.scoreCt > match.scoreT;
  return (
    <div className="flex items-center justify-between gap-3 border border-primary/15 bg-muted/20 px-3.5 py-2.5">
      <span className="truncate font-[family-name:var(--font-display)] text-sm font-bold uppercase tracking-wide text-foreground">
        {prettyMapName(match.map)}
      </span>
      <span className="flex items-center gap-1.5 font-[family-name:var(--font-mono)] text-sm tabular-nums">
        <span className={cn(tWon ? 'font-bold text-amber-400' : 'text-muted-foreground')}>{match.scoreT}</span>
        <span className="text-muted-foreground/50">-</span>
        <span className={cn(ctWon ? 'font-bold text-primary' : 'text-muted-foreground')}>{match.scoreCt}</span>
      </span>
      <span className="shrink-0 font-[family-name:var(--font-mono)] text-[0.7rem] uppercase tracking-wider text-muted-foreground">
        {match.rounds} rondas
      </span>
    </div>
  );
}

/**
 * PlayerPicker — pick whose POV to clip after a roster scan, shown as a CS-style
 * scoreboard split by team (Terroristas / Antiterroristas), with a match header
 * (map, score, rounds) above it when the scan reports one. Each team is its own
 * table with column headers; rows carry HLTV rating, K/D/A, +/-, ADR, KAST, HS%
 * (and MVP when the demo reports it), plus a Highlights line of multi-kill chips
 * under the player name. The roster's clip-worthiest player (by multi-kill rounds,
 * the strongest signal for a good reel) is auto-highlighted and tagged
 * "Recomendado", but the user must click a row to confirm the target, which is
 * the whole point of this screen.
 */
export function PlayerPicker({ players, onPick, match }: PlayerPickerProps) {
  const recommended = pickRecommended(players);
  const [highlighted, setHighlighted] = useState<string | null>(recommended?.steamId ?? players[0]?.steamId ?? null);

  const showMvp = players.some((p) => p.mvps > 0);
  const columns: Column[] = [
    { key: 'rating', label: 'RAT', value: (p) => p.rating.toFixed(2), render: (p) => <RatingCell rating={p.rating} /> },
    { key: 'k', label: 'K', value: (p) => `${p.kills}` },
    { key: 'd', label: 'D', value: (p) => `${p.deaths}` },
    { key: 'a', label: 'A', value: (p) => `${p.assists}` },
    { key: 'pm', label: '+/-', secondary: true, value: (p) => signed(p.kills - p.deaths), tone: (p) => (p.kills - p.deaths >= 0 ? 'text-foreground' : 'text-muted-foreground') },
    { key: 'adr', label: 'ADR', secondary: true, value: (p) => `${Math.round(p.adr)}` },
    { key: 'kast', label: 'KAST', secondary: true, value: (p) => `${Math.round(p.kast)}%` },
    { key: 'hs', label: 'HS', secondary: true, value: (p) => `${Math.round(p.hsPct)}%` },
    ...(showMvp ? [{ key: 'mvp', label: 'MVP', secondary: true, value: (p: DemoPlayer) => `${p.mvps}` }] : []),
  ];

  // A fixed flexible player column plus one compact, tabular column per stat.
  // Narrow windows (a snapped or resized desktop window) drop the secondary
  // columns instead of letting the grid crush the player name to zero width,
  // so the template switches with the same sm breakpoint that hides the cells.
  const coreCount = columns.filter((c) => !c.secondary).length;
  const gridStyle = {
    '--pp-cols': `minmax(0,1fr) repeat(${coreCount}, minmax(2.5rem,2.75rem))`,
    '--pp-cols-sm': `minmax(0,1fr) repeat(${columns.length}, minmax(2.5rem,2.75rem))`,
  } as React.CSSProperties;
  const gridClass = '[grid-template-columns:var(--pp-cols)] sm:[grid-template-columns:var(--pp-cols-sm)]';
  const cellClass = (c: Column) => (c.secondary ? 'hidden sm:block' : undefined);

  const sides: Array<DemoPlayer['team']> = ['T', 'CT', ''];
  const groups = sides
    .map((side) => players.filter((p) => p.team === side))
    .map((roster, i) => ({ side: sides[i], roster }))
    .filter((g) => g.roster.length > 0);

  return (
    <div className="flex flex-col gap-5">
      {match ? <MatchHeader match={match} /> : null}
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
                media {avg.toFixed(2)}
              </span>
            </div>

            <div className="overflow-hidden border border-primary/15">
              <div
                className={cn(
                  'grid items-center gap-x-1 border-b border-border/70 bg-muted/30 px-3 py-2 font-[family-name:var(--font-mono)] text-[0.65rem] uppercase tracking-wider text-muted-foreground',
                  gridClass,
                )}
                style={gridStyle}
              >
                <span>Jugador</span>
                {columns.map((c) => (
                  <span key={c.key} className={cn('text-right', cellClass(c))} title={STAT_TOOLTIPS[c.key]}>
                    {c.label}
                  </span>
                ))}
              </div>

              {roster.map((p) => {
                const active = p.steamId === highlighted;
                const isRecommended = p.steamId === recommended?.steamId;
                const chips = highlightChips(p);
                let chipRow: ReactNode;
                if (chips.length > 0) {
                  chipRow = chips.map((c) => (
                    <span
                      key={c.key}
                      className={cn(
                        'inline-flex items-center rounded-full border px-1.5 py-0.5 font-[family-name:var(--font-mono)] text-[0.6rem] font-semibold tabular-nums',
                        c.className,
                      )}
                    >
                      {c.label}
                    </span>
                  ));
                } else if (!isRecommended) {
                  chipRow = <span className="text-[0.65rem] text-muted-foreground/60">-</span>;
                } else {
                  chipRow = null;
                }
                return (
                  <button
                    key={p.steamId}
                    type="button"
                    onMouseEnter={() => setHighlighted(p.steamId)}
                    onFocus={() => setHighlighted(p.steamId)}
                    onClick={() => onPick(p.steamId)}
                    style={gridStyle}
                    className={cn(
                      'grid w-full cursor-pointer items-center gap-x-1 border-b border-border/40 px-3 py-2.5 text-left transition-colors last:border-b-0',
                      'focus:outline-none focus-visible:bg-primary/10',
                      gridClass,
                      // The recommended row keeps a permanent left accent + tint so it reads at a
                      // glance even when the user's mouse is elsewhere; the ring below layers on
                      // top for whichever row is the current pick target (hover/keyboard focus).
                      isRecommended && 'bg-primary/10 shadow-[inset_3px_0_0_0_var(--primary)]',
                      active
                        ? 'bg-primary/10 ring-1 ring-inset ring-primary/60'
                        : !isRecommended && 'hover:bg-muted/40 hover:ring-1 hover:ring-inset hover:ring-border',
                    )}
                  >
                    <span className="flex min-w-0 flex-col gap-0.5">
                      <span className="flex min-w-0 items-center gap-2.5">
                        <span
                          data-testid="player-avatar"
                          className={cn(
                            'inline-flex size-7 shrink-0 items-center justify-center rounded-md border font-[family-name:var(--font-display)] text-[0.65rem] font-bold leading-none',
                            active ? 'border-primary/50 bg-primary/15 text-primary' : meta.chip,
                          )}
                        >
                          {initials(p.name)}
                        </span>
                        {/* min-w-0 lets this shrink inside the flex row without evicting the name;
                            the name never has to share a row with the Recommended badge, which
                            keeps this row narrow-window-safe like the rest of the scoreboard. */}
                        <span className="min-w-0 flex-1 truncate text-sm font-medium text-foreground">{p.name}</span>
                      </span>
                      {/* Second line, indented under the name: the Recommended tag and the
                          Highlights chips. Always rendered (never just for narrow windows) so
                          it never competes with the player name for horizontal space. */}
                      <span className="flex flex-wrap items-center gap-1 pl-[2.375rem]">
                        {isRecommended ? (
                          <Badge className="shrink-0 px-1.5 py-0 text-[0.6rem] leading-4">Recomendado</Badge>
                        ) : null}
                        {chipRow}
                      </span>
                    </span>
                    {columns.map((c) => (
                      <span
                        key={c.key}
                        className={cn(
                          'text-right font-[family-name:var(--font-mono)] text-sm tabular-nums',
                          cellClass(c),
                          c.tone?.(p) ?? 'text-foreground',
                        )}
                      >
                        {c.render ? c.render(p) : c.value(p)}
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
