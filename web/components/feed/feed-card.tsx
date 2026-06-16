'use client';

import { useState } from 'react';
import { Heart, MapPin, Play } from 'lucide-react';
import type { FeedItem } from '@/lib/api/types';
import { Avatar, AvatarFallback, AvatarImage } from '@/components/ui/avatar';
import { timeAgo } from '@/lib/format';
import { cn } from '@/lib/utils';

export type FeedCardProps = {
  item: FeedItem;
};

/**
 * One community reel in the feed: a 9:16 portrait thumbnail with a soft gradient
 * foot, author avatar + name, a map chip, and a like toggle. Lime is reserved
 * for the liked state; everything else stays neutral charcoal.
 */
export function FeedCard({ item }: FeedCardProps) {
  const [liked, setLiked] = useState(false);
  const likeCount = item.likes + (liked ? 1 : 0);
  const initials = item.author.slice(0, 2).toUpperCase();

  return (
    <figure className="group break-inside-avoid overflow-hidden rounded-xl border border-border bg-card">
      <div className="relative aspect-[9/16] overflow-hidden bg-muted">
        {/* Plain <img>: mock thumbnails come from external placeholder hosts. */}
        {/* eslint-disable-next-line @next/next/no-img-element */}
        <img
          src={item.thumbnailUrl}
          alt={item.title}
          loading="lazy"
          className="size-full object-cover transition-transform duration-300 ease-out group-hover:scale-[1.04]"
        />

        {/* soft gradient foot for legibility */}
        <div className="pointer-events-none absolute inset-0 bg-gradient-to-t from-background via-background/15 to-transparent" />

        {/* map chip */}
        <div className="absolute left-2.5 top-2.5">
          <span className="inline-flex items-center gap-1 rounded-full border border-white/10 bg-background/70 px-2 py-0.5 text-[0.7rem] font-medium text-foreground/90 backdrop-blur-sm">
            <MapPin className="size-3 text-muted-foreground" aria-hidden />
            {item.map}
          </span>
        </div>

        {/* hover play affordance */}
        <div className="pointer-events-none absolute inset-0 flex items-center justify-center opacity-0 transition-opacity duration-200 group-hover:opacity-100">
          <span className="flex size-14 items-center justify-center rounded-full bg-background/70 text-foreground ring-1 ring-white/15 backdrop-blur-sm">
            <Play className="ml-0.5 size-6 fill-current" aria-hidden />
          </span>
        </div>

        {/* footer: title + author */}
        <figcaption className="absolute inset-x-0 bottom-0 space-y-2 p-3">
          <h3 className="line-clamp-2 text-sm font-semibold leading-snug text-foreground drop-shadow-sm">
            {item.title}
          </h3>
          <div className="flex items-center gap-2">
            <Avatar className="size-6">
              <AvatarImage src={item.authorAvatarUrl} alt={item.author} />
              <AvatarFallback className="text-[0.6rem]">{initials}</AvatarFallback>
            </Avatar>
            <span className="min-w-0 flex-1 truncate text-xs font-medium text-foreground/90">
              {item.author}
            </span>
            <span className="font-[family-name:var(--font-mono)] text-[0.7rem] tabular-nums text-muted-foreground">
              {timeAgo(item.createdAt)}
            </span>
          </div>
        </figcaption>
      </div>

      <div className="flex items-center justify-end px-3 py-2">
        <button
          type="button"
          onClick={() => setLiked((v) => !v)}
          aria-pressed={liked}
          aria-label={liked ? 'Unlike' : 'Like'}
          className={cn(
            'inline-flex items-center gap-1.5 text-sm font-medium transition-colors',
            liked ? 'text-primary' : 'text-muted-foreground hover:text-foreground',
          )}
        >
          <Heart className={cn('size-4', liked && 'fill-current')} aria-hidden />
          <span className="font-[family-name:var(--font-mono)] tabular-nums">
            {likeCount.toLocaleString()}
          </span>
        </button>
      </div>
    </figure>
  );
}
