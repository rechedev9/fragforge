'use client';

import { use, useEffect, useState } from 'react';
import { useRouter } from 'next/navigation';
import { ArrowLeft, Music } from 'lucide-react';
import type { Match, Play, Preset } from '@/lib/api/types';
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
import { PresetCards } from '@/components/clips/preset-cards';
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

  const [presets, setPresets] = useState<Preset[] | null>(null);
  const [selectedPlayId, setSelectedPlayId] = useState<string | null>(null);
  const [variant, setVariant] = useState<string | null>(null);
  const [songId, setSongId] = useState<string | null>(null);
  const [songTitle, setSongTitle] = useState<string | null>(null);
  const [songOpen, setSongOpen] = useState(false);
  const [creating, setCreating] = useState(false);

  useEffect(() => {
    let active = true;
    (async () => {
      try {
        const [m, p] = await Promise.all([api.getMatch(id), api.findClips(id)]);
        if (!active) return;
        setMatch(m);
        setPlays(p);
      } catch {
        // A failed fetch falls through to the "Match not found" branch below.
        if (!active) return;
        setMatch(null);
        setPlays([]);
      } finally {
        if (active) setLoaded(true);
      }
    })();
    return () => {
      active = false;
    };
  }, [id]);

  // Load the reel presets and default to the registry's default (first) preset.
  useEffect(() => {
    let active = true;
    (async () => {
      try {
        const list = await api.listPresets();
        if (!active) return;
        setPresets(list);
        setVariant((cur) => cur ?? (list.find((p) => p.default)?.name ?? list[0]?.name ?? null));
      } catch {
        if (active) setPresets([]);
      }
    })();
    return () => {
      active = false;
    };
  }, []);

  const selectedPlay = plays?.find((p) => p.id === selectedPlayId) ?? null;
  const presetLabel = presets?.find((p) => p.name === variant)?.label ?? null;
  const busy = creating;

  async function onCreate() {
    if (busy || !selectedPlayId || variant === null) return;
    setCreating(true);
    try {
      await api.createVideo({
        matchId: id,
        playId: selectedPlayId,
        mode: songId ? 'music' : 'clean',
        songId: songId ?? undefined,
        variant,
      });
      router.push('/videos');
    } catch {
      setCreating(false);
    }
  }

  function onChooseSong(chosenId: string, chosenTitle: string) {
    setSongId(chosenId);
    setSongTitle(chosenTitle);
    setSongOpen(false);
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
  // Uploaded demos have no round score (the parser computes none): hide the
  // win/loss + score chips and show the highlight count instead.
  const hasScore = match.score.trim() !== '';
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
              {hasScore ? (win ? 'WIN' : 'LOSS') : `${n} ${n === 1 ? 'HIGHLIGHT' : 'HIGHLIGHTS'}`}
            </Badge>
          </div>
        </div>

        {hasScore ? (
          <StatMono label="Score" value={score ? `${score[0]}-${score[1]}` : match.score} />
        ) : null}

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
            <span className="font-[family-name:var(--font-mono)] tabular-nums text-primary">{n}</span>{' '}
            {n === 1 ? 'highlight' : 'highlights'}
          </h2>
          <p className="text-sm text-muted-foreground">Pick the play you want to forge into a reel.</p>
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

      {/* Preset picker */}
      {n > 0 ? (
        <section className="flex flex-col gap-4">
          <SectionEyebrow label="Reel preset" />
          {presets === null ? (
            <div className="grid gap-4 sm:grid-cols-3">
              {[0, 1, 2].map((i) => (
                <Skeleton key={i} className="h-40 rounded-xl" />
              ))}
            </div>
          ) : presets.length === 0 ? (
            <p className="rounded-xl border border-dashed border-border bg-card/50 px-5 py-6 text-center text-sm text-muted-foreground">
              Couldn&apos;t load reel presets. Refresh the page to try again.
            </p>
          ) : (
            <PresetCards
              presets={presets}
              value={variant}
              onChange={setVariant}
              disabled={!selectedPlayId || busy}
            />
          )}
        </section>
      ) : null}

      {/* Soundtrack (optional) */}
      {n > 0 ? (
        <section className="flex flex-col gap-4">
          <SectionEyebrow label="Soundtrack (optional)" />
          {songTitle ? (
            <div className="flex items-center justify-between gap-3 rounded-xl border border-border bg-card px-5 py-4">
              <div className="flex min-w-0 items-center gap-3">
                <Music className="size-5 shrink-0 text-primary" />
                <div className="min-w-0">
                  <p className="truncate text-sm font-medium text-foreground">{songTitle}</p>
                  <p className="text-xs text-muted-foreground">Soundtrack added</p>
                </div>
              </div>
              <div className="flex shrink-0 gap-2">
                <Button variant="secondary" size="sm" disabled={busy} onClick={() => setSongOpen(true)}>
                  Change
                </Button>
                <Button
                  variant="ghost"
                  size="sm"
                  disabled={busy}
                  onClick={() => {
                    setSongId(null);
                    setSongTitle(null);
                  }}
                >
                  Remove
                </Button>
              </div>
            </div>
          ) : (
            <button
              type="button"
              disabled={busy}
              onClick={() => setSongOpen(true)}
              className="flex items-center gap-3 rounded-xl border border-dashed border-border bg-card/50 px-5 py-4 text-left text-sm text-muted-foreground transition-colors hover:border-muted-foreground/40 hover:text-foreground disabled:cursor-not-allowed disabled:opacity-50"
            >
              <Music className="size-5" />
              Add a soundtrack — sync the action to a track.
            </button>
          )}
        </section>
      ) : null}

      <div className="flex-1" />

      {/* Sticky action bar */}
      {n > 0 ? (
        <CreateReelBar
          playLabel={selectedPlay?.label ?? null}
          presetLabel={presetLabel}
          songTitle={songTitle}
          creating={creating}
          onCreate={onCreate}
        />
      ) : null}

      <SongPickerDialog
        open={songOpen}
        onOpenChange={setSongOpen}
        onChoose={onChooseSong}
        selectedSongId={songId}
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
      <div className="grid gap-4 sm:grid-cols-3">
        <Skeleton className="h-40 rounded-xl" />
        <Skeleton className="h-40 rounded-xl" />
        <Skeleton className="h-40 rounded-xl" />
      </div>
    </div>
  );
}
