'use client';

import { Loader2, Sparkles } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { cn } from '@/lib/utils';

export type CreateReelBarProps = {
  /** Label of the selected play, or null when nothing is picked. */
  playLabel: string | null;
  /** Selected render mode, or null when none chosen. */
  mode: 'clean' | 'music' | null;
  /** Whether a render is in flight (spinner + disabled). */
  creating: boolean;
  onCreate: () => void;
};

/**
 * CreateReelBar — the sticky bottom action bar. Mirrors the current selection
 * (play + mode) on the left and offers the single lime "Create reel" CTA. For
 * Music Edit the CTA is deferred to the song picker, so it only fires here for
 * Clean POV; the bar still shows the chosen mode for context.
 */
export function CreateReelBar({ playLabel, mode, creating, onCreate }: CreateReelBarProps) {
  const ready = playLabel != null && mode != null;
  const modeLabel = mode === 'music' ? 'Music Edit' : mode === 'clean' ? 'Clean POV' : null;

  return (
    <div className="sticky bottom-0 z-20 -mx-4 mt-2 border-t border-border bg-background/85 px-4 py-3 backdrop-blur md:-mx-8 md:px-8">
      <div className="mx-auto flex max-w-[1200px] items-center justify-between gap-4">
        <div className="min-w-0 text-sm">
          {ready ? (
            <p className="truncate text-foreground">
              <span className="text-muted-foreground">Selected </span>
              <span className="font-medium">{playLabel}</span>
              <span className="text-muted-foreground"> · </span>
              <span className="font-[family-name:var(--font-mono)] uppercase tracking-wide text-primary">
                {modeLabel}
              </span>
            </p>
          ) : (
            <p className="truncate text-muted-foreground">
              {playLabel == null
                ? 'Pick a highlight to start.'
                : 'Choose a mode below.'}
            </p>
          )}
        </div>

        <Button
          type="button"
          size="lg"
          disabled={!ready || creating}
          onClick={onCreate}
          className={cn('shrink-0', creating && 'pointer-events-none')}
        >
          {creating ? <Loader2 className="animate-spin" /> : <Sparkles />}
          Create reel
        </Button>
      </div>
    </div>
  );
}
