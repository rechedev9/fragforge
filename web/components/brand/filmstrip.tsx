import type { ReactNode } from 'react';
import { ScrollArea, ScrollBar } from '@/components/ui/scroll-area';
import { cn } from '@/lib/utils';

export type FilmstripProps = {
  /** The play tiles (or any horizontal items) to lay out in a row. */
  children: ReactNode;
  className?: string;
  /** Class applied to the inner flex row (e.g. gap/padding overrides). */
  rowClassName?: string;
};

/**
 * Filmstrip — a horizontal ScrollArea row for selectable play tiles. The
 * filmstrip selector (instead of a vertical card list) is part of the v2
 * identity. Callers drop tiles in as children; this owns the scroll/overflow.
 */
export function Filmstrip({ children, className, rowClassName }: FilmstripProps) {
  return (
    <ScrollArea className={cn('w-full whitespace-nowrap', className)}>
      <div className={cn('flex w-max gap-3 pb-3', rowClassName)}>{children}</div>
      <ScrollBar orientation="horizontal" />
    </ScrollArea>
  );
}
