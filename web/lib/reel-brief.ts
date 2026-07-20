import type { EditConfig, Preset } from './api/types';

export type CreativeBriefItem = {
  label: string;
  value: string;
};

export function canForgeReel({
  briefApproved,
  creating,
  hasPreset,
  selectionCount,
}: {
  briefApproved: boolean;
  creating: boolean;
  hasPreset: boolean;
  selectionCount: number;
}): boolean {
  return !creating && briefApproved && hasPreset && selectionCount > 0;
}

const FORMAT_LABEL: Record<EditConfig['format'], string> = {
  'short-9x16': 'Vertical 9:16 · 1080×1920',
  'landscape-16x9': 'Horizontal 16:9 · 1920×1080',
};

const EFFECT_LABEL: Record<EditConfig['killEffect'], string> = {
  clean: 'Limpio',
  'punch-in': 'Impacto / punch-in',
  velocity: 'Velocidad',
  'freeze-flash': 'Congelado con flash',
};

const TRANSITION_LABEL: Record<EditConfig['transition'], string> = {
  cut: 'Corte',
  flash: 'Destello',
  whip: 'Barrido',
  dip: 'Fundido',
};

const HUD_LABEL: Record<string, string> = {
  deathnotices: 'Sin HUD, conserva killfeed',
  clean: 'Sin HUD ni killfeed',
  gameplay: 'HUD completo con killfeed',
};

function bookendLabel(enabled: boolean, text: string | undefined, generatedFallback: string): string {
  if (!enabled) return 'No';
  return text?.trim() ? `Sí · “${text.trim()}”` : `Sí · ${generatedFallback}`;
}

/** Exact, reviewable values that must be approved before capture or render. */
export function reelCreativeBrief(
  edit: EditConfig,
  preset: Preset | null,
  songTitle: string | null,
  musicVolumePercent: number,
): CreativeBriefItem[] {
  const hud = preset?.hudMode ? (HUD_LABEL[preset.hudMode] ?? `Modo ${preset.hudMode}`) : 'Pendiente de preset';
  return [
    { label: 'Formato', value: FORMAT_LABEL[edit.format] },
    { label: 'HUD / killfeed', value: hud },
    { label: 'Efecto de kill', value: EFFECT_LABEL[edit.killEffect] },
    { label: 'Transición', value: TRANSITION_LABEL[edit.transition] },
    { label: 'Título / contador', value: `${edit.hookText ? 'Título automático' : 'Sin título automático'} · ${edit.killCounter ? 'Contador activado' : 'Sin contador'}` },
    { label: 'Intro', value: bookendLabel(edit.intro, edit.introText, 'titular generado') },
    { label: 'Outro', value: bookendLabel(edit.outro, edit.outroText, 'firma FragForge') },
    { label: 'Música', value: songTitle ? `${songTitle} · ${musicVolumePercent}%` : 'Sin música' },
    {
      label: 'Portada',
      value: edit.coverStrategy === 'generated-gameplay'
        ? 'Generar candidatos de gameplay para revisión'
        : 'No generar portada',
    },
  ];
}
