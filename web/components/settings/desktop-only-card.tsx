import type { ReactNode } from 'react';
import { WifiOff } from 'lucide-react';

/**
 * The shared "this feature lives in the desktop app" card for /settings.
 * Each desktop-only feature keeps its own descriptive title (so the two cards
 * on the page stay distinguishable) over a common eyebrow line naming where
 * the feature is available; the body explains what the desktop app adds.
 */
export function DesktopOnlyCard({ titleId, title, children }: { titleId: string; title: string; children: ReactNode }): ReactNode {
  return (
    <section className="studio-panel max-w-3xl p-6" aria-labelledby={titleId}>
      <div className="flex gap-4">
        <WifiOff className="mt-0.5 size-6 shrink-0 text-muted-foreground" aria-hidden />
        <div>
          <p className="font-[family-name:var(--font-mono)] text-[11px] uppercase tracking-[0.14em] text-muted-foreground">
            Disponible en FragForge Studio para Windows
          </p>
          <h2 id={titleId} className="mt-1 font-[family-name:var(--font-display)] text-xl font-semibold text-foreground">
            {title}
          </h2>
          <p className="mt-2 text-sm leading-6 text-muted-foreground">{children}</p>
        </div>
      </div>
    </section>
  );
}
