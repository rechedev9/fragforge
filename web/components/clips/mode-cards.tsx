'use client';

import { Clapperboard, Music } from 'lucide-react';
import { cn } from '@/lib/utils';

export type RenderModeChoice = 'clean' | 'music';

export type ModeCardsProps = {
  /** Currently chosen mode, or null when nothing is picked yet. */
  value: RenderModeChoice | null;
  onChange: (mode: RenderModeChoice) => void;
  /** Disable interaction (e.g. no play selected, or a render is in flight). */
  disabled?: boolean;
};

/**
 * ModeCards — the two large render-mode choices below the filmstrip. Clean POV
 * is the raw highlight; Music Edit adds a soundtrack (and opens the song
 * picker). The active card carries the lime selection ring.
 */
export function ModeCards({ value, onChange, disabled = false }: ModeCardsProps) {
  return (
    <div className="grid gap-4 sm:grid-cols-2">
      <ModeCard
        icon={<Clapperboard className="size-5" />}
        title="Clean POV"
        pitch="Your raw highlight, no music. Pure aim, ready to post."
        selected={value === 'clean'}
        disabled={disabled}
        onSelect={() => onChange('clean')}
      />
      <ModeCard
        icon={<Music className="size-5" />}
        title="Music Edit"
        pitch="Sync the action to a track. Pick a song next."
        selected={value === 'music'}
        disabled={disabled}
        onSelect={() => onChange('music')}
      />
    </div>
  );
}

type ModeCardProps = {
  icon: React.ReactNode;
  title: string;
  pitch: string;
  selected: boolean;
  disabled: boolean;
  onSelect: () => void;
};

function ModeCard({ icon, title, pitch, selected, disabled, onSelect }: ModeCardProps) {
  return (
    <button
      type="button"
      onClick={onSelect}
      disabled={disabled}
      aria-pressed={selected}
      className={cn(
        'flex flex-col items-start gap-3 rounded-xl border bg-card p-5 text-left transition-all',
        'disabled:cursor-not-allowed disabled:opacity-50',
        selected
          ? 'border-primary ring-2 ring-primary'
          : 'border-border hover:border-muted-foreground/40',
      )}
    >
      <span
        className={cn(
          'inline-flex size-10 items-center justify-center rounded-lg border transition-colors',
          selected
            ? 'border-primary/40 bg-primary/10 text-primary'
            : 'border-border bg-muted text-muted-foreground',
        )}
      >
        {icon}
      </span>
      <span className="font-[family-name:var(--font-display)] text-lg font-semibold tracking-tight text-foreground">
        {title}
      </span>
      <span className="text-sm text-muted-foreground">{pitch}</span>
    </button>
  );
}
