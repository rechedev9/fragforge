'use client';

import { useState } from 'react';
import { Heart, Play } from 'lucide-react';
import type { FeedItem } from '@/lib/api/types';
import { Avatar, AvatarFallback, AvatarImage } from '@/components/ui/avatar';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { timeAgo } from '@/lib/format';
import { cn } from '@/lib/utils';

export type FeedCardProps = {
  item: FeedItem;
};

/**
 * One community reel in the feed, NEON HUD style: a thumbnail with a
 * click-to-play affordance, a Chakra Petch title, a dim mono handle, and a
 * magenta heart — magenta is reserved for likes per the skin's color rule, so
 * it never turns cyan even once liked. The mockup also shows a mono
 * duration/aspect-ratio badge on the thumbnail; `FeedItem` carries neither
 * field, so it is left out here rather than displaying a fabricated number.
 */
export function FeedCard({ item }: FeedCardProps) {
  const [liked, setLiked] = useState(false);
  const [playerOpen, setPlayerOpen] = useState(false);
  const likeCount = item.likes + (liked ? 1 : 0);
  const initials = item.author.slice(0, 2).toUpperCase();

  return (
    <figure className="studio-panel studio-panel-interactive group overflow-hidden">
      <div className="relative aspect-video overflow-hidden bg-muted">
        {/* eslint-disable-next-line @next/next/no-img-element -- remote seed thumbnail */}
        <img
          src={item.thumbnailUrl}
          alt=""
          loading="lazy"
          decoding="async"
          className="size-full object-cover transition-transform duration-300 ease-out group-hover:scale-[1.035]"
        />
        <div className="pointer-events-none absolute inset-0 bg-gradient-to-t from-background/75 via-transparent to-background/10" />
        <span className="pointer-events-none absolute left-3 top-3 border border-primary/35 bg-background/80 px-2.5 py-1 font-[family-name:var(--font-mono)] text-xs uppercase tracking-[0.12em] text-foreground backdrop-blur-sm">
          {item.map}
        </span>

        <button
          type="button"
          onClick={() => setPlayerOpen(true)}
          aria-label={`Reproducir ${item.title}`}
          className="absolute inset-0 flex items-center justify-center outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring"
        >
          <span className="flex size-12 items-center justify-center rounded-full border border-white/45 bg-background/65 text-foreground shadow-lg backdrop-blur-sm transition-all group-hover:scale-105 group-hover:border-primary group-hover:text-primary">
            <Play className="ml-0.5 size-5 fill-current" aria-hidden />
          </span>
        </button>
      </div>

      <figcaption className="flex flex-col gap-4 p-4">
        <div className="min-w-0">
          <h3 className="line-clamp-2 font-[family-name:var(--font-display)] text-lg font-bold leading-snug text-foreground">
            {item.title}
          </h3>
          <p className="mt-1 font-[family-name:var(--font-mono)] text-xs uppercase tracking-[0.1em] text-muted-foreground">
            {timeAgo(item.createdAt)}
          </p>
        </div>

        <div className="flex items-center justify-between gap-3 border-t border-border/65 pt-3">
          <div className="flex min-w-0 items-center gap-2.5">
            <Avatar className="size-8 rounded-md border border-border-strong">
              <AvatarImage src={item.authorAvatarUrl} alt={item.author} />
              <AvatarFallback className="rounded-md text-xs">{initials}</AvatarFallback>
            </Avatar>
            <span className="min-w-0 truncate font-[family-name:var(--font-mono)] text-xs tracking-[0.08em] text-muted-foreground">
              @{item.author}
            </span>
          </div>
          <button
            type="button"
            onClick={() => setLiked((v) => !v)}
            aria-pressed={liked}
            aria-label={liked ? 'Quitar me gusta' : 'Me gusta'}
            className="inline-flex h-11 shrink-0 items-center gap-2 border border-stream/35 bg-stream/[0.06] px-3 font-[family-name:var(--font-mono)] text-xs tabular-nums text-stream outline-none transition-colors hover:border-stream/65 hover:bg-stream/10 focus-visible:ring-2 focus-visible:ring-stream focus-visible:ring-offset-2 focus-visible:ring-offset-background"
          >
            <Heart className={cn('size-4', liked && 'fill-current')} aria-hidden />
            {likeCount.toLocaleString()}
          </button>
        </div>
      </figcaption>

      <Dialog open={playerOpen} onOpenChange={setPlayerOpen}>
        <DialogContent className="max-w-md">
          <DialogHeader>
            <DialogTitle className="truncate">{item.title}</DialogTitle>
            <DialogDescription>
              {item.author} · {item.map}
            </DialogDescription>
          </DialogHeader>
          <video
            src={item.videoUrl}
            controls
            autoPlay
            playsInline
            className="mx-auto max-h-[72vh] w-auto rounded-lg bg-black"
          />
        </DialogContent>
      </Dialog>
    </figure>
  );
}
