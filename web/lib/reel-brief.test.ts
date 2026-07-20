import assert from 'node:assert/strict';
import test from 'node:test';
import { canForgeReel, reelCreativeBrief } from './reel-brief.ts';
import type { EditConfig, Preset } from './api/types.ts';

const PRESET: Preset = {
  name: 'viral-60-clean',
  label: 'Kill Feed',
  description: 'test',
  hudMode: 'deathnotices',
};

test('forging stays blocked until the exact brief is approved', () => {
  const ready = { briefApproved: true, creating: false, hasPreset: true, selectionCount: 1 };
  assert.equal(canForgeReel(ready), true);
  assert.equal(canForgeReel({ ...ready, briefApproved: false }), false);
  assert.equal(canForgeReel({ ...ready, creating: true }), false);
  assert.equal(canForgeReel({ ...ready, hasPreset: false }), false);
  assert.equal(canForgeReel({ ...ready, selectionCount: 0 }), false);
});

test('creative brief resolves every required production choice', () => {
  const edit: EditConfig = {
    format: 'short-9x16',
    killEffect: 'punch-in',
    transition: 'flash',
    hookText: true,
    killCounter: true,
    coverStrategy: 'generated-gameplay',
    intro: true,
    introText: 'Entrada',
    outro: false,
    outroText: '',
  };

  assert.deepEqual(reelCreativeBrief(edit, PRESET, 'Tema CC0', 35), [
    { label: 'Formato', value: 'Vertical 9:16 · 1080×1920' },
    { label: 'HUD / killfeed', value: 'Sin HUD, conserva killfeed' },
    { label: 'Efecto de kill', value: 'Impacto / punch-in' },
    { label: 'Transición', value: 'Destello' },
    { label: 'Título / contador', value: 'Título automático · Contador activado' },
    { label: 'Intro', value: 'Sí · “Entrada”' },
    { label: 'Outro', value: 'No' },
    { label: 'Música', value: 'Tema CC0 · 35%' },
    { label: 'Portada', value: 'Generar candidatos de gameplay para revisión' },
  ]);
});

test('creative brief makes disabled options and missing preset explicit', () => {
  const edit: EditConfig = {
    format: 'landscape-16x9',
    killEffect: 'clean',
    transition: 'cut',
    hookText: false,
    killCounter: false,
    coverStrategy: 'no-cover',
    intro: false,
    outro: true,
    introText: '',
    outroText: '',
  };
  const brief = Object.fromEntries(reelCreativeBrief(edit, null, null, 100).map((item) => [item.label, item.value]));
  assert.equal(brief['Formato'], 'Horizontal 16:9 · 1920×1080');
  assert.equal(brief['HUD / killfeed'], 'Pendiente de preset');
  assert.equal(brief['Título / contador'], 'Sin título automático · Sin contador');
  assert.equal(brief['Intro'], 'No');
  assert.equal(brief['Outro'], 'Sí · firma FragForge');
  assert.equal(brief['Música'], 'Sin música');
  assert.equal(brief['Portada'], 'No generar portada');
});
