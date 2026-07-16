// Spanish preset descriptions for the reel style picker.
//
// The render preset registry (internal/editor/preset.go, served through
// /api/presets) is the engineering source of truth and stays in English: it is
// shared by the CLI, the HTTP API, and Go tests. The desktop UI, however, is
// Spanish, so the picker would otherwise show English copy verbatim. Rather
// than translate the registry, we localise at the presentation layer: this map
// overrides each preset's description by NAME, and unknown presets fall back to
// whatever the API returned. Keep technical terms (HUD, POV, kill feed,
// punch-in) as-is; they read the same in both languages.

/**
 * Faithful Spanish translations of the registry descriptions, keyed by preset
 * name (the stable identifier, not the label). Mirror preset.go: when a
 * registry description changes, update the matching entry here.
 */
export const PRESET_DESCRIPTION_ES: Record<string, string> = {
  'viral-60-clean':
    'Edición viral limpia por defecto: POV a 60fps sin HUD que conserva el kill feed del juego, con punch-in en las bajas.',
  'clean-pov-60':
    'POV en primera persona totalmente sin HUD: punch-in cinematográfico en las bajas, sin HUD ni kill feed del juego.',
  'full-hud-60':
    'POV con el HUD completo del juego: mantiene visibles el HUD de CS2, la vida, la munición y el radar sobre la edición viral.',
};

/**
 * The Spanish description for a preset: the localized override when the preset
 * name is known, otherwise the description the API supplied (English) so an
 * unrecognised preset still shows meaningful copy instead of a blank.
 */
export function presetDescription(preset: { name: string; description: string }): string {
  return PRESET_DESCRIPTION_ES[preset.name] ?? preset.description;
}
