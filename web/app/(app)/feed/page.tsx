'use client';

import { useEffect, useMemo, useState } from 'react';
import { Compass } from 'lucide-react';
import { api } from '@/lib/api';
import type { FeedItem } from '@/lib/api/types';
import { SectionEyebrow } from '@/components/brand/section-eyebrow';
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group';
import { FeedGrid, FeedGridSkeleton } from '@/components/feed/feed-grid';

/** RECIENTES sorts by publish time; TOP SEMANA sorts the last 7 days by likes
 * (falling back to the full list when nothing falls in that window, so a
 * short-lived seed/mock dataset never renders an empty grid). */
type FeedSort = 'recent' | 'top-week';
const WEEK_MS = 7 * 24 * 60 * 60 * 1000;

function sortFeed(items: FeedItem[], sort: FeedSort): FeedItem[] {
  if (sort === 'recent') {
    return [...items].sort((a, b) => b.createdAt - a.createdAt);
  }
  const cutoff = Date.now() - WEEK_MS;
  const thisWeek = items.filter((item) => item.createdAt >= cutoff);
  const pool = thisWeek.length > 0 ? thisWeek : items;
  return [...pool].sort((a, b) => b.likes - a.likes);
}

export default function FeedPage() {
  const [items, setItems] = useState<FeedItem[] | null>(null);
  const [sort, setSort] = useState<FeedSort>('recent');

  useEffect(() => {
    let active = true;
    api.listFeed().then((feed) => {
      if (active) setItems(feed);
    });
    return () => {
      active = false;
    };
  }, []);

  const visible = useMemo(() => sortFeed(items ?? [], sort), [items, sort]);

  return (
    <div className="flex flex-col gap-7">
      <header className="flex flex-col gap-2.5">
        <SectionEyebrow number={5} label="FEED" />
        <div className="flex flex-col gap-3 sm:flex-row sm:items-baseline sm:justify-between sm:gap-6">
          <h1 className="font-[family-name:var(--font-display)] text-3xl font-bold leading-none tracking-tight text-foreground sm:text-[34px]">
            LA COMUNIDAD FORJA
          </h1>
          {items !== null && items.length > 0 ? (
            <ToggleGroup
              type="single"
              value={sort}
              onValueChange={(v) => v && setSort(v as FeedSort)}
              className="w-fit gap-2"
              aria-label="Ordenar feed"
            >
              <ToggleGroupItem
                value="recent"
                aria-label="Más recientes"
                className="h-auto rounded-none border border-primary/25 bg-transparent px-3.5 py-1.5 font-[family-name:var(--font-mono)] text-[11px] uppercase tracking-[0.14em] text-muted-foreground first:rounded-none last:rounded-none hover:bg-primary/10 hover:text-foreground data-[state=on]:border-primary data-[state=on]:bg-primary data-[state=on]:text-primary-foreground"
              >
                RECIENTES
              </ToggleGroupItem>
              <ToggleGroupItem
                value="top-week"
                aria-label="Top de la semana"
                className="h-auto rounded-none border border-primary/25 bg-transparent px-3.5 py-1.5 font-[family-name:var(--font-mono)] text-[11px] uppercase tracking-[0.14em] text-muted-foreground first:rounded-none last:rounded-none hover:bg-primary/10 hover:text-foreground data-[state=on]:border-primary data-[state=on]:bg-primary data-[state=on]:text-primary-foreground"
              >
                TOP SEMANA
              </ToggleGroupItem>
            </ToggleGroup>
          ) : null}
        </div>
        <p className="max-w-2xl text-sm text-muted-foreground">
          Reels forjados en los rigs de la comunidad. Mira uno, deja un like.
        </p>
      </header>

      {items === null ? (
        <FeedGridSkeleton />
      ) : items.length === 0 ? (
        <EmptyState />
      ) : (
        <FeedGrid items={visible} />
      )}
    </div>
  );
}

function EmptyState() {
  return (
    <div className="flex flex-col items-center justify-center gap-3 border border-dashed border-border bg-card/40 py-24 text-center">
      <span className="flex size-12 items-center justify-center border border-border bg-accent text-muted-foreground">
        <Compass className="size-5" aria-hidden />
      </span>
      <p className="text-base font-semibold text-foreground">Todavía no hay nada publicado</p>
      <p className="max-w-sm text-sm text-muted-foreground">
        Sé el primero en publicar un highlight — tus reels aparecerán aquí para todos.
      </p>
    </div>
  );
}
