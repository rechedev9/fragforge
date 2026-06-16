import { Card } from '@/components/ui/card';
import { Skeleton } from '@/components/ui/skeleton';

/** Loading placeholder mirroring the scoreboard row layout. */
export function MatchListSkeleton() {
  return (
    <div className="flex flex-col gap-3">
      {Array.from({ length: 5 }).map((_, i) => (
        <Card
          key={i}
          className="flex flex-row items-stretch gap-0 overflow-hidden py-0"
        >
          <Skeleton className="w-1 rounded-none" />
          <div className="flex flex-1 flex-col gap-4 p-4 sm:flex-row sm:items-center sm:justify-between">
            <div className="flex flex-1 flex-col gap-3">
              <div className="flex items-center gap-3">
                <Skeleton className="h-5 w-28" />
                <Skeleton className="h-5 w-12 rounded-full" />
                <Skeleton className="h-5 w-20 rounded-full" />
              </div>
              <div className="flex gap-6">
                {Array.from({ length: 5 }).map((__, j) => (
                  <div key={j} className="flex flex-col gap-1.5">
                    <Skeleton className="h-3 w-6" />
                    <Skeleton className="h-4 w-8" />
                  </div>
                ))}
              </div>
            </div>
            <Skeleton className="h-9 w-36 rounded-md" />
          </div>
        </Card>
      ))}
    </div>
  );
}
