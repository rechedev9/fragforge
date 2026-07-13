'use client';

import { type ReactNode } from 'react';
import { Loader2, Plus, Sparkles, Trash2 } from 'lucide-react';
import { KILLFEED_SIDES, type KillfeedKill, type KillfeedSide } from '@/lib/api/streams';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group';
import { cn } from '@/lib/utils';

/** Boolean kill-notice flags shown as multi-select chips, with Spanish labels. */
type KillfeedFlagKey = 'headshot' | 'wallbang' | 'noscope' | 'smoke' | 'blind' | 'in_air';
const KILL_FLAGS: readonly { key: KillfeedFlagKey; label: string }[] = [
  { key: 'headshot', label: 'Headshot' },
  { key: 'wallbang', label: 'Wallbang' },
  { key: 'noscope', label: 'Noscope' },
  { key: 'smoke', label: 'Humo' },
  { key: 'blind', label: 'A ciegas' },
  { key: 'in_air', label: 'En el aire' },
];

function newKill(weapons: string[]): KillfeedKill {
  return {
    attacker_side: 'CT',
    attacker_name: '',
    victim_side: 'T',
    victim_name: '',
    weapon: weapons[0] ?? '',
  };
}

function SideToggle({
  value,
  label,
  disabled,
  onChange,
}: {
  value: KillfeedSide;
  label: string;
  disabled: boolean;
  onChange: (side: KillfeedSide) => void;
}) {
  return (
    <ToggleGroup
      type="single"
      variant="outline"
      size="sm"
      value={value}
      disabled={disabled}
      aria-label={label}
      onValueChange={(next) => {
        if (next === 'CT' || next === 'T') onChange(next);
      }}
    >
      {KILLFEED_SIDES.map((side) => (
        <ToggleGroupItem
          key={side}
          value={side}
          className={cn(
            'text-xs data-[state=on]:text-white',
            side === 'CT'
              ? 'data-[state=on]:border-sky-500 data-[state=on]:bg-sky-500'
              : 'data-[state=on]:border-amber-500 data-[state=on]:bg-amber-500',
          )}
        >
          {side}
        </ToggleGroupItem>
      ))}
    </ToggleGroup>
  );
}

/**
 * Per-cue editor for the confirmed kills rendered as synthetic notices. Rows are
 * fully editable, and "Leer con IA" prefills them from the cue frame (still
 * editable afterward). Kills stay index-aligned with the cue by the parent.
 */
