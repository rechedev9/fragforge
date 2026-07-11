import { Skeleton } from '@/components/ui/skeleton';

/** Loading placeholder mirroring the scoreboard row layout. */
export function MatchListSkeleton() {
  return (
    <div className="flex flex-col gap-2.5">
      {Array.from({ length: 5 }).map((_, i) => (
        <div
          key={i}
          className="flex items-center gap-6 border border-primary/10 bg-card/75 px-5 py-4"
        >
          <Skeleton className="h-11 w-[3px] rounded-full" />
          <div className="flex w-[150px] flex-col gap-1.5">
            <Skeleton className="h-5 w-28" />
            <Skeleton className="h-3 w-24" />
          </div>
          <Skeleton className="h-5 w-16" />
          <div className="flex gap-7">
            {Array.from({ length: 5 }).map((__, j) => (
              <div key={j} className="flex flex-col gap-1.5">
                <Skeleton className="h-3 w-6" />
                <Skeleton className="h-4 w-8" />
              </div>
            ))}
          </div>
          <Skeleton className="ml-auto h-9 w-28" />
        </div>
      ))}
    </div>
  );
}
