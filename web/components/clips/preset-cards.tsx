'use client';

import { Clapperboard, Eye, Crosshair, Check } from 'lucide-react';
import type { Preset } from '@/lib/api/types';
import { presetDescription } from '@/lib/preset-copy';
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
 * active card carries the cyan ring and a filled check; the registry default is
 * flagged so the user knows the safe pick.
 */
export function PresetCards({ presets, value, onChange, disabled = false }: PresetCardsProps) {
  return (
    <div className="grid gap-4 sm:grid-cols-3">
      {presets.map((preset) => (
        <PresetCard
          key={preset.name}
          icon={PRESET_ICONS[preset.name] ?? <Clapperboard className="size-5" />}
          title={preset.label}
          pitch={presetDescription(preset)}
          hud={preset.hudMode}
          isDefault={Boolean(preset.default)}
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
  isDefault: boolean;
  selected: boolean;
  disabled: boolean;
  onSelect: () => void;
};

function PresetCard({ icon, title, pitch, hud, isDefault, selected, disabled, onSelect }: PresetCardProps) {
  return (
    <button
      type="button"
      onClick={onSelect}
      disabled={disabled}
      aria-pressed={selected}
      className={cn(
        'group relative flex flex-col items-start gap-3 border p-5 text-left transition-all',
        'disabled:cursor-not-allowed disabled:opacity-50',
        selected
          ? 'border-primary bg-primary/[0.06] ring-1 ring-primary'
          : 'border-border bg-card hover:border-muted-foreground/40 hover:bg-card/80',
      )}
    >
      <div className="flex w-full items-start justify-between">
        <span
          className={cn(
            'inline-flex size-10 items-center justify-center border transition-colors',
            selected ? 'border-primary/40 bg-primary/10 text-primary' : 'border-border bg-muted text-muted-foreground group-hover:text-foreground',
          )}
        >
          {icon}
        </span>
        <span
          className={cn(
            'flex size-5 items-center justify-center border transition-colors',
            selected ? 'border-primary bg-primary text-primary-foreground' : 'border-border bg-transparent text-transparent',
          )}
          aria-hidden
        >
          <Check className="size-3" />
        </span>
      </div>

      <div className="flex flex-wrap items-center gap-2">
        <span className="font-[family-name:var(--font-display)] text-lg font-bold uppercase tracking-tight text-foreground">
          {title}
        </span>
        {isDefault ? (
          <span className="border border-primary/30 bg-primary/10 px-1.5 py-0.5 font-[family-name:var(--font-mono)] text-[0.6rem] font-semibold uppercase tracking-wider text-primary">
            POR DEFECTO
          </span>
        ) : null}
      </div>

      <span className="text-sm leading-relaxed text-muted-foreground">{pitch}</span>

      {hud ? (
        <span className="mt-auto inline-flex items-center border border-border bg-muted/40 px-2 py-0.5 font-[family-name:var(--font-mono)] text-[0.65rem] uppercase tracking-wider text-muted-foreground">
          HUD · {hud}
        </span>
      ) : null}
    </button>
  );
}
