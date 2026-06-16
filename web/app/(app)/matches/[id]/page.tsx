'use client';

import { use, useEffect, useState } from 'react';
import { useRouter } from 'next/navigation';
import { ArrowLeft } from 'lucide-react';
import type { Match, Play } from '@/lib/api/types';
import { api } from '@/lib/api';
import { formatKd } from '@/lib/format';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { Filmstrip } from '@/components/brand/filmstrip';
import { ScoreBar } from '@/components/brand/score-bar';
import { StatMono } from '@/components/brand/stat-mono';
import { SectionEyebrow } from '@/components/brand/section-eyebrow';
import { PlayTile } from '@/components/clips/play-tile';
import { ModeCards, type RenderModeChoice } from '@/components/clips/mode-cards';
import { CreateReelBar } from '@/components/clips/create-reel-bar';
import { SongPickerDialog } from '@/components/clips/song-picker-dialog';

/** Parse "13-2" into [13, 2]; returns null if it isn't a clean rounds score. */
function parseScore(score: string): [number, number] | null {
  const m = /^(\d+)\s*-\s*(\d+)$/.exec(score.trim());
  if (!m) return null;
  return [Number(m[1]), Number(m[2])];
}

function isWin(score: string): boolean {
  const parsed = parseScore(score);
  return parsed ? parsed[0] > parsed[1] : false;
}

