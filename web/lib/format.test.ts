// Unit tests for the visible-string formatters on the matches screens
// (Spanish NEON HUD skin). Run: node --test "lib/**/*.test.ts"
import test from 'node:test';
import assert from 'node:assert/strict';
import { timeAgo, playsSelectionLabel, formatKd } from './format.ts';
import type { Play } from './api/types.ts';

function play(overrides: Partial<Play>): Play {
  return {
    id: 'seg-001',
    matchId: 'job',
    label: '1K · Ronda 1',
    kind: 'highlight',
    round: 1,
    kills: 1,
    ...overrides,
  };
}

test('timeAgo: under a minute reads "ahora mismo"', () => {
  assert.equal(timeAgo(Date.now() - 5_000), 'ahora mismo');
});

test('timeAgo: minutes read "hace N min"', () => {
  assert.equal(timeAgo(Date.now() - 5 * 60_000), 'hace 5 min');
});

test('timeAgo: hours read "hace N h"', () => {
  assert.equal(timeAgo(Date.now() - 2 * 3_600_000), 'hace 2 h');
});

test('timeAgo: days read "hace N d"', () => {
  assert.equal(timeAgo(Date.now() - 3 * 86_400_000), 'hace 3 d');
});

test('playsSelectionLabel: empty selection is null', () => {
  assert.equal(playsSelectionLabel([]), null);
});

test('playsSelectionLabel: a single pick reuses its own label', () => {
  assert.equal(playsSelectionLabel([play({ label: '3K · Ronda 6', round: 6, kills: 3 })]), '3K · Ronda 6');
});

test('playsSelectionLabel: 2+ picks summarize count and sorted distinct rounds in Spanish', () => {
  const picks = [
    play({ id: 'a', label: '2K · Ronda 9', round: 9 }),
    play({ id: 'b', label: '1K · Ronda 1', round: 1 }),
    play({ id: 'c', label: '3K · Ronda 6', round: 6 }),
  ];
  assert.equal(playsSelectionLabel(picks), '3 jugadas · Rondas 1, 6, 9');
});

test('playsSelectionLabel: duplicate rounds collapse in the summary', () => {
  const picks = [
    play({ id: 'a', label: '1K · Ronda 6', round: 6 }),
    play({ id: 'b', label: '2K · Ronda 6', round: 6 }),
  ];
  assert.equal(playsSelectionLabel(picks), '2 jugadas · Rondas 6');
});

test('formatKd renders two decimals', () => {
  assert.equal(formatKd(2.2), '2.20');
});
