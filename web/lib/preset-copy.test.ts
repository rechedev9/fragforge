// Unit tests for the Spanish preset description overrides.
// Run: node --test preset-copy.test.ts
import test from 'node:test';
import assert from 'node:assert/strict';
import { PRESET_DESCRIPTION_ES, presetDescription } from './preset-copy.ts';

// The preset names registered in internal/editor/preset.go. Every override must
// key on one of these; a stray key means a typo or a renamed/removed preset.
const KNOWN_PRESET_NAMES = ['viral-60-clean', 'clean-pov-60', 'full-hud-60'] as const;

// The exact English descriptions from the Go registry (internal/editor/preset.go).
// Hardcoded as the guard so an override that was never translated (left equal to
// the API string) fails the test.
const ENGLISH_DESCRIPTIONS: Record<(typeof KNOWN_PRESET_NAMES)[number], string> = {
  'viral-60-clean':
    'default clean viral edit: HUD-less 60fps POV that keeps the in-game kill feed, with punch-in kills',
  'clean-pov-60':
    'fully HUD-less first-person POV: cinematic punch-in kills, no in-game HUD or kill feed',
  'full-hud-60':
    'full in-game HUD POV: keeps the CS2 HUD, health, ammo, and radar visible over the viral edit',
};

test('every override key is a known registry preset name', () => {
  const known = new Set<string>(KNOWN_PRESET_NAMES);
  for (const name of Object.keys(PRESET_DESCRIPTION_ES)) {
    assert.ok(known.has(name), `unexpected preset key: ${name}`);
  }
});

test('no override value is empty or left as the English source', () => {
  for (const name of KNOWN_PRESET_NAMES) {
    const value = PRESET_DESCRIPTION_ES[name];
    assert.ok(value && value.trim().length > 0, `empty override for ${name}`);
    assert.notEqual(value, ENGLISH_DESCRIPTIONS[name], `override for ${name} is still English`);
  }
});

test('presetDescription returns the Spanish override for a known preset', () => {
  const preset = { name: 'viral-60-clean', description: ENGLISH_DESCRIPTIONS['viral-60-clean'] };
  assert.equal(presetDescription(preset), PRESET_DESCRIPTION_ES['viral-60-clean']);
  assert.notEqual(presetDescription(preset), preset.description);
});

test('presetDescription falls back to the API description for an unknown preset', () => {
  const preset = { name: 'some-future-preset', description: 'brand new registry copy' };
  assert.equal(presetDescription(preset), 'brand new registry copy');
});
