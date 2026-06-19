'use client';

import { Loader2, Sparkles } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { cn } from '@/lib/utils';

export type CreateReelBarProps = {
  /** Label of the selected play, or null when nothing is picked. */
  playLabel: string | null;
  /** Label of the chosen preset, or null when none chosen. */
  presetLabel: string | null;
  /** Title of the chosen soundtrack, or null when the reel has no music. */
  songTitle: string | null;
  /** Whether a render is in flight (spinner + disabled). */
  creating: boolean;
  onCreate: () => void;
};

/**
 * CreateReelBar — the sticky bottom action bar. Mirrors the current selection
 * (play + preset + optional music) on the left and fires the single lime
 * "Create reel" CTA. Enabled once a play and a preset are chosen.
 */
export function CreateReelBar({ playLabel, presetLabel, songTitle, creating, onCreate }: CreateReelBarProps) {
  const ready = playLabel != null && presetLabel != null;

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
                {presetLabel}
              </span>
              {songTitle ? (
                <>
                  <span className="text-muted-foreground"> · </span>
                  <span className="text-muted-foreground">♪ {songTitle}</span>
                </>
              ) : null}
            </p>
          ) : (
            <p className="truncate text-muted-foreground">
              {playLabel == null ? 'Pick a highlight to start.' : 'Choose a preset below.'}
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
