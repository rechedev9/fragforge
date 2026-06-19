'use client';

import { Clapperboard, Eye, Crosshair } from 'lucide-react';
import type { Preset } from '@/lib/api/types';
import { cn } from '@/lib/utils';

export type PresetCardsProps = {
  presets: Preset[];
  /** Chosen preset name (== render variant), or null when none is picked. */
  value: string | null;
  onChange: (variant: string) => void;
  /** Disable interaction (e.g. no play selected, or a render is in flight). */
  disabled?: boolean;
};

const PRESET_ICONS: Record<string, React.ReactNode> = {
  'clean-pov-60': <Eye className="size-5" />,
  'viral-60-clean': <Crosshair className="size-5" />,
  'full-hud-60': <Clapperboard className="size-5" />,
};

/**
 * PresetCards — the reel style picker. Each preset is one choice that sets both
 * the recording HUD (Clean POV vs Full HUD vs Kill Feed) and the render style;
 * the list comes from the orchestrator's preset registry (/api/presets). The
 * active card carries the lime selection ring.
 */
export function PresetCards({ presets, value, onChange, disabled = false }: PresetCardsProps) {
  return (
    <div className="grid gap-4 sm:grid-cols-3">
      {presets.map((preset) => (
        <PresetCard
          key={preset.name}
          icon={PRESET_ICONS[preset.name] ?? <Clapperboard className="size-5" />}
          title={preset.label}
          pitch={preset.description}
          hud={preset.hudMode}
          selected={value === preset.name}
          disabled={disabled}
          onSelect={() => onChange(preset.name)}
        />
      ))}
    </div>
  );
}

type PresetCardProps = {
  icon: React.ReactNode;
  title: string;
  pitch: string;
  hud?: string;
  selected: boolean;
  disabled: boolean;
  onSelect: () => void;
};

function PresetCard({ icon, title, pitch, hud, selected, disabled, onSelect }: PresetCardProps) {
  return (
    <button
      type="button"
      onClick={onSelect}
      disabled={disabled}
      aria-pressed={selected}
      className={cn(
        'flex flex-col items-start gap-3 rounded-xl border bg-card p-5 text-left transition-all',
        'disabled:cursor-not-allowed disabled:opacity-50',
        selected ? 'border-primary ring-2 ring-primary' : 'border-border hover:border-muted-foreground/40',
      )}
    >
      <span
        className={cn(
          'inline-flex size-10 items-center justify-center rounded-lg border transition-colors',
          selected ? 'border-primary/40 bg-primary/10 text-primary' : 'border-border bg-muted text-muted-foreground',
        )}
      >
        {icon}
      </span>
      <span className="font-[family-name:var(--font-display)] text-lg font-semibold tracking-tight text-foreground">
        {title}
      </span>
      <span className="text-sm text-muted-foreground">{pitch}</span>
      {hud ? (
        <span className="font-[family-name:var(--font-mono)] text-[11px] uppercase tracking-wide text-muted-foreground/70">
          HUD · {hud}
        </span>
      ) : null}
    </button>
  );
}
