'use client';

import { Monitor, PanelTop, Smartphone, Sparkles, Zap } from 'lucide-react';
import type { EditConfig } from '@/lib/api/types';
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group';
import { cn } from '@/lib/utils';

export type EditOptionsProps = {
  value: EditConfig;
  onChange: (next: EditConfig) => void;
  disabled?: boolean;
};

const formatItems: Array<{ value: EditConfig['format']; label: string; icon: React.ReactNode }> = [
  { value: 'short-9x16', label: 'Short', icon: <Smartphone className="size-4" /> },
  { value: 'landscape-16x9', label: '16:9', icon: <Monitor className="size-4" /> },
];

const effectItems: Array<{ value: EditConfig['killEffect']; label: string }> = [
  { value: 'punch-in', label: 'Punch' },
  { value: 'clean', label: 'Clean' },
  { value: 'velocity', label: 'Velocity' },
  { value: 'freeze-flash', label: 'Freeze' },
];

const transitionItems: Array<{ value: EditConfig['transition']; label: string }> = [
  { value: 'flash', label: 'Flash' },
  { value: 'cut', label: 'Cut' },
  { value: 'whip', label: 'Whip' },
  { value: 'dip', label: 'Dip' },
];

export function EditOptions({ value, onChange, disabled = false }: EditOptionsProps) {
  return (
    <div className={cn('grid gap-4 md:grid-cols-[1fr_1fr_auto]', disabled && 'opacity-60')}>
      <OptionBlock label="Format">
        <ToggleGroup
          type="single"
          value={value.format}
          onValueChange={(format) => format && onChange({ ...value, format: format as EditConfig['format'] })}
          disabled={disabled}
          variant="outline"
          className="flex-wrap"
        >
          {formatItems.map((item) => (
            <ToggleGroupItem key={item.value} value={item.value} aria-label={item.label}>
              {item.icon}
              {item.label}
            </ToggleGroupItem>
          ))}
        </ToggleGroup>
      </OptionBlock>

      <OptionBlock label="Kill effect">
        <ToggleGroup
          type="single"
          value={value.killEffect}
          onValueChange={(killEffect) =>
            killEffect && onChange({ ...value, killEffect: killEffect as EditConfig['killEffect'] })
          }
          disabled={disabled}
          variant="outline"
          className="flex-wrap"
        >
          {effectItems.map((item) => (
            <ToggleGroupItem key={item.value} value={item.value} aria-label={item.label}>
              <Zap className="size-4" />
              {item.label}
            </ToggleGroupItem>
          ))}
        </ToggleGroup>
      </OptionBlock>

      <OptionBlock label="Transitions">
        <ToggleGroup
          type="single"
          value={value.transition}
          onValueChange={(transition) =>
            transition && onChange({ ...value, transition: transition as EditConfig['transition'] })
          }
          disabled={disabled}
          variant="outline"
          className="flex-wrap"
        >
          {transitionItems.map((item) => (
            <ToggleGroupItem key={item.value} value={item.value} aria-label={item.label}>
              <Sparkles className="size-4" />
              {item.label}
            </ToggleGroupItem>
          ))}
        </ToggleGroup>
      </OptionBlock>

      <OptionBlock label="Bookends" className="md:col-span-3">
        <ToggleGroup
          type="multiple"
          value={[value.intro ? 'intro' : '', value.outro ? 'outro' : ''].filter(Boolean)}
          onValueChange={(items) =>
            onChange({ ...value, intro: items.includes('intro'), outro: items.includes('outro') })
          }
          disabled={disabled}
          variant="outline"
          className="flex-wrap"
        >
          <ToggleGroupItem value="intro" aria-label="Intro">
            <PanelTop className="size-4" />
            Intro
          </ToggleGroupItem>
          <ToggleGroupItem value="outro" aria-label="Outro">
            <PanelTop className="size-4 rotate-180" />
            Outro
          </ToggleGroupItem>
        </ToggleGroup>
      </OptionBlock>
    </div>
  );
}

function OptionBlock({ label, className, children }: { label: string; className?: string; children: React.ReactNode }) {
  return (
    <div className={cn('flex min-w-0 flex-col gap-2', className)}>
      <span className="font-[family-name:var(--font-mono)] text-[11px] uppercase tracking-wide text-muted-foreground">
        {label}
      </span>
      {children}
    </div>
  );
}
