import test from 'node:test';
import assert from 'node:assert/strict';
import {
  appliedKillfeedEventReference,
  invalidateKillfeedAnalysis,
  killfeedAnalysisInputsFingerprint,
  killfeedAnalysisNeeded,
  killfeedManualCueIssue,
  killfeedStateNeedsRefreshForRead,
} from './killfeed-analysis.ts';
import type { KillfeedAnalysisEvent, KillfeedAnalysisState, StreamEditPlan } from './api/streams.ts';

function plan(): StreamEditPlan {
  return {
    schema_version: '1.1',
    variant: 'streamer-fullframe-nocam',
    gameplay_crop: { x: 0, y: 0, width: 1, height: 1 },
    killfeed_crop: { x: 0.7, y: 0.03, width: 0.28, height: 0.2 },
    killfeed_analysis: {
      generation_id: '11111111-1111-4111-8111-111111111111',
      fingerprint: 'a'.repeat(64),
      applied_at: '2026-07-18T00:00:00Z',
    },
    clips: [{
      id: 'clip-1',
      start_seconds: 1,
      end_seconds: 5,
      killfeed_seconds: [2],
      killfeed_kills: [],
    }],
  };
}

test('killfeed analysis fingerprint changes for crop or clip bounds, not unrelated edits', () => {
  const base = plan();
  const fingerprint = killfeedAnalysisInputsFingerprint(base);
  assert.equal(
    killfeedAnalysisInputsFingerprint({ ...base, effects: { grade: true } }),
    fingerprint,
  );
  assert.notEqual(
    killfeedAnalysisInputsFingerprint({
      ...base,
      killfeed_crop: { x: 0.71, y: 0.03, width: 0.28, height: 0.2 },
    }),
    fingerprint,
  );
  assert.notEqual(
    killfeedAnalysisInputsFingerprint({
      ...base,
      clips: [{ ...base.clips[0], end_seconds: 6 }],
    }),
    fingerprint,
  );
});

test('invalidating killfeed analysis removes provenance and stale aligned events', () => {
  const invalidated = invalidateKillfeedAnalysis(plan());
  assert.equal(invalidated.killfeed_analysis, undefined);
  assert.equal(invalidated.clips[0].killfeed_seconds, undefined);
  assert.equal(invalidated.clips[0].killfeed_kills, undefined);
  assert.equal(killfeedAnalysisNeeded(invalidated), true);
});

test('applied event reference uses exact cue identity for adjacent source frames', () => {
  const base = plan();
  const firstCue = 1001 / 30000;
  const nextCue = 1002 / 30000;
  const event = (eventId: string, sourcePts: number, cueSeconds: number): KillfeedAnalysisEvent => ({
    event_id: eventId,
    source_pts: sourcePts,
    time_base: { num: 1, den: 30000 },
    cue_seconds: cueSeconds,
    onset_start_pts: sourcePts - 1,
    onset_end_pts: sourcePts,
    sample_pts: sourcePts + 100,
    sample_seconds: (sourcePts + 100) / 30000,
    mode: 'aligned_frame',
    rows: [{
      onset_row_index: 0,
      sample_row_index: 0,
      fingerprint: eventId,
      onset_bounds: { x: 10, y: 10, width: 100, height: 30 },
      sample_bounds: { x: 10, y: 10, width: 100, height: 30 },
    }],
    kills: [],
  });
  const state: KillfeedAnalysisState = {
    job_id: '00000000-0000-4000-8000-000000000001',
    generation_id: base.killfeed_analysis?.generation_id ?? '',
    status: 'applied',
    fingerprint: base.killfeed_analysis?.fingerprint,
    clips: [{
      clip_id: 'clip-1',
      start_seconds: 1,
      end_seconds: 5,
      events: [event('event-first', 1001, firstCue), event('event-next', 1002, nextCue)],
    }],
    updated_at: '2026-07-18T00:00:00Z',
  };

  assert.deepEqual(
    appliedKillfeedEventReference(base, state, 'clip-1', firstCue),
    { eventId: 'event-first', generationId: state.generation_id },
  );
  assert.deepEqual(
    appliedKillfeedEventReference(base, state, 'clip-1', nextCue),
    { eventId: 'event-next', generationId: state.generation_id },
  );
  assert.equal(appliedKillfeedEventReference(base, state, 'clip-1', firstCue + 1e-12), undefined);
});

