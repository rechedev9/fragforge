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
    <figure className="group border border-primary/14 bg-card/80">
      <div className="relative aspect-video overflow-hidden bg-muted">
        {/* eslint-disable-next-line @next/next/no-img-element -- remote seed thumbnail */}
        <img
          src={item.thumbnailUrl}
          alt=""
          className="size-full object-cover transition-transform duration-300 ease-out group-hover:scale-[1.04]"
        />

        {/* click-to-play overlay */}
        <button
          type="button"
          onClick={() => setPlayerOpen(true)}
          aria-label={`Reproducir ${item.title}`}
          className="absolute inset-0 flex items-center justify-center opacity-0 transition-opacity duration-200 focus-visible:opacity-100 group-hover:opacity-100"
        >
          <span className="flex size-11 items-center justify-center rounded-full border border-white/40 bg-background/50 text-foreground">
            <Play className="ml-0.5 size-4 fill-current" aria-hidden />
          </span>
        </button>
      </div>

      <div className="flex flex-col gap-1.5 p-3.5">
        <h3 className="line-clamp-2 font-[family-name:var(--font-display)] text-sm font-bold leading-snug text-foreground">
          {item.title}
        </h3>
        <div className="flex items-center justify-between gap-2">
          <div className="flex min-w-0 items-center gap-2">
            <Avatar className="size-5 rounded-none">
              <AvatarImage src={item.authorAvatarUrl} alt={item.author} />
              <AvatarFallback className="rounded-none text-[0.55rem]">{initials}</AvatarFallback>
            </Avatar>
            <span className="min-w-0 truncate font-[family-name:var(--font-mono)] text-[10px] uppercase tracking-[0.1em] text-muted-foreground/70">
              @{item.author}
            </span>
          </div>
          <button
            type="button"
            onClick={() => setLiked((v) => !v)}
            aria-pressed={liked}
            aria-label={liked ? 'Quitar me gusta' : 'Me gusta'}
            className="inline-flex shrink-0 items-center gap-1 font-[family-name:var(--font-mono)] text-[10px] tabular-nums text-destructive"
          >
            <Heart className={cn('size-3.5', liked && 'fill-current')} aria-hidden />
            {likeCount.toLocaleString()}
          </button>
        </div>
      </div>

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
