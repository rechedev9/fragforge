import test from 'node:test';
import assert from 'node:assert/strict';
import { addClipCue, applyClipKillfeedRead, fitPlanToSourceDuration, initialStreamClipEnd, normalizeClipKillfeed, normalizeKillfeedPlan, removeClipCue, setClipCueKills } from './killfeed-plan.ts';
import type { KillfeedCueProvenance } from './killfeed-plan.ts';
import type { KillfeedKill, StreamClipRange, StreamEditPlan } from './api/streams.ts';

const KILL: KillfeedKill = {
  attacker_side: 'CT',
  attacker_name: 'hero',
  victim_side: 'T',
  victim_name: 'villain',
  weapon: 'ak47',
};

test('initialStreamClipEnd keeps the 20-second default while respecting short sources', () => {
  assert.equal(initialStreamClipEnd(15.15), 15.15);
  assert.equal(initialStreamClipEnd(120), 20);
  assert.equal(initialStreamClipEnd(0), 20);
  assert.equal(initialStreamClipEnd(Number.NaN), 20);
});

function clip(overrides: Partial<StreamClipRange> = {}): StreamClipRange {
  return { id: 'clip-1', start_seconds: 0, end_seconds: 20, ...overrides };
}

function cueProvenance(value: StreamClipRange): KillfeedCueProvenance[] | undefined {
  return (value as StreamClipRange & { killfeed_cue_provenance?: KillfeedCueProvenance[] })
    .killfeed_cue_provenance;
}

test('normalizeClipKillfeed sorts cues and keeps kills index-aligned', () => {
  const other: KillfeedKill = { ...KILL, attacker_name: 'second' };
  const normalized = normalizeClipKillfeed(
    clip({ killfeed_seconds: [12, 4], killfeed_kills: [[other], [KILL]] }),
  );
  assert.deepEqual(normalized.killfeed_seconds, [4, 12]);
  assert.deepEqual(normalized.killfeed_kills, [[KILL], [other]]);
});

