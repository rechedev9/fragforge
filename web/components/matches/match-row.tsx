import Link from 'next/link';
import { ScoreBar } from '@/components/brand/score-bar';
import { StatMono } from '@/components/brand/stat-mono';
import { DeleteMatchButton } from '@/components/matches/delete-match-button';
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
  /** Deletes this match (and its artifacts); when set, the row shows a trash button. */
  onDelete?: (jobId: string) => Promise<void>;
  /** Called after a successful delete so the page can re-fetch its lists. */
  onDeleted?: () => void;
};

/**
 * One scoreboard row, NEON HUD style: a 3px win/loss accent bar, the map in
 * display caps over a dim mono meta line, the round score in mono (own score
 * cyan on a win), the K/D/A/MVP stat strip, and the per-row CTA.
 */
export function MatchRow({ match, featured = false, onDelete, onDeleted }: MatchRowProps) {
  const win = isWin(match.score);
  const { stats } = match;
  const { ours, theirs } = parseScore(match.score);
  // Lead the meta line with the clipped player when known ("<PLAYER> · HACE X"),
  // dropping it cleanly (no stray separator) when it is absent.
  const meta = [
    match.player,
    timeAgo(match.playedAt),
    match.decentPlays > 0 ? `${match.decentPlays} ${match.decentPlays === 1 ? 'jugada' : 'jugadas'}` : null,
  ]
    .filter(Boolean)
    .join(' · ');

  return (
    <article
      className={cn(
        'flex items-stretch gap-4 px-4 py-4 transition-colors sm:gap-5 sm:px-5',
        featured
          ? 'studio-panel studio-panel-raised'
          : 'studio-panel studio-panel-interactive bg-card/80',
      )}
    >
      <ScoreBar win={win} className="w-1 shrink-0" />

      <div className="grid min-w-0 flex-1 grid-cols-[minmax(0,1fr)_auto] items-center gap-x-4 gap-y-4 xl:grid-cols-[minmax(160px,1.1fr)_90px_minmax(320px,1.7fr)_auto] xl:gap-x-8">
        <div className="min-w-0">
          <h2 className="truncate font-[family-name:var(--font-display)] text-xl font-bold uppercase leading-tight text-foreground">
            {match.map}
          </h2>
          <p className="mt-1 truncate font-[family-name:var(--font-mono)] text-xs uppercase tracking-[0.1em] text-muted-foreground">
            {meta}
          </p>
        </div>

        <div className="shrink-0 text-right font-[family-name:var(--font-mono)] text-lg tabular-nums xl:text-left">
          {ours !== null && theirs !== null ? (
            <>
              <span className={win ? 'text-primary' : 'text-muted-foreground'}>{ours}</span>
              <span className="text-muted-foreground"> : </span>
              <span className="text-muted-foreground">{theirs}</span>
            </>
          ) : (
            <span className="text-muted-foreground">{match.score}</span>
          )}
        </div>

        <div className="col-span-2 grid grid-cols-5 gap-3 border-y border-border/55 py-3 sm:gap-5 xl:col-span-1 xl:border-0 xl:py-0">
          <StatMono label="K" value={stats.kills} />
          <StatMono label="D" value={stats.deaths} />
          <StatMono label="A" value={stats.assists} />
          <StatMono label="MVP" value={stats.mvps} />
          <StatMono label="K/D" value={formatKd(stats.kd)} accent />
        </div>

        <div className="col-span-2 flex min-w-0 items-start justify-end gap-2 xl:col-span-1">
          {featured ? (
            <Link
              href={`/matches/${match.id}`}
              className="neon-glow rounded-md inline-flex h-11 flex-1 items-center justify-center bg-primary px-5 font-[family-name:var(--font-display)] text-sm font-bold tracking-[0.06em] text-primary-foreground transition-colors hover:bg-primary/90 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background sm:flex-initial"
            >
              FORJAR REEL
            </Link>
          ) : (
            <Link
              href={`/matches/${match.id}`}
              className="inline-flex h-11 flex-1 items-center justify-center border border-border-strong bg-background/45 px-4 font-[family-name:var(--font-mono)] text-xs tracking-[0.14em] text-muted-foreground transition-colors hover:border-primary/55 hover:bg-primary/10 hover:text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background sm:flex-initial"
            >
              VER PARTIDA ▸
            </Link>
          )}
          {onDelete ? (
            <DeleteMatchButton
              label={match.map}
              onConfirm={() => onDelete(match.id)}
              onDeleted={() => onDeleted?.()}
            />
          ) : null}
        </div>
      </div>
    </article>
  );
}