test('event reference rejects stale or non-applied analysis state', () => {
  const base = plan();
  const state: KillfeedAnalysisState = {
    job_id: '00000000-0000-4000-8000-000000000001',
    generation_id: '22222222-2222-4222-8222-222222222222',
    status: 'applied',
    fingerprint: base.killfeed_analysis?.fingerprint,
    clips: [],
    updated_at: '2026-07-18T00:00:00Z',
  };
  assert.equal(appliedKillfeedEventReference(base, state, 'clip-1', 2), undefined);
  assert.equal(appliedKillfeedEventReference(base, { ...state, status: 'ready' }, 'clip-1', 2), undefined);
});

test('exact reads refresh missing or stale applied generation state before choosing a path', () => {
  const base = plan();
  const matching: KillfeedAnalysisState = {
    job_id: '00000000-0000-4000-8000-000000000001',
    generation_id: base.killfeed_analysis?.generation_id ?? '',
    status: 'applied',
    fingerprint: base.killfeed_analysis?.fingerprint,
    clips: [],
    updated_at: '2026-07-18T00:00:00Z',
  };
  assert.equal(killfeedStateNeedsRefreshForRead(base, null), true);
  assert.equal(killfeedStateNeedsRefreshForRead(base, { ...matching, status: 'ready' }), true);
  assert.equal(killfeedStateNeedsRefreshForRead(base, matching), false);
  const manual = { ...base };
  delete manual.killfeed_analysis;
  assert.equal(killfeedStateNeedsRefreshForRead(manual, null), false);
});

test('render issue distinguishes exact empty events from unresolved manual placeholders', () => {
  const base = plan();
  const event: KillfeedAnalysisEvent = {
    event_id: 'event-exact',
    source_pts: 2000,
    time_base: { num: 1, den: 1000 },
    cue_seconds: 2,
    onset_start_pts: 1999,
    onset_end_pts: 2000,
    sample_pts: 2350,
    sample_seconds: 2.35,
    mode: 'aligned_frame',
    rows: [{
      onset_row_index: 0,
      sample_row_index: 0,
      fingerprint: 'row',
      onset_bounds: { x: 10, y: 10, width: 100, height: 30 },
      sample_bounds: { x: 10, y: 10, width: 100, height: 30 },
    }],
    kills: [],
  };
  const state: KillfeedAnalysisState = {
    job_id: '00000000-0000-4000-8000-000000000001',
    generation_id: base.killfeed_analysis?.generation_id ?? '',
    status: 'applied',
    fingerprint: base.killfeed_analysis?.fingerprint,
    clips: [{ clip_id: 'clip-1', start_seconds: 1, end_seconds: 5, events: [event] }],
    updated_at: '2026-07-18T00:00:00Z',
  };

  assert.equal(killfeedManualCueIssue(base, state), undefined, 'captured empty event is renderable');
  const unresolved = {
    ...base,
    clips: [{ ...base.clips[0], killfeed_seconds: [2, 3], killfeed_kills: [[], []] }],
  };
  assert.match(killfeedManualCueIssue(unresolved, state) ?? '', /marca manual 3\.000s/);
  const reviewed = {
    ...unresolved,
    clips: [{
      ...unresolved.clips[0],
      killfeed_kills: [[], [{
        attacker_side: 'CT' as const,
        attacker_name: 'hero',
        victim_side: 'T' as const,
        victim_name: 'victim',
        weapon: 'ak47',
      }]],
    }],
  };
  assert.equal(killfeedManualCueIssue(reviewed, state), undefined);
  const missingAutomatic = {
    ...base,
    clips: [{ ...base.clips[0], killfeed_seconds: [], killfeed_kills: [] }],
  };
  assert.match(killfeedManualCueIssue(missingAutomatic, state) ?? '', /Falta el evento automático 2\.000s/);
});
