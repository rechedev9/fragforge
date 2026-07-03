import { cn } from '@/lib/utils';

export type WordmarkProps = {
  className?: string;
  /** Hide the small magenta katakana line (for tight spots). Default on. */
  katakana?: boolean;
};

/**
 * The FragForge wordmark, NEON HUD style: FRAG//FORGE in Chakra Petch 700 with
 * the `//` in signal cyan, plus an optional katakana subline in magenta.
 * Renaming the product is a one-line change here.
 */
export function Wordmark({ className, katakana = true }: WordmarkProps) {
  return (
    <span className={cn('inline-flex flex-col', className)}>
      <span className="font-[family-name:var(--font-display)] text-lg font-bold tracking-[0.04em] text-foreground">
        FRAG
        <span className="text-primary">{'//'}</span>
        FORGE
      </span>
      {katakana ? (
        <span aria-hidden className="text-[10px] leading-tight tracking-[0.26em] text-destructive">
          フラグ・フォージ
        </span>
      ) : null}
    </span>
  );
}
