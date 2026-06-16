'use client';

import { useEffect, useState } from 'react';
import { Compass } from 'lucide-react';
import { api } from '@/lib/api';
import type { FeedItem } from '@/lib/api/types';
import { SectionEyebrow } from '@/components/brand/section-eyebrow';
import { FeedGrid, FeedGridSkeleton } from '@/components/feed/feed-grid';

export default function FeedPage() {
  const [items, setItems] = useState<FeedItem[] | null>(null);

  useEffect(() => {
    let active = true;
    api.listFeed().then((feed) => {
      if (active) setItems(feed);
    });
    return () => {
      active = false;
    };
  }, []);

  return (
    <div className="space-y-8">
      <header className="space-y-2">
        <h1 className="font-[family-name:var(--font-display)] text-3xl font-bold uppercase tracking-tight text-foreground sm:text-4xl">
          Feed
        </h1>
        <p className="max-w-2xl text-muted-foreground">
          Reels the community forged on their own rigs. Find a clip, drop a like.
        </p>
      </header>

      {items === null ? (
        <FeedGridSkeleton />
      ) : items.length === 0 ? (
        <EmptyState />
      ) : (
        <section className="space-y-4">
          <SectionEyebrow label="Latest reels" count={items.length} />
          <FeedGrid items={items} />
        </section>
      )}
    </div>
  );
}

function EmptyState() {
  return (
    <div className="flex flex-col items-center justify-center gap-3 rounded-xl border border-dashed border-border bg-card/40 py-24 text-center">
      <span className="flex size-12 items-center justify-center rounded-full bg-accent text-muted-foreground">
        <Compass className="size-5" aria-hidden />
      </span>
      <p className="text-base font-semibold text-foreground">Nothing published yet</p>
      <p className="max-w-sm text-sm text-muted-foreground">
        Be the first to publish a highlight — your reels show up here for everyone.
      </p>
    </div>
  );
}
