import type { FeedItem } from '@/lib/api/types';
import { Skeleton } from '@/components/ui/skeleton';
import { FeedCard } from './feed-card';

export type FeedGridProps = {
  items: FeedItem[];
};

/** A responsive, uniform 16:9-thumbnail grid of community reels. */
export function FeedGrid({ items }: FeedGridProps) {
  return (
    <div
      className="grid grid-cols-1 gap-5 sm:grid-cols-2 xl:grid-cols-3"
      aria-label="Reels de la comunidad"
    >
      {items.map((item) => (
        <FeedCard key={item.id} item={item} />
      ))}
    </div>
  );
}

/** Loading placeholder mirroring the grid layout. */
export function FeedGridSkeleton() {
  return (
    <div
      className="grid grid-cols-1 gap-5 sm:grid-cols-2 xl:grid-cols-3"
      aria-label="Cargando reels"
    >
      {Array.from({ length: 6 }).map((_, i) => (
        <div key={i} className="studio-panel overflow-hidden">
          <Skeleton className="aspect-video w-full rounded-none" />
          <div className="flex flex-col gap-4 p-4">
            <div className="flex flex-col gap-2">
              <Skeleton className="h-5 w-3/4 rounded-none" />
              <Skeleton className="h-3 w-1/3 rounded-none" />
            </div>
            <div className="flex items-center justify-between gap-4">
              <Skeleton className="h-8 w-28 rounded-none" />
              <Skeleton className="h-11 w-20 rounded-none" />
            </div>
          </div>
        </div>
      ))}
    </div>
  );
}
