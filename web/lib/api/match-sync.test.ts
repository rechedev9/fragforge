// Unit tests for the reel-library synthesis core (pure decision helpers).
// Run: node --test match-sync.test.ts
import test from 'node:test';
import assert from 'node:assert/strict';
import {
  selectSyntheticCandidates,
  syntheticIntentFrom,
  syntheticVideoId,
  staleSyntheticVideoIds,
} from './match-sync.ts';
import { DEFAULT_EDIT_CONFIG, DEFAULT_VARIANT } from './reel-store.ts';
import type { EditConfig, JobSummary } from './types.ts';

function summary(id: string, status: string, extra: Partial<JobSummary> = {}): JobSummary {
  return { id, status, created_at: '2026-07-11T10:00:00Z', ...extra };
}

test('selectSyntheticCandidates keeps only advanced, uncovered, un-synthesized jobs', () => {
  const summaries: JobSummary[] = [
    summary('scan', 'scanning'), // too early → excluded.
    summary('rec', 'recording'), // no render yet → excluded.
    summary('done', 'done'), // advanced → candidate.
    summary('composed', 'composed'), // advanced → candidate.
    summary('local', 'done'), // covered by a local intent → excluded.
    summary('synthd', 'done'), // already synthesized → excluded.
  ];
  const candidates = selectSyntheticCandidates({
    summaries,
    localJobIds: new Set(['local']),
    existingSyntheticVideoIds: new Set([syntheticVideoId('synthd')]),
  });
  assert.deepEqual(
    candidates.map((s) => s.id).sort(),
    ['composed', 'done'],
  );
});

const OPTS: { variant: string; editConfig: EditConfig; map: string } = {
  variant: DEFAULT_VARIANT,
  editConfig: DEFAULT_EDIT_CONFIG,
  map: 'Inferno', // caller injects the already-prettified display map.
};

test('syntheticIntentFrom builds a pure, driveless reflection of a server render', () => {
  const intent = syntheticIntentFrom(summary('job1', 'done', { map: 'de_inferno' }), OPTS);
  assert.equal(intent.videoId, 'job1__server');
  assert.equal(intent.jobId, 'job1');
  assert.deepEqual(intent.segmentIds, []);
  assert.equal(intent.mode, 'clean');
  assert.equal(intent.variant, DEFAULT_VARIANT);
  assert.deepEqual(intent.editConfig, DEFAULT_EDIT_CONFIG);
  assert.equal(intent.map, 'Inferno'); // the injected display map is carried through.
  assert.equal(intent.score, '');
  assert.equal(intent.published, false);
  assert.equal(intent.createdAt, Date.parse('2026-07-11T10:00:00Z'));
});

test('syntheticIntentFrom falls back to now for an unparseable timestamp', () => {
  const before = Date.now();
  const intent = syntheticIntentFrom(summary('job1', 'done', { created_at: 'not-a-date' }), OPTS);
  assert.ok(intent.createdAt >= before);
});

test('staleSyntheticVideoIds evicts covered and vanished jobs, keeps live uncovered', () => {
  const stale = staleSyntheticVideoIds({
    syntheticVideoIds: [syntheticVideoId('covered'), syntheticVideoId('gone'), syntheticVideoId('live')],
    localJobIds: new Set(['covered']), // a local intent now covers this job.
    liveJobIds: new Set(['covered', 'live']), // 'gone' is no longer on the server.
  });
  assert.deepEqual(
    stale.sort(),
    [syntheticVideoId('covered'), syntheticVideoId('gone')].sort(),
  );
});
