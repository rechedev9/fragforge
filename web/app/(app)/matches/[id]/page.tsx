'use client';

import { use, useEffect, useState } from 'react';
import { useRouter } from 'next/navigation';
import { Music } from 'lucide-react';
import type { EditConfig, Match, Play, Preset } from '@/lib/api/types';
import { api } from '@/lib/api';
import { DEFAULT_EDIT_CONFIG } from '@/lib/api/reel-store';
import { formatKd, playsSelectionLabel, ratingClass, timeAgo } from '@/lib/format';
import { cn } from '@/lib/utils';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { ScoreBar } from '@/components/brand/score-bar';
import { StatMono } from '@/components/brand/stat-mono';
import { SectionEyebrow } from '@/components/brand/section-eyebrow';
import { PlayList } from '@/components/clips/play-list';
import { PresetCards } from '@/components/clips/preset-cards';
import { CreateReelBar } from '@/components/clips/create-reel-bar';
import { EditOptions } from '@/components/clips/edit-options';
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
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set());
  const [variant, setVariant] = useState<string | null>(null);
  const [songId, setSongId] = useState<string | null>(null);
  const [songTitle, setSongTitle] = useState<string | null>(null);
  const [editConfig, setEditConfig] = useState<EditConfig>(DEFAULT_EDIT_CONFIG);
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
        // A failed fetch falls through to the "Partida no encontrada" branch below.
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

  // Plan order (the order plays appear in the list), not click order — the
  // Set only tracks membership, so the source of truth for order is always
  // the filter below.
  const selectedPlays = (plays ?? []).filter((p) => selectedIds.has(p.id));
  const selectionLabel = playsSelectionLabel(selectedPlays);
  const presetLabel = presets?.find((p) => p.name === variant)?.label ?? null;
  const busy = creating;

  function toggleSelect(playId: string) {
    if (busy) return;
    setSelectedIds((prev) => {
      const next = new Set(prev);
      if (next.has(playId)) next.delete(playId);
      else next.add(playId);
      return next;
    });
  }

  function selectAll() {
    if (busy || !plays) return;
    setSelectedIds(new Set(plays.map((p) => p.id)));
  }

  function clearSelection() {
    if (busy) return;
    setSelectedIds(new Set());
  }

  async function onCreate() {
    if (busy || selectedPlays.length === 0 || variant === null) return;
    setCreating(true);
    try {
      await api.createVideo({
        matchId: id,
        playIds: selectedPlays.map((p) => p.id),
        mode: songId ? 'music' : 'clean',
        songId: songId ?? undefined,
        variant,
        editConfig,
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
        <p className="text-muted-foreground">Partida no encontrada.</p>
        <Button variant="secondary" className="mt-4" onClick={() => router.push('/matches')}>
          VOLVER A PARTIDAS
        </Button>
      </div>
    );
  }

  const playList = plays ?? [];
  const n = playList.length;
  const score = parseScore(match.score);
  const win = isWin(match.score);
  // Uploaded demos have no round score (the parser computes none): hide the
  // score block and let the mono meta line carry the play count instead.
  const hasScore = match.score.trim() !== '';
  const fromUpload = match.source === 'upload';
  const backHref = fromUpload ? '/upload' : '/matches';
  const backLabel = fromUpload ? 'SUBIR DEMO' : 'PARTIDAS';
  const meta = [
    timeAgo(match.playedAt),
    `${n} ${n === 1 ? 'jugada' : 'jugadas'}`,
  ].join(' · ');

  // Scoreboard extras exist only on enriched (uploaded) matches; mock/seed
  // matches show the classic K/D/A line. `hasRich` gates the ADR/KAST/HS row.
  const { rating = 0, adr, kast, hsPct } = match.stats;
  const hasRich = adr !== undefined;
  const hasRating = rating > 0;

  return (
    <div className="flex min-h-[calc(100vh-5rem)] flex-col gap-8 pb-2">
      <button
        type="button"
        onClick={() => router.push(backHref)}
        className="w-fit cursor-pointer font-[family-name:var(--font-mono)] text-[11px] tracking-[0.22em] text-muted-foreground/70 transition-colors hover:text-primary"
      >
        ◂ {backLabel}
      </button>

      {/* Match summary — accent bar + map title + mono meta, score, stat strip. */}
      <section className="flex flex-col gap-5 sm:flex-row sm:items-center sm:justify-between sm:gap-6">
        <div className="flex items-center gap-5">
          <ScoreBar win={win} className="h-[52px] w-[3px]" />
          <div className="flex flex-col gap-1">
            <h1 className="font-[family-name:var(--font-display)] text-[28px] font-bold uppercase leading-none tracking-tight text-foreground sm:text-[32px]">
              {match.map}
            </h1>
            <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
              <span className="font-[family-name:var(--font-mono)] text-[10.5px] uppercase tracking-[0.14em] text-muted-foreground/70">
                {meta}
              </span>
              {hasRating ? (
                <span className="inline-flex items-baseline gap-1.5 border border-border bg-muted/40 px-2 py-0.5">
                  <span
                    className={cn(
                      'font-[family-name:var(--font-mono)] text-sm font-semibold tabular-nums',
                      ratingClass(rating),
                    )}
                  >
                    {rating.toFixed(2)}
                  </span>
                  <span className="font-[family-name:var(--font-mono)] text-[0.6rem] uppercase tracking-wider text-muted-foreground">
                    rating
                  </span>
                </span>
              ) : null}
            </div>
          </div>
          {hasScore && score ? (
            <div className="ml-2 font-[family-name:var(--font-mono)] text-[26px] tabular-nums">
              <span className={win ? 'text-primary' : 'text-muted-foreground'}>{score[0]}</span>
              <span className="text-muted-foreground/70"> : </span>
              <span className="text-muted-foreground">{score[1]}</span>
            </div>
          ) : null}
        </div>

        <div className="grid grid-cols-4 gap-x-5 gap-y-3 sm:flex sm:flex-wrap sm:items-center sm:gap-x-7">
          <StatMono label="K" value={match.stats.kills} />
          <StatMono label="D" value={match.stats.deaths} />
          <StatMono label="A" value={match.stats.assists} />
          {hasRich ? <StatMono label="ADR" value={Math.round(adr!)} /> : null}
          {hasRich ? <StatMono label="KAST" value={`${Math.round(kast!)}%`} /> : null}
          {hasRich ? <StatMono label="HS" value={`${Math.round(hsPct!)}%`} /> : null}
          {match.stats.mvps > 0 ? <StatMono label="MVP" value={match.stats.mvps} /> : null}
          <StatMono label="K/D" value={formatKd(match.stats.kd)} accent />
        </div>
      </section>

      {/* Detected plays */}
      <section className="flex flex-col gap-4">
        <div className="flex flex-col gap-1">
          <h2 className="font-[family-name:var(--font-mono)] text-xs uppercase tracking-[0.24em] text-primary">
            JUGADAS DETECTADAS{' '}
            <span className="tracking-[0.14em] text-muted-foreground/70">
              · <span className="tabular-nums">{n}</span>
            </span>
          </h2>
          <p className="text-sm text-muted-foreground">
            Elige las jugadas que quieras forjar en un reel; 2 o más se concatenan en uno.
          </p>
        </div>

        {n === 0 ? (
          <p className="border border-dashed border-border bg-card/50 px-5 py-10 text-center text-sm text-muted-foreground">
            No hay jugadas dignas de highlight en esta partida.
          </p>
        ) : (
          <PlayList
            plays={playList}
            selectedIds={selectedIds}
            onToggle={toggleSelect}
            onSelectAll={selectAll}
            onClear={clearSelection}
          />
        )}
      </section>

      {/* Preset picker */}
      {n > 0 ? (
        <section className="flex flex-col gap-4">
          <SectionEyebrow label="PRESET DEL REEL" />
          {presets === null ? (
            <div className="grid gap-4 sm:grid-cols-3">
              {[0, 1, 2].map((i) => (
                <Skeleton key={i} className="h-40" />
              ))}
            </div>
          ) : presets.length === 0 ? (
            <p className="border border-dashed border-border bg-card/50 px-5 py-6 text-center text-sm text-muted-foreground">
              No se pudieron cargar los presets. Recarga la página para reintentar.
            </p>
          ) : (
            <PresetCards
              presets={presets}
              value={variant}
              onChange={setVariant}
              disabled={selectedIds.size === 0 || busy}
            />
          )}
        </section>
      ) : null}

      {/* Edit options */}
      {n > 0 ? (
        <section className="flex flex-col gap-4">
          <SectionEyebrow label="OPCIONES DE EDICIÓN" />
          <EditOptions value={editConfig} onChange={setEditConfig} disabled={selectedIds.size === 0 || busy} />
        </section>
      ) : null}

      {/* Music (optional) */}
      {n > 0 ? (
        <section className="flex flex-col gap-4">
          <SectionEyebrow label="MÚSICA (OPCIONAL)" />
          {songTitle ? (
            <div className="flex items-center justify-between gap-3 border border-destructive/30 bg-card px-5 py-4">
              <div className="flex min-w-0 items-center gap-3">
                <Music className="size-5 shrink-0 text-destructive" />
                <div className="min-w-0">
                  <p className="truncate text-sm font-medium text-foreground">{songTitle}</p>
                  <p className="text-xs text-muted-foreground">Música añadida</p>
                </div>
              </div>
              <div className="flex shrink-0 gap-2">
                <Button variant="secondary" size="sm" disabled={busy} onClick={() => setSongOpen(true)}>
                  Cambiar
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
                  Quitar
                </Button>
              </div>
            </div>
          ) : (
            <button
              type="button"
              disabled={busy}
              onClick={() => setSongOpen(true)}
              className="flex items-center gap-3 border border-dashed border-border bg-card/50 px-5 py-4 text-left text-sm text-muted-foreground transition-colors hover:border-muted-foreground/40 hover:text-foreground disabled:cursor-not-allowed disabled:opacity-50"
            >
              <Music className="size-5" />
              Añade música: sincroniza la acción con un tema.
            </button>
          )}
        </section>
      ) : null}

      <div className="flex-1" />

      {/* Sticky action bar */}
      {n > 0 ? (
        <CreateReelBar
          selectionLabel={selectionLabel}
          presetLabel={presetLabel}
          songTitle={songTitle}
          format={editConfig.format}
          onFormatChange={(format) => setEditConfig({ ...editConfig, format })}
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
      <Skeleton className="h-20 w-full" />
      <div className="flex flex-col gap-4">
        <Skeleton className="h-6 w-48" />
        <div className="flex flex-col gap-px overflow-hidden border border-border">
          {[0, 1, 2, 3].map((i) => (
            <Skeleton key={i} className="h-[76px] w-full rounded-none" />
          ))}
        </div>
      </div>
      <div className="grid gap-4 sm:grid-cols-3">
        <Skeleton className="h-40" />
        <Skeleton className="h-40" />
        <Skeleton className="h-40" />
      </div>
    </div>
  );
}
