'use client';

import { useEffect, useState } from 'react';
import { Loader2, Pause, Play } from 'lucide-react';
import type { Song } from '@/lib/api/types';
import { api } from '@/lib/api';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Button } from '@/components/ui/button';
import { cn } from '@/lib/utils';

export type SongPickerDialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** Called with the chosen song id; the page creates the music video. */
  onChoose: (songId: string) => void;
  /** Song id currently being committed (its "Use this track" button spins). */
  pendingSongId?: string | null;
};

/**
 * SongPickerDialog — the Music Edit soundtrack picker. Lists songs from the
 * api; each row has a UI-only preview toggle (no real audio — a tiny equalizer
 * animates while "playing") and a lime "Use this track" that creates the reel.
 */
export function SongPickerDialog({
  open,
  onOpenChange,
  onChoose,
  pendingSongId,
}: SongPickerDialogProps) {
  const [songs, setSongs] = useState<Song[] | null>(null);
  const [playingId, setPlayingId] = useState<string | null>(null);

  useEffect(() => {
    if (!open) return;
    let active = true;
    (async () => {
      const next = await api.listSongs();
      if (active) setSongs(next);
    })();
    return () => {
      active = false;
    };
  }, [open]);

  // Stop the fake preview whenever the dialog closes.
  useEffect(() => {
    if (!open) setPlayingId(null);
  }, [open]);

  const committing = pendingSongId != null;

  return (
    <Dialog open={open} onOpenChange={(next) => (committing ? null : onOpenChange(next))}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle className="font-[family-name:var(--font-display)] tracking-tight">
            Pick a track
          </DialogTitle>
          <DialogDescription>
            We&apos;ll cut the action to the beat. Preview is for vibe only.
          </DialogDescription>
        </DialogHeader>

        <div className="-mx-2 max-h-[22rem] overflow-y-auto px-2">
          {songs === null ? (
            <SongRowSkeletons />
          ) : (
            <ul className="flex flex-col gap-1.5">
              {songs.map((song) => (
                <SongRow
                  key={song.id}
                  song={song}
                  playing={playingId === song.id}
                  pending={pendingSongId === song.id}
                  disabled={committing}
                  onTogglePlay={() =>
                    setPlayingId((cur) => (cur === song.id ? null : song.id))
                  }
                  onUse={() => onChoose(song.id)}
                />
              ))}
            </ul>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}

type SongRowProps = {
  song: Song;
  playing: boolean;
  pending: boolean;
  disabled: boolean;
  onTogglePlay: () => void;
  onUse: () => void;
};

function SongRow({ song, playing, pending, disabled, onTogglePlay, onUse }: SongRowProps) {
  return (
    <li className="flex items-center gap-3 rounded-lg border border-border bg-card px-3 py-2.5">
      <Button
        type="button"
        variant="ghost"
        size="icon-sm"
        onClick={onTogglePlay}
        aria-label={playing ? `Pause ${song.title}` : `Preview ${song.title}`}
        className="shrink-0 text-muted-foreground hover:text-foreground"
      >
        {playing ? <Pause /> : <Play />}
      </Button>

      <div className="min-w-0 flex-1">
        <p className="truncate text-sm font-medium text-foreground">{song.title}</p>
        <p className="truncate text-xs text-muted-foreground">{song.artist}</p>
      </div>

      {playing ? <Equalizer /> : null}

      <Button
        type="button"
        size="sm"
        onClick={onUse}
        disabled={disabled}
        className="shrink-0"
      >
        {pending ? <Loader2 className="animate-spin" /> : null}
        Use this track
      </Button>
    </li>
  );
}

/**
 * A purely decorative equalizer that animates while a preview is "playing".
 * The keyframes are scoped here (globals.css is foundation-owned) and honor
 * prefers-reduced-motion by holding the bars at a static mid height.
 */
function Equalizer() {
  return (
    <span aria-hidden className="flex h-4 items-end gap-0.5">
      <style>{EQ_KEYFRAMES}</style>
      {[0, 1, 2, 3].map((i) => (
        <span
          key={i}
          className="w-0.5 rounded-full bg-primary"
          style={{ height: '40%', animation: `ff-eq 0.9s ease-in-out ${i * 0.12}s infinite` }}
        />
      ))}
    </span>
  );
}

const EQ_KEYFRAMES = `
@keyframes ff-eq {
  0%, 100% { height: 30%; }
  50% { height: 100%; }
}
@media (prefers-reduced-motion: reduce) {
  @keyframes ff-eq { 0%, 100% { height: 55%; } }
}
`;

function SongRowSkeletons() {
  return (
    <ul className="flex flex-col gap-1.5">
      {[0, 1, 2, 3].map((i) => (
        <li
          key={i}
          className="flex items-center gap-3 rounded-lg border border-border bg-card px-3 py-2.5"
        >
          <div className="size-8 shrink-0 animate-pulse rounded-md bg-accent" />
          <div className="flex-1 space-y-1.5">
            <div className="h-3.5 w-24 animate-pulse rounded bg-accent" />
            <div className="h-3 w-16 animate-pulse rounded bg-accent" />
          </div>
          <div className="h-8 w-28 animate-pulse rounded-md bg-accent" />
        </li>
      ))}
    </ul>
  );
}
