import type { LucideIcon } from 'lucide-react';
import type { ReactNode } from 'react';
import { cn } from '@/lib/utils';

export type StudioEmptyStateProps = {
  icon: LucideIcon;
  title: string;
  description: ReactNode;
  actions?: ReactNode;
  note?: ReactNode;
  accent?: 'cyan' | 'magenta';
  compact?: boolean;
  className?: string;
};

/** Actionable, bounded empty state shared by dashboard destinations. */
export function StudioEmptyState({
  icon: Icon,
  title,
  description,
  actions,
  note,
  accent = 'cyan',
  compact = false,
  className,
}: StudioEmptyStateProps): ReactNode {
  const magenta = accent === 'magenta';

  return (
    <section
      aria-label={title}
      className={cn(
        'studio-panel studio-panel-raised flex w-full max-w-3xl flex-col items-center px-6 text-center sm:px-10',
        compact ? 'py-10' : 'py-14 sm:py-16',
        className,
      )}
    >
      <span
        className={cn(
          'grid size-12 place-items-center rounded-lg border bg-background/55 shadow-inner',
          magenta ? 'border-stream/30 text-stream' : 'border-primary/30 text-primary',
        )}
      >
        <Icon className="size-5" aria-hidden />
      </span>
      <h2 className="mt-5 font-[family-name:var(--font-display)] text-xl font-bold uppercase tracking-tight text-foreground">
        {title}
      </h2>
      <div className="mt-2 max-w-xl text-[15px] leading-6 text-muted-foreground">{description}</div>
      {actions ? <div className="mt-7 flex w-full flex-col justify-center gap-3 sm:w-auto sm:flex-row">{actions}</div> : null}
      {note ? (
        <div className="mt-6 border-t border-border/70 pt-4 font-[family-name:var(--font-mono)] text-xs uppercase tracking-[0.12em] text-muted-foreground">
          {note}
        </div>
      ) : null}
    </section>
  );
}
