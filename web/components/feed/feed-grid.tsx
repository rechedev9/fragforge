import type { FeedItem } from '@/lib/api/types';
import { Skeleton } from '@/components/ui/skeleton';
import { FeedCard } from './feed-card';

export type FeedGridProps = {
  items: FeedItem[];
};

/**
 * Responsive portrait masonry of community reels. CSS columns let the 9:16
 * cards tile densely without a fixed row grid; cards opt out of column breaks.
 */
export function FeedGrid({ items }: FeedGridProps) {
  return (
    <div className="columns-2 gap-4 sm:columns-3 lg:columns-4 [&>*]:mb-4">
      {items.map((item) => (
        <FeedCard key={item.id} item={item} />
      ))}
    </div>
  );
}

/** Loading placeholder mirroring the masonry layout with staggered heights. */
export function FeedGridSkeleton() {
  const ratios = ['aspect-[9/16]', 'aspect-[9/14]', 'aspect-[9/15]', 'aspect-[3/5]'];
  return (
    <div className="columns-2 gap-4 sm:columns-3 lg:columns-4 [&>*]:mb-4">
      {Array.from({ length: 8 }).map((_, i) => (
        <div
          key={i}
          className="break-inside-avoid overflow-hidden rounded-xl border border-border bg-card"
        >
          <Skeleton className={`w-full rounded-none ${ratios[i % ratios.length]}`} />
          <div className="flex items-center justify-end px-3 py-2">
            <Skeleton className="h-4 w-10" />
          </div>
        </div>
      ))}
    </div>
  );
}
