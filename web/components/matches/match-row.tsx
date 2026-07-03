import Link from 'next/link';
import { ScoreBar } from '@/components/brand/score-bar';
import { StatMono } from '@/components/brand/stat-mono';
import { formatKd, timeAgo } from '@/lib/format';
import { cn } from '@/lib/utils';
import type { Match } from '@/lib/api/types';
import { isWin, parseScore } from './match-score';

export type MatchRowProps = {
  match: Match;
  /**
   * The spotlight row (the first in the list): corner brackets, cyan border,
   * tinted background, and the notched FORJAR REEL CTA. Other rows get the
   * quiet "VER ▸" mono link instead.
   */
  featured?: boolean;
};

/**
 * One scoreboard row, NEON HUD style: a 3px win/loss accent bar, the map in
 * display caps over a dim mono meta line, the round score in mono (own score
 * cyan on a win), the K/D/A/MVP stat strip, and the per-row CTA.
 */
export function MatchRow({ match, featured = false }: MatchRowProps) {
  const win = isWin(match.score);
  const { stats } = match;
  const { ours, theirs } = parseScore(match.score);
  const meta = [
    timeAgo(match.playedAt),
    match.decentPlays > 0 ? `${match.decentPlays} ${match.decentPlays === 1 ? 'jugada' : 'jugadas'}` : null,
  ]
    .filter(Boolean)
    .join(' · ');

  return (
    <div
      className={cn(
        'flex flex-wrap items-center gap-x-6 gap-y-3 px-5 py-4 transition-colors sm:flex-nowrap',
        featured
          ? 'neon-brackets relative border border-primary/45 bg-primary/[0.06]'
          : 'border border-primary/10 bg-card/75 hover:border-primary/25',
      )}
    >
      <ScoreBar win={win} className="h-11 w-[3px] self-center" />

      <div className="w-[150px] min-w-0 shrink-0">
        <div className="truncate font-[family-name:var(--font-display)] text-lg font-bold uppercase leading-tight text-foreground">
          {match.map}
        </div>
        <div className="mt-0.5 truncate font-[family-name:var(--font-mono)] text-[10.5px] uppercase tracking-[0.12em] text-muted-foreground/70">
          {meta}
        </div>
      </div>

      <div className="w-[90px] shrink-0 font-[family-name:var(--font-mono)] text-[17px] tabular-nums">
        {ours !== null && theirs !== null ? (
          <>
            <span className={win ? 'text-primary' : 'text-muted-foreground'}>{ours}</span>
            <span className="text-muted-foreground/70"> : </span>
            <span className="text-muted-foreground">{theirs}</span>
          </>
        ) : (
          <span className="text-muted-foreground">{match.score}</span>
        )}
      </div>

      <div className="flex flex-wrap items-center gap-x-7 gap-y-3">
        <StatMono label="K" value={stats.kills} />
        <StatMono label="D" value={stats.deaths} />
        <StatMono label="A" value={stats.assists} />
        <StatMono label="MVP" value={stats.mvps} />
        <StatMono label="K/D" value={formatKd(stats.kd)} accent />
      </div>

      <div className="ml-auto shrink-0">
        {featured ? (
          <Link
            href={`/matches/${match.id}`}
            className="neon-notch neon-glow inline-flex items-center bg-primary px-5 py-2.5 font-[family-name:var(--font-display)] text-[13px] font-bold tracking-[0.06em] text-primary-foreground transition-colors hover:bg-primary/90"
          >
            FORJAR REEL
          </Link>
        ) : (
          <Link
            href={`/matches/${match.id}`}
            className="inline-flex items-center px-2 py-2 font-[family-name:var(--font-mono)] text-[11px] tracking-[0.16em] text-muted-foreground/70 transition-colors hover:text-primary"
          >
            VER ▸
          </Link>
        )}
      </div>
    </div>
  );
}