export function KillfeedKillsEditor({
  kills,
  weapons,
  reading,
  readError,
  disabled,
  onChange,
  onReadWithAI,
}: {
  kills: KillfeedKill[];
  weapons: string[];
  reading: boolean;
  readError: string | null;
  disabled: boolean;
  onChange: (kills: KillfeedKill[]) => void;
  onReadWithAI: () => void;
}): ReactNode {
  const updateKill = (index: number, patch: Partial<KillfeedKill>) =>
    onChange(kills.map((kill, i) => (i === index ? { ...kill, ...patch } : kill)));

  const setAssisterName = (index: number, rawName: string) => {
    const name = rawName;
    if (name.trim() === '') {
      onChange(
        kills.map((kill, i) => {
          if (i !== index) return kill;
          const { assister_name: _n, assister_side: _s, flash_assist: _f, ...rest } = kill;
          return rest;
        }),
      );
      return;
    }
    updateKill(index, { assister_name: name, assister_side: kills[index].assister_side ?? 'CT' });
  };

  const removeKill = (index: number) => onChange(kills.filter((_kill, i) => i !== index));
  const addKill = () => onChange([...kills, newKill(weapons)]);

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center gap-2">
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={disabled || reading}
          onClick={onReadWithAI}
          className="border-stream/60 text-stream hover:border-stream hover:bg-stream/10 focus-visible:ring-stream"
        >
          {reading ? <Loader2 className="size-4 animate-spin" aria-hidden /> : <Sparkles className="size-4" aria-hidden />}
          Leer con IA
        </Button>
        <Button
          type="button"
          variant="ghost"
          size="sm"
          disabled={disabled || reading}
          onClick={addKill}
          className="text-muted-foreground hover:text-foreground"
        >
          <Plus className="size-4" aria-hidden />
          Añadir kill
        </Button>
        {reading ? (
          <span className="font-[family-name:var(--font-mono)] text-xs text-muted-foreground">Leyendo el fotograma…</span>
        ) : null}
      </div>

      {readError ? (
        <p role="alert" className="text-xs leading-relaxed text-destructive">
          {readError}
        </p>
      ) : null}

      {kills.length === 0 ? (
        <p className="text-xs text-muted-foreground">
          Sin kills en esta marca: se congelará el recorte del MP4. Añade kills o léelas con IA para superponer avisos sintéticos.
        </p>
      ) : (
        <ul className="flex flex-col gap-3">
          {kills.map((kill, index) => (
            <li key={index} className="flex flex-col gap-3 border border-border bg-background/40 p-3">
              <div className="flex flex-wrap items-end gap-3">
                <div className="flex min-w-40 flex-1 flex-col gap-1">
                  <Label htmlFor={`kill-${index}-attacker`} className="text-xs text-muted-foreground">
                    Atacante
                  </Label>
                  <Input
                    id={`kill-${index}-attacker`}
                    value={kill.attacker_name}
                    disabled={disabled}
                    onChange={(event) => updateKill(index, { attacker_name: event.target.value })}
                    placeholder="Nombre"
                  />
                </div>
                <SideToggle
                  value={kill.attacker_side}
                  label={`Bando del atacante de la kill ${index + 1}`}
                  disabled={disabled}
                  onChange={(side) => updateKill(index, { attacker_side: side })}
                />
                <div className="flex min-w-40 flex-1 flex-col gap-1">
                  <Label htmlFor={`kill-${index}-victim`} className="text-xs text-muted-foreground">
                    Víctima
                  </Label>
                  <Input
                    id={`kill-${index}-victim`}
                    value={kill.victim_name}
                    disabled={disabled}
                    onChange={(event) => updateKill(index, { victim_name: event.target.value })}
                    placeholder="Nombre"
                  />
                </div>
                <SideToggle
                  value={kill.victim_side}
                  label={`Bando de la víctima de la kill ${index + 1}`}
                  disabled={disabled}
                  onChange={(side) => updateKill(index, { victim_side: side })}
                />
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-sm"
                  disabled={disabled}
                  onClick={() => removeKill(index)}
                  aria-label={`Eliminar la kill ${index + 1}`}
                >
                  <Trash2 className="size-4" aria-hidden />
                </Button>
              </div>

              <div className="flex flex-wrap items-end gap-3">
                <div className="flex flex-col gap-1">
                  <Label className="text-xs text-muted-foreground">Arma</Label>
                  <Select
                    value={kill.weapon || undefined}
                    disabled={disabled || weapons.length === 0}
                    onValueChange={(weapon) => updateKill(index, { weapon })}
                  >
                    <SelectTrigger aria-label={`Arma de la kill ${index + 1}`} className="w-44">
                      <SelectValue placeholder="Selecciona un arma" />
                    </SelectTrigger>
                    <SelectContent>
                      {weapons.map((weapon) => (
                        <SelectItem key={weapon} value={weapon}>
                          {weapon}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
                <div className="flex min-w-40 flex-1 flex-col gap-1">
                  <Label htmlFor={`kill-${index}-assister`} className="text-xs text-muted-foreground">
                    Asistencia (opcional)
                  </Label>
                  <Input
                    id={`kill-${index}-assister`}
                    value={kill.assister_name ?? ''}
                    disabled={disabled}
                    onChange={(event) => setAssisterName(index, event.target.value)}
                    placeholder="Nombre"
                  />
                </div>
                {kill.assister_name && kill.assister_name.trim() !== '' ? (
                  <>
                    <SideToggle
                      value={kill.assister_side ?? 'CT'}
                      label={`Bando de la asistencia de la kill ${index + 1}`}
                      disabled={disabled}
                      onChange={(side) => updateKill(index, { assister_side: side })}
                    />
                    <Button
                      type="button"
                      variant={kill.flash_assist ? 'default' : 'outline'}
                      size="sm"
                      disabled={disabled}
                      aria-pressed={kill.flash_assist ?? false}
                      onClick={() => updateKill(index, { flash_assist: !(kill.flash_assist ?? false) })}
                      className="text-xs"
                    >
                      Flash assist
                    </Button>
                  </>
                ) : null}
              </div>

              <ToggleGroup
                type="multiple"
                variant="outline"
                size="sm"
                spacing={2}
                disabled={disabled}
                aria-label={`Etiquetas de la kill ${index + 1}`}
                value={KILL_FLAGS.filter((flag) => kill[flag.key]).map((flag) => flag.key)}
                onValueChange={(active) => {
                  const patch: Partial<KillfeedKill> = {};
                  for (const flag of KILL_FLAGS) patch[flag.key] = active.includes(flag.key);
                  updateKill(index, patch);
                }}
                className="flex-wrap"
              >
                {KILL_FLAGS.map((flag) => (
                  <ToggleGroupItem key={flag.key} value={flag.key} className="rounded-md text-xs">
                    {flag.label}
                  </ToggleGroupItem>
                ))}
              </ToggleGroup>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
