'use client';

import { Loader2 } from 'lucide-react';
import type { RenderFormat } from '@/lib/api/types';
import { cn } from '@/lib/utils';

export type CreateReelBarProps = {
  /**
   * Selection summary, or null when nothing is picked. One highlight reuses
   * its own label ("1K · Ronda 1"); 2+ summarize as a count plus rounds
   * ("3 jugadas · Rondas 1, 6, 9") — see lib/format#playsSelectionLabel.
   */
  selectionLabel: string | null;
  /** Label of the chosen preset, or null when none chosen. */
  presetLabel: string | null;
  /** Title of the chosen soundtrack, or null when the reel has no music. */
  songTitle: string | null;
  /** Reel aspect (the mockup's 9:16 / 16:9 segmented toggle). */
  format: RenderFormat;
  onFormatChange: (format: RenderFormat) => void;
  /** Whether a render is in flight (spinner + disabled). */
  creating: boolean;
  onCreate: () => void;
};

const FORMAT_ITEMS: Array<{ value: RenderFormat; label: string }> = [
  { value: 'short-9x16', label: '9:16' },
  { value: 'landscape-16x9', label: '16:9' },
];

/**
 * CreateReelBar — the sticky bottom action bar, NEON HUD style: the mono REEL
 * summary on the left (selection + preset + optional music), the square
 * 9:16/16:9 aspect toggle, and the notched cyan FORJAR REEL CTA on the right.
 * Enabled once at least one highlight and a preset are chosen; 2+ selected
 * highlights render as one concatenated reel.
 */
export function CreateReelBar({
  selectionLabel,
  presetLabel,
  songTitle,
  format,
  onFormatChange,
  creating,
  onCreate,
}: CreateReelBarProps) {
  const ready = selectionLabel != null && presetLabel != null;

  return (
    <div className="sticky bottom-0 z-20 -mx-4 mt-2 border-t border-primary/20 bg-background/90 px-4 py-3 backdrop-blur md:-mx-8 md:px-8">
      <div className="mx-auto flex max-w-[1200px] flex-wrap items-center justify-between gap-x-6 gap-y-3">
        <div className="min-w-0">
          <p className="font-[family-name:var(--font-mono)] text-[9.5px] tracking-[0.2em] text-muted-foreground/70">
            REEL
          </p>
          {ready ? (
            <p className="truncate font-[family-name:var(--font-mono)] text-[15px] uppercase text-foreground">
              {selectionLabel}
              <span className="text-muted-foreground/70"> · </span>
              <span className="text-primary">{presetLabel}</span>
              {songTitle ? (
                <span className="text-muted-foreground"> · ♪ {songTitle}</span>
              ) : null}
            </p>
          ) : (
            <p className="truncate text-sm text-muted-foreground">
              {selectionLabel == null
                ? 'Elige al menos una jugada para empezar.'
                : 'Elige un preset abajo.'}
            </p>
          )}
        </div>

        <div className="flex font-[family-name:var(--font-mono)] text-[11px] tracking-[0.12em]">
          {FORMAT_ITEMS.map((item) => (
            <button
              key={item.value}
              type="button"
              aria-pressed={format === item.value}
              disabled={creating}
              onClick={() => onFormatChange(item.value)}
              className={cn(
                'px-4 py-2 transition-colors',
                format === item.value
                  ? 'bg-primary text-primary-foreground'
                  : 'border border-primary/30 text-muted-foreground hover:text-foreground',
              )}
            >
              {item.label}
            </button>
          ))}
        </div>

        <button
          type="button"
          disabled={!ready || creating}
          onClick={onCreate}
          className={cn(
            'neon-notch inline-flex shrink-0 items-center gap-2 bg-primary px-8 py-3 font-[family-name:var(--font-display)] text-[15px] font-bold tracking-[0.06em] text-primary-foreground transition-colors',
            !ready || creating ? 'opacity-50' : 'neon-glow hover:bg-primary/90',
            creating && 'pointer-events-none',
          )}
        >
          {creating ? <Loader2 className="size-4 animate-spin" /> : null}
          FORJAR REEL
        </button>
      </div>
    </div>
  );
}
