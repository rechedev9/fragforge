'use client';

import { useEffect, useMemo, useState, type ReactNode } from 'react';
import Link from 'next/link';
import { Compass, Film, UploadCloud } from 'lucide-react';
import { api } from '@/lib/api';
import type { FeedItem } from '@/lib/api/types';
import { FeedGrid, FeedGridSkeleton } from '@/components/feed/feed-grid';
import { StudioEmptyState } from '@/components/studio/empty-state';
import { STUDIO_FILTER_CHIP_CLASS } from '@/components/studio/filter-chip';
import { StudioPageHeader } from '@/components/studio/page-header';
import { Button } from '@/components/ui/button';
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group';

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

  let content: ReactNode;
  if (items === null) {
    content = <FeedGridSkeleton />;
  } else if (items.length === 0) {
    content = <FeedEmptyState />;
  } else {
    content = <FeedGrid items={visible} />;
  }

  return (
    <div className="flex flex-col gap-8 sm:gap-10">
      <StudioPageHeader
        number={5}
        label="FEED"
        title="LA COMUNIDAD FORJA"
        description="Reels forjados en los rigs de la comunidad. Mira uno, deja un like."
        actions={
          items !== null && items.length > 0 ? (
            <div className="w-full overflow-x-auto pb-1 lg:w-auto lg:pb-0">
              <ToggleGroup
                type="single"
                value={sort}
                onValueChange={(value) => value && setSort(value as FeedSort)}
                className="w-max gap-2"
                aria-label="Ordenar feed"
              >
                <ToggleGroupItem
                  value="recent"
                  aria-label="Más recientes"
                  className={STUDIO_FILTER_CHIP_CLASS}
                >
                  RECIENTES
                </ToggleGroupItem>
                <ToggleGroupItem
                  value="top-week"
                  aria-label="Top de la semana"
                  className={STUDIO_FILTER_CHIP_CLASS}
                >
                  TOP SEMANA
                </ToggleGroupItem>
              </ToggleGroup>
            </div>
          ) : null
        }
      />

      {content}
    </div>
  );
}

function FeedEmptyState() {
  return (
    <StudioEmptyState
      icon={Compass}
      title="Todavía no hay nada publicado"
      description="Sé el primero en publicar un highlight — tus reels aparecerán aquí para todos."
      accent="magenta"
      compact
      actions={
        <>
          <Button asChild className="font-[family-name:var(--font-display)] tracking-[0.06em]">
            <Link href="/videos">
              <Film aria-hidden />
              PUBLICAR UN REEL
            </Link>
          </Button>
          <Button
            asChild
            variant="outline"
            className="font-[family-name:var(--font-display)] tracking-[0.06em]"
          >
            <Link href="/upload">
              <UploadCloud aria-hidden />
              CREAR UN REEL
            </Link>
          </Button>
        </>
      }
    />
  );
}
