import test from 'node:test';
import assert from 'node:assert/strict';
import { addClipCue, normalizeClipKillfeed, normalizeKillfeedPlan, removeClipCue, setClipCueKills } from './killfeed-plan.ts';
import type { KillfeedKill, StreamClipRange, StreamEditPlan } from './api/streams.ts';

const KILL: KillfeedKill = {
  attacker_side: 'CT',
  attacker_name: 'hero',
  victim_side: 'T',
  victim_name: 'villain',
  weapon: 'ak47',
};

function clip(overrides: Partial<StreamClipRange> = {}): StreamClipRange {
  return { id: 'clip-1', start_seconds: 0, end_seconds: 20, ...overrides };
}

test('normalizeClipKillfeed sorts cues and keeps kills index-aligned', () => {
  const other: KillfeedKill = { ...KILL, attacker_name: 'second' };
  const normalized = normalizeClipKillfeed(
    clip({ killfeed_seconds: [12, 4], killfeed_kills: [[other], [KILL]] }),
  );
  assert.deepEqual(normalized.killfeed_seconds, [4, 12]);
  assert.deepEqual(normalized.killfeed_kills, [[KILL], [other]]);
});

test('normalizeClipKillfeed omits killfeed_kills when no cue has kills', () => {
  const normalized = normalizeClipKillfeed(
    clip({ killfeed_seconds: [4, 8], killfeed_kills: [[], []] }),
  );
  assert.deepEqual(normalized.killfeed_seconds, [4, 8]);
  assert.equal('killfeed_kills' in normalized, false);
});

test('normalizeClipKillfeed drops out-of-range cues with their kills', () => {
  const normalized = normalizeClipKillfeed(
    clip({ start_seconds: 5, end_seconds: 10, killfeed_seconds: [4, 7, 10], killfeed_kills: [[KILL], [KILL], [KILL]] }),
  );
  assert.deepEqual(normalized.killfeed_seconds, [7]);
  assert.deepEqual(normalized.killfeed_kills, [[KILL]]);
});

test('normalizeClipKillfeed dedupes cues, preferring the entry with kills', () => {
  const normalized = normalizeClipKillfeed(
    clip({ killfeed_seconds: [7, 7], killfeed_kills: [[], [KILL]] }),
  );
  assert.deepEqual(normalized.killfeed_seconds, [7]);
  assert.deepEqual(normalized.killfeed_kills, [[KILL]]);
});

test('normalizeKillfeedPlan strips cues and kills when there is no killfeed crop', () => {
  const plan: StreamEditPlan = {
    schema_version: '1.0',
    variant: 'streamer-vertical-stack-40-60',
    clips: [clip({ killfeed_seconds: [4], killfeed_kills: [[KILL]] })],
  };
  const normalized = normalizeKillfeedPlan(plan);
  assert.equal('killfeed_seconds' in normalized.clips[0], false);
  assert.equal('killfeed_kills' in normalized.clips[0], false);
});

test('setClipCueKills replaces only the targeted cue kills and stays aligned', () => {
  const base = clip({ killfeed_seconds: [4, 8], killfeed_kills: [[KILL], []] });
  const updated = setClipCueKills(base, 8, [KILL, KILL]);
  assert.deepEqual(updated.killfeed_seconds, [4, 8]);
  assert.deepEqual(updated.killfeed_kills, [[KILL], [KILL, KILL]]);
});

test('setClipCueKills is a no-op for an unknown cue', () => {
  const base = clip({ killfeed_seconds: [4], killfeed_kills: [[KILL]] });
  assert.equal(setClipCueKills(base, 99, []), base);
});

test('addClipCue inserts a cue and keeps kills aligned', () => {
  const base = clip({ killfeed_seconds: [8], killfeed_kills: [[KILL]] });
  const updated = addClipCue(base, 4);
  assert.deepEqual(updated.killfeed_seconds, [4, 8]);
  assert.deepEqual(updated.killfeed_kills, [[], [KILL]]);
});

test('addClipCue ignores a duplicate cue', () => {
  const base = clip({ killfeed_seconds: [4], killfeed_kills: [[KILL]] });
  const updated = addClipCue(base, 4);
  assert.deepEqual(updated.killfeed_seconds, [4]);
  assert.deepEqual(updated.killfeed_kills, [[KILL]]);
});

test('removeClipCue drops the cue and its aligned kills by index', () => {
  const other: KillfeedKill = { ...KILL, attacker_name: 'second' };
  const base = clip({ killfeed_seconds: [4, 8], killfeed_kills: [[KILL], [other]] });
  const updated = removeClipCue(base, 4);
  assert.deepEqual(updated.killfeed_seconds, [8]);
  assert.deepEqual(updated.killfeed_kills, [[other]]);
});