export default function FindHighlightsPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = use(params);
  const router = useRouter();

  const [match, setMatch] = useState<Match | null>(null);
  const [plays, setPlays] = useState<Play[] | null>(null);
  const [loaded, setLoaded] = useState(false);

  const [selectedPlayId, setSelectedPlayId] = useState<string | null>(null);
  const [mode, setMode] = useState<RenderModeChoice | null>(null);
  const [songOpen, setSongOpen] = useState(false);
  const [creating, setCreating] = useState(false);
  const [pendingSongId, setPendingSongId] = useState<string | null>(null);

  useEffect(() => {
    let active = true;
    (async () => {
      const [m, p] = await Promise.all([api.getMatch(id), api.findClips(id)]);
      if (!active) return;
      setMatch(m);
      setPlays(p);
      setLoaded(true);
    })();
    return () => {
      active = false;
    };
  }, [id]);

  const selectedPlay = plays?.find((p) => p.id === selectedPlayId) ?? null;
  const busy = creating || pendingSongId !== null;

  function onPickMode(next: RenderModeChoice) {
    if (busy) return;
    setMode(next);
    if (next === 'music') setSongOpen(true);
  }

  async function createReel(chosenMode: RenderModeChoice, songId?: string) {
    if (!selectedPlayId) return;
    await api.createVideo({ matchId: id, playId: selectedPlayId, mode: chosenMode, songId });
    router.push('/videos');
  }

  // The sticky-bar CTA commits Clean POV directly; Music defers to the song
  // picker, so the CTA re-opens the dialog in that mode.
  async function onCreate() {
    if (busy || !selectedPlayId || mode === null) return;
    if (mode === 'music') {
      setSongOpen(true);
      return;
    }
    setCreating(true);
    try {
      await createReel('clean');
    } catch {
      setCreating(false);
    }
  }

  async function onChooseSong(songId: string) {
    if (pendingSongId) return;
    setPendingSongId(songId);
    try {
      await createReel('music', songId);
    } catch {
      setPendingSongId(null);
    }
  }

  if (!loaded) {
    return <LoadingState />;
  }

  if (!match) {
    return (
      <div className="py-24 text-center">
        <p className="text-muted-foreground">Match not found.</p>
        <Button variant="secondary" className="mt-4" onClick={() => router.push('/matches')}>
          Back to matches
        </Button>
      </div>
    );
  }

  const playList = plays ?? [];
  const n = playList.length;
  const score = parseScore(match.score);
  const win = isWin(match.score);
  const fromUpload = match.source === 'upload';
  const backHref = fromUpload ? '/upload' : '/matches';
  const backLabel = fromUpload ? 'Upload' : 'Matches';

  return (
    <div className="flex min-h-[calc(100vh-5rem)] flex-col gap-8 pb-2">
      <Button
        variant="ghost"
        size="sm"
        className="-ml-2 w-fit text-muted-foreground"
        onClick={() => router.push(backHref)}
      >
        <ArrowLeft className="size-4" />
        {backLabel}
      </Button>

      {/* Compact match summary strip */}
      <section className="flex flex-wrap items-center gap-x-6 gap-y-4 rounded-xl border border-border bg-card px-5 py-4">
        <div className="flex items-center gap-3">
          <ScoreBar win={win} className="h-8" />
          <div className="flex flex-col gap-1">
            <h1 className="font-[family-name:var(--font-display)] text-2xl font-semibold tracking-tight text-foreground">
              {match.map}
            </h1>
            <Badge variant="outline" className="font-[family-name:var(--font-mono)]">
              {win ? 'WIN' : 'LOSS'}
            </Badge>
          </div>
        </div>

        <StatMono label="Score" value={score ? `${score[0]}-${score[1]}` : match.score} />

        <div className="flex flex-wrap items-center gap-x-6 gap-y-3">
          <StatMono label="K" value={match.stats.kills} />
          <StatMono label="D" value={match.stats.deaths} />
          <StatMono label="A" value={match.stats.assists} />
          <StatMono label="MVP" value={match.stats.mvps} />
          <StatMono label="K/D" value={formatKd(match.stats.kd)} accent />
        </div>
      </section>

      {/* Highlights filmstrip */}
      <section className="flex flex-col gap-4">
        <div className="flex flex-col gap-1">
          <h2 className="font-[family-name:var(--font-display)] text-xl font-semibold tracking-tight text-foreground">
            We found{' '}
            <span className="font-[family-name:var(--font-mono)] tabular-nums text-primary">
              {n}
            </span>{' '}
            {n === 1 ? 'highlight' : 'highlights'}
          </h2>
          <p className="text-sm text-muted-foreground">
            Pick the play you want to forge into a reel.
          </p>
        </div>

        {n === 0 ? (
          <p className="rounded-xl border border-dashed border-border bg-card/50 px-5 py-10 text-center text-sm text-muted-foreground">
            No highlight-worthy plays found in this match.
          </p>
        ) : (
          <Filmstrip>
            {playList.map((play) => (
              <PlayTile
                key={play.id}
                play={play}
                selected={selectedPlayId === play.id}
                onSelect={() => !busy && setSelectedPlayId(play.id)}
              />
            ))}
          </Filmstrip>
        )}
      </section>

      {/* Mode cards */}
      {n > 0 ? (
        <section className="flex flex-col gap-4">
          <SectionEyebrow label="Render mode" />
          <ModeCards value={mode} onChange={onPickMode} disabled={!selectedPlayId || busy} />
        </section>
      ) : null}

      <div className="flex-1" />

      {/* Sticky action bar */}
      {n > 0 ? (
        <CreateReelBar
          playLabel={selectedPlay?.label ?? null}
          mode={mode}
          creating={creating}
          onCreate={onCreate}
        />
      ) : null}

      <SongPickerDialog
        open={songOpen}
        onOpenChange={(open) => {
          if (pendingSongId) return;
          setSongOpen(open);
        }}
        onChoose={onChooseSong}
        pendingSongId={pendingSongId}
      />
    </div>
  );
}

function LoadingState() {
  return (
    <div className="flex flex-col gap-8">
      <Skeleton className="h-8 w-24" />
      <Skeleton className="h-20 w-full rounded-xl" />
      <div className="flex flex-col gap-4">
        <Skeleton className="h-6 w-48" />
        <div className="flex gap-3">
          {[0, 1, 2].map((i) => (
            <Skeleton key={i} className="h-[180px] w-[220px] rounded-xl" />
          ))}
        </div>
      </div>
      <div className="grid gap-4 sm:grid-cols-2">
        <Skeleton className="h-36 rounded-xl" />
        <Skeleton className="h-36 rounded-xl" />
      </div>
    </div>
  );
}
