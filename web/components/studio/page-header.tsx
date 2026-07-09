import type { ReactNode } from 'react';
import { SectionEyebrow } from '@/components/brand/section-eyebrow';
import { cn } from '@/lib/utils';

export type StudioPageHeaderProps = {
  number: number;
  label: string;
  title: string;
  description: ReactNode;
  accent?: 'cyan' | 'magenta';
  actions?: ReactNode;
  className?: string;
};

/** Consistent title block for every Studio destination. */
export function StudioPageHeader({
  number,
  label,
  title,
  description,
  accent = 'cyan',
  actions,
  className,
}: StudioPageHeaderProps): ReactNode {
  return (
    <header className={cn('flex flex-col gap-3', className)}>
      <SectionEyebrow number={number} label={label} accent={accent} />
      <div className="flex flex-col gap-4 lg:flex-row lg:items-end lg:justify-between lg:gap-8">
        <div className="min-w-0">
          <h1 className="font-[family-name:var(--font-display)] text-[2rem] font-bold leading-[1.05] tracking-[-0.025em] text-foreground sm:text-[2.5rem]">
            {title}
          </h1>
          <div className="mt-3 max-w-2xl text-[15px] leading-6 text-muted-foreground">{description}</div>
        </div>
        {actions ? <div className="shrink-0">{actions}</div> : null}
      </div>
    </header>
  );
}
