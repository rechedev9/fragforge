import type { FeedItem } from '@/lib/api/types';
import { Skeleton } from '@/components/ui/skeleton';
import { FeedCard } from './feed-card';

export type FeedGridProps = {
  items: FeedItem[];
};

/** A uniform 16:9-thumbnail grid of community reels, per the NEON HUD mockup
 * (a fixed grid, not a masonry — every card gets the same thumbnail height
 * regardless of the underlying render's aspect ratio). */
export function FeedGrid({ items }: FeedGridProps) {
  return (
    <div className="grid grid-cols-2 gap-4 sm:grid-cols-3">
      {items.map((item) => (
        <FeedCard key={item.id} item={item} />
      ))}
    </div>
  );
}

/** Loading placeholder mirroring the grid layout. */
export function FeedGridSkeleton() {
  return (
    <div className="grid grid-cols-2 gap-4 sm:grid-cols-3">
      {Array.from({ length: 6 }).map((_, i) => (
        <div key={i} className="border border-primary/14 bg-card/80">
          <Skeleton className="aspect-video w-full rounded-none" />
          <div className="flex flex-col gap-2 p-3.5">
            <Skeleton className="h-4 w-3/4 rounded-none" />
            <Skeleton className="h-3 w-1/3 rounded-none" />
          </div>
        </div>
      ))}
    </div>
  );
}
