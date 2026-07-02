'use client';

import { Monitor, PanelTop, Smartphone, Sparkles, Zap } from 'lucide-react';
import { BOOKEND_TEXT_MAX_LENGTH, type EditConfig } from '@/lib/api/types';
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group';
import { Input } from '@/components/ui/input';
import { cn } from '@/lib/utils';

/** Show the live character counter only once the input is getting close to the limit. */
const COUNTER_THRESHOLD = BOOKEND_TEXT_MAX_LENGTH - 20;

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

        <div className="grid gap-3 sm:grid-cols-2">
          <BookendTextField
            label="Intro title"
            value={value.introText ?? ''}
            visible={value.intro}
            placeholder="Intro title — leave empty to use the generated headline"
            disabled={disabled}
            onChange={(introText) => onChange({ ...value, introText })}
          />
          <BookendTextField
            label="Outro text"
            value={value.outroText ?? ''}
            visible={value.outro}
            placeholder="Outro text — e.g. your handle; empty = FragForge"
            disabled={disabled}
            onChange={(outroText) => onChange({ ...value, outroText })}
          />
        </div>
      </OptionBlock>
    </div>
  );
}

function BookendTextField({
  label,
  value,
  visible,
  placeholder,
  disabled,
  onChange,
}: {
  label: string;
  value: string;
  visible: boolean;
  placeholder: string;
  disabled: boolean;
  onChange: (value: string) => void;
}) {
  const near = value.length >= COUNTER_THRESHOLD;
  return (
    <div
      className={cn(
        'overflow-hidden transition-[max-height,opacity] duration-200 ease-out',
        visible ? 'max-h-24 opacity-100 visible' : 'max-h-0 opacity-0 invisible',
      )}
    >
      <div className="flex flex-col gap-1.5 pt-1">
        <div className="flex items-center justify-between gap-2">
          <span className="font-[family-name:var(--font-mono)] text-[10px] uppercase tracking-wide text-muted-foreground">
            {label}
          </span>
          {near ? (
            <span
              className={cn(
                'font-[family-name:var(--font-mono)] text-[10px] tabular-nums text-muted-foreground',
                value.length >= BOOKEND_TEXT_MAX_LENGTH && 'text-destructive',
              )}
            >
              {value.length}/{BOOKEND_TEXT_MAX_LENGTH}
            </span>
          ) : null}
        </div>
        <Input
          value={value}
          placeholder={placeholder}
          maxLength={BOOKEND_TEXT_MAX_LENGTH}
          disabled={disabled || !visible}
          tabIndex={visible ? 0 : -1}
          onChange={(e) => onChange(e.target.value)}
          aria-label={label}
        />
      </div>
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
