import Link from 'next/link';
import { ArrowRight, Film } from 'lucide-react';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { ScoreBar, StatMono } from '@/components/brand';
import { formatKd, timeAgo } from '@/lib/format';
import type { Match } from '@/lib/api/types';
import { isWin } from './match-score';

export type MatchRowProps = {
  match: Match;
};

/**
 * One scoreboard row: a win/loss accent bar, the map and score in mono, a
 * highlights badge, the K/D/A/MVP/ratio stat strip, and a lime "Find
 * highlights" CTA. Hovering elevates the row.
 */
export function MatchRow({ match }: MatchRowProps) {
  const win = isWin(match.score);
  const { stats } = match;

  return (
    <Card className="flex flex-row items-stretch gap-0 overflow-hidden py-0 transition-all hover:-translate-y-0.5 hover:border-border/80 hover:shadow-md">
      <ScoreBar win={win} className="rounded-none" />

      <div className="flex min-w-0 flex-1 flex-col gap-4 p-4 sm:flex-row sm:items-center sm:justify-between sm:gap-6">
        <div className="flex min-w-0 flex-1 flex-col gap-3">
          <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
            <span className="font-[family-name:var(--font-display)] text-base font-semibold tracking-tight text-foreground">
              {match.map}
            </span>
            <Badge
              variant="outline"
              className="font-[family-name:var(--font-display)] text-[0.65rem] uppercase tracking-[0.12em] text-muted-foreground"
            >
              Map
            </Badge>
            <span
              className={
                'font-[family-name:var(--font-mono)] text-sm font-semibold tabular-nums ' +
                (win ? 'text-primary' : 'text-muted-foreground')
              }
            >
              {match.score}
            </span>
            {match.decentPlays > 0 ? (
              <Badge variant="secondary" className="gap-1">
                <Film size={12} aria-hidden />
                <span className="font-[family-name:var(--font-mono)] tabular-nums">
                  {match.decentPlays}
                </span>
                highlights
              </Badge>
            ) : null}
            <span className="font-[family-name:var(--font-mono)] text-xs tabular-nums text-muted-foreground">
              {timeAgo(match.playedAt)}
            </span>
          </div>

          <div className="flex flex-wrap items-center gap-x-6 gap-y-3">
            <StatMono label="K" value={stats.kills} />
            <StatMono label="D" value={stats.deaths} />
            <StatMono label="A" value={stats.assists} />
            <StatMono label="MVP" value={stats.mvps} />
            <StatMono label="K/D" value={formatKd(stats.kd)} accent />
          </div>
        </div>

        <div className="shrink-0">
          <Button asChild className="w-full sm:w-auto">
            <Link href={`/matches/${match.id}`}>
              Find highlights
              <ArrowRight size={16} aria-hidden />
            </Link>
          </Button>
        </div>
      </div>
    </Card>
  );
}
