import { cn } from '@/lib/utils';

export type WordmarkProps = {
  className?: string;
};

/**
 * The compact mark selected for FragForge's Japanese-inspired identity.
 * Its three parts stay tied to semantic brand tokens so it remains legible in
 * high-contrast themes without shipping a separate raster asset.
 */
export function BrandMark({ className }: { className?: string }) {
  return (
    <svg
      aria-hidden
      viewBox="0 0 64 64"
      className={cn('size-8 shrink-0', className)}
      fill="none"
    >
      <path className="fill-foreground" d="M8 8h46l-8 9H22v10h20l-8 9H22v16L8 62V8Z" />
      <path className="fill-primary" d="m20 57 31-35h9L29 57h-9Z" />
      <path className="fill-brand-accent" d="M50 49h8v8h-8z" />
    </svg>
  );
}

/** The full horizontal lockup used by the sidebar and standalone headers. */
export function Wordmark({ className }: WordmarkProps) {
  return (
    <span className={cn('inline-flex items-center gap-2.5', className)}>
      <BrandMark className="size-9" />
      <span className="font-[family-name:var(--font-display)] text-lg font-bold leading-none tracking-[0.11em] text-foreground">
        FRAGFORGE
      </span>
    </span>
  );
}