test('normalizeClipKillfeed keeps provenance attached by exact cue and drops stale entries', () => {
  const normalized = normalizeClipKillfeed(clip({
    killfeed_seconds: [12, 4],
    ...({
      killfeed_cue_provenance: [
        { cue_seconds: 12, origin: 'automatic', event_id: 'event-12' },
        { cue_seconds: 4, origin: 'manual' },
        { cue_seconds: 9, origin: 'automatic', event_id: 'stale' },
      ],
    } as { killfeed_cue_provenance: KillfeedCueProvenance[] }),
  }));
  assert.deepEqual(cueProvenance(normalized), [
    { cue_seconds: 4, origin: 'manual' },
    { cue_seconds: 12, origin: 'automatic', event_id: 'event-12' },
  ]);
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

test('normalizeClipKillfeed merges unique kills from duplicate populated cues', () => {
  const other: KillfeedKill = { ...KILL, victim_name: 'second' };
  const normalized = normalizeClipKillfeed(
    clip({ killfeed_seconds: [7, 7, 7], killfeed_kills: [[KILL], [other], [KILL]] }),
  );
  assert.deepEqual(normalized.killfeed_seconds, [7]);
  assert.deepEqual(normalized.killfeed_kills, [[KILL, other]]);
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

test('normalizeKillfeedPlan migrates schema 1.0 cumulative snapshots to event deltas', () => {
  const second: KillfeedKill = { ...KILL, victim_name: 'second' };
  const third: KillfeedKill = { ...KILL, victim_name: 'third' };
  const plan: StreamEditPlan = {
    schema_version: '1.0',
    variant: 'streamer-vertical-stack-40-60',
    killfeed_crop: { x: 0.68, y: 0.04, width: 0.31, height: 0.14 },
    clips: [clip({
      killfeed_seconds: [2, 3, 4, 5],
      killfeed_kills: [[KILL], [], [KILL, second], [second, third]],
    })],
  };

  const normalized = normalizeKillfeedPlan(plan);

  assert.equal(normalized.schema_version, '1.1');
  assert.deepEqual(normalized.clips[0].killfeed_kills, [[KILL], [], [second], [third]]);
  assert.deepEqual(plan.clips[0].killfeed_kills, [[KILL], [], [KILL, second], [second, third]]);
});

test('fitPlanToSourceDuration clamps legacy ranges and aligned killfeed data to EOF', () => {
  const plan: StreamEditPlan = {
    schema_version: '1.0',
    variant: 'streamer-vertical-stack-40-60',
    killfeed_crop: { x: 0.68, y: 0.04, width: 0.31, height: 0.14 },
    clips: [
      clip({
        end_seconds: 20,
        killfeed_seconds: [8, 16],
        killfeed_kills: [[KILL], [{ ...KILL, victim_name: 'past-eof' }]],
      }),
    ],
  };

  const fitted = fitPlanToSourceDuration(plan, 15.15);
  assert.equal(fitted.clips[0].end_seconds, 15.15);
  assert.deepEqual(fitted.clips[0].killfeed_seconds, [8]);
  assert.deepEqual(fitted.clips[0].killfeed_kills, [[KILL]]);
  assert.equal(plan.clips[0].end_seconds, 20, 'caller plan must stay unchanged');
});

test('fitPlanToSourceDuration preserves custom overruns for strict backend validation', () => {
  const plan: StreamEditPlan = {
    schema_version: '1.0',
    variant: 'streamer-vertical-stack-40-60',
    clips: [
      clip({ id: 'custom-overrun', end_seconds: 19 }),
      clip({ id: 'custom-past-eof', start_seconds: 16, end_seconds: 19 }),
    ],
  };

  const fitted = fitPlanToSourceDuration(plan, 15.15);
  assert.deepEqual(
    fitted.clips.map(({ id, start_seconds, end_seconds }) => ({ id, start_seconds, end_seconds })),
    [
      { id: 'custom-overrun', start_seconds: 0, end_seconds: 19 },
      { id: 'custom-past-eof', start_seconds: 16, end_seconds: 19 },
    ],
  );
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

test('applyClipKillfeedRead replaces cumulative snapshots with aligned event deltas', () => {
  const second: KillfeedKill = { ...KILL, victim_name: 'second' };
  const third: KillfeedKill = { ...KILL, victim_name: 'third' };
  const base = clip({
    killfeed_seconds: [7.575, 8.51],
    killfeed_kills: [[KILL, second], [KILL, second, third]],
  });

  const updated = applyClipKillfeedRead(base, 8.51, [
    { cue_seconds: 2.5, kills: [KILL] },
    { cue_seconds: 2.6, kills: [second] },
    { cue_seconds: 8.4, kills: [third] },
  ]);

  assert.deepEqual(updated.killfeed_seconds, [2.5, 2.6, 8.4]);
  assert.deepEqual(updated.killfeed_kills, [[KILL], [second], [third]]);
});

test('applyClipKillfeedRead moves the requested placeholder to the exact detector time', () => {
  const base = clip({ killfeed_seconds: [2.5], killfeed_kills: [[KILL]] });
  const updated = applyClipKillfeedRead(base, 2.5, [
    { cue_seconds: 2.504, kills: [KILL] },
  ]);
  assert.deepEqual(updated.killfeed_seconds, [2.504]);
  assert.deepEqual(updated.killfeed_kills, [[KILL]]);
  assert.deepEqual(cueProvenance(updated), [
    { cue_seconds: 2.504, origin: 'manual' },
  ]);
});

test('applyClipKillfeedRead only merges events with the exact same frame timestamp', () => {
  const nextFrame = 4 + 1 / 60;
  const updated = applyClipKillfeedRead(clip(), 4, [
    { cue_seconds: 4, kills: [KILL] },
    { cue_seconds: nextFrame, kills: [{ ...KILL, victim_name: 'next-frame' }] },
  ]);
  assert.deepEqual(updated.killfeed_seconds, [4, nextFrame]);
  assert.equal(updated.killfeed_kills?.length, 2);
});

test('exact event enrichment keeps its PTS cue bit-identical beside the next frame', () => {
  const cue = 1001 / 30000;
  const nextFrame = 1002 / 30000;
  const base = clip({
    killfeed_seconds: [cue, nextFrame],
    killfeed_kills: [[], []],
    ...({
      killfeed_cue_provenance: [
        { cue_seconds: cue, origin: 'automatic', event_id: 'event-cue' },
        { cue_seconds: nextFrame, origin: 'automatic', event_id: 'event-next' },
      ],
    } as { killfeed_cue_provenance: KillfeedCueProvenance[] }),
  });
  const updated = applyClipKillfeedRead(base, cue, [{ cue_seconds: cue, kills: [KILL] }]);
  assert.deepEqual(updated.killfeed_seconds, [cue, nextFrame]);
  assert.deepEqual(updated.killfeed_kills, [[KILL], []]);
  assert.deepEqual(cueProvenance(updated), [
    { cue_seconds: cue, origin: 'automatic', event_id: 'event-cue' },
    { cue_seconds: nextFrame, origin: 'automatic', event_id: 'event-next' },
  ]);
});

test('applyClipKillfeedRead preserves unrelated kills from a cumulative cue', () => {
  const unrelated: KillfeedKill = { ...KILL, victim_name: 'manual-kill' };
  const base = clip({
    killfeed_seconds: [8.51],
    killfeed_kills: [[KILL, unrelated]],
  });

  const updated = applyClipKillfeedRead(base, 8.51, [
    { cue_seconds: 2.5, kills: [KILL] },
  ]);

  assert.deepEqual(updated.killfeed_seconds, [2.5, 8.51]);
  assert.deepEqual(updated.killfeed_kills, [[KILL], [unrelated]]);
});

test('applyClipKillfeedRead preserves unresolved cues outside the read interval', () => {
  const base = clip({
    killfeed_seconds: [2, 8.51, 12],
    killfeed_kills: [[], [KILL], []],
  });

  const updated = applyClipKillfeedRead(base, 8.51, [
    { cue_seconds: 8.25, kills: [KILL] },
  ]);

  assert.deepEqual(updated.killfeed_seconds, [2, 8.25, 12]);
  assert.deepEqual(updated.killfeed_kills, [[], [KILL], []]);
});

test('applyClipKillfeedRead preserves unrelated unresolved cues inside the read interval', () => {
  const base = clip({
    killfeed_seconds: [2.7, 8.51],
    killfeed_kills: [[], [KILL]],
  });

  const updated = applyClipKillfeedRead(base, 8.51, [
    { cue_seconds: 2.5, kills: [KILL] },
  ]);

  assert.deepEqual(updated.killfeed_seconds, [2.5, 2.7]);
  assert.deepEqual(updated.killfeed_kills, [[KILL], []]);
});

test('applyClipKillfeedRead clears stale kills when the requested snapshot is empty', () => {
  const base = clip({
    killfeed_seconds: [4],
    killfeed_kills: [[KILL]],
  });

  const updated = applyClipKillfeedRead(base, 4, [
    { cue_seconds: 4, kills: [] },
  ]);

  assert.deepEqual(updated.killfeed_seconds, [4]);
  assert.equal(updated.killfeed_kills, undefined);
});

test('addClipCue inserts a cue and keeps kills aligned', () => {
  const base = clip({ killfeed_seconds: [8], killfeed_kills: [[KILL]] });
  const updated = addClipCue(base, 4);
  assert.deepEqual(updated.killfeed_seconds, [4, 8]);
  assert.deepEqual(updated.killfeed_kills, [[], [KILL]]);
  assert.deepEqual(cueProvenance(updated), [
    { cue_seconds: 4, origin: 'manual' },
  ]);
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
