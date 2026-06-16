import { Flame } from 'lucide-react';
import { cn } from '@/lib/utils';

export type WordmarkProps = {
  className?: string;
  /** Hide the spark/forge mark (text only). */
  hideMark?: boolean;
};

/**
 * The FragForge wordmark: lime "Frag" + white "Forge" with a small forge/spark
 * mark. Renaming the product is a one-line change here.
 */
export function Wordmark({ className, hideMark = false }: WordmarkProps) {
  return (
    <span className={cn('inline-flex items-center gap-2', className)}>
      {!hideMark ? (
        <span className="grid size-7 place-items-center rounded-md bg-primary text-primary-foreground">
          <Flame className="size-4" aria-hidden />
        </span>
      ) : null}
      <span className="font-[family-name:var(--font-display)] text-lg font-bold tracking-tight">
        <span className="text-primary">Frag</span>
        <span className="text-foreground">Forge</span>
      </span>
    </span>
  );
}
