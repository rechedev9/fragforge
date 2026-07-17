// Unit tests for the pure reel-reconcile core. Run: node --test reel-reconcile.test.ts
// Type-checked TypeScript run directly by Node's native type stripping, so the
// reconcile state machine is testable with zero new dependencies (Node 24 node:test).
import test from 'node:test';
import assert from 'node:assert/strict';
import { canHaveRenderState, deriveReelView, unrecoverableJobGoneView, viewForJobGone } from './reel-reconcile.ts';
import type { ReconcileInput } from './reel-reconcile.ts';

/** deriveReelView with sane defaults so each case states only what it varies. */
function view(over: Partial<ReconcileInput>) {
  return deriveReelView({ jobStatus: 'parsed', renderStatus: 'none', ...over });
}

test('parsed + no render → drive record', () => {
  assert.deepEqual(view({ jobStatus: 'parsed' }), { status: 'queued', action: 'record' });
});

test('recording → show recording, do not re-drive record', () => {
  assert.deepEqual(view({ jobStatus: 'recording' }), { status: 'recording', action: 'none' });
});

test('recording with progress → carries segments done/total to the card', () => {
  assert.deepEqual(
    view({ jobStatus: 'recording', captureProgress: { done: 2, total: 4 } }),
    { status: 'recording', action: 'none', captureProgress: { done: 2, total: 4 } },
  );
});

test('recording without progress → no captureProgress key (indeterminate bar)', () => {
  const v = view({ jobStatus: 'recording' });
  assert.equal('captureProgress' in v, false);
});

test('progress is ignored when not recording (never leaks onto other stages)', () => {
  assert.deepEqual(
    view({ jobStatus: 'recorded', captureProgress: { done: 2, total: 4 } }),
    { status: 'composing', action: 'render' },
  );
});

test('recorded + no render → drive render', () => {
  assert.deepEqual(view({ jobStatus: 'recorded' }), { status: 'composing', action: 'render' });
});

test('composed + no render → drive render', () => {
  assert.deepEqual(view({ jobStatus: 'composed' }), { status: 'composing', action: 'render' });
});

test('done + no render → drive render', () => {
  assert.deepEqual(view({ jobStatus: 'done' }), { status: 'composing', action: 'render' });
});

test('render queued → composing, no action', () => {
  assert.deepEqual(view({ jobStatus: 'recorded', renderStatus: 'queued' }), { status: 'composing', action: 'none' });
});

test('render rendering → composing, do not re-drive render', () => {
  assert.deepEqual(view({ jobStatus: 'recorded', renderStatus: 'rendering' }), { status: 'composing', action: 'none' });
});

test('render ready → ready', () => {
  assert.deepEqual(view({ jobStatus: 'done', renderStatus: 'ready' }), { status: 'ready', action: 'none' });
});

test('render ready wins even if job flags failed (a finished reel is ready)', () => {
  assert.deepEqual(
    view({ jobStatus: 'failed', jobFailureReason: 'x', renderStatus: 'ready' }),
    { status: 'ready', action: 'none' },
  );
});

test('job failed → failed with reason', () => {
  assert.deepEqual(
    view({ jobStatus: 'failed', jobFailureReason: 'recorder exited with code 1' }),
    { status: 'failed', action: 'none', failureReason: 'recorder exited with code 1' },
  );
});

test('render failed → failed with reason', () => {
  assert.deepEqual(
    view({ jobStatus: 'recorded', renderStatus: 'failed', renderFailureReason: 'ffmpeg error' }),
    { status: 'failed', action: 'none', failureReason: 'ffmpeg error' },
  );
});

test('still parsing → queued, no action (only drive from parsed onward)', () => {
  assert.deepEqual(view({ jobStatus: 'parsing' }), { status: 'queued', action: 'none' });
  assert.deepEqual(view({ jobStatus: 'scanned' }), { status: 'queued', action: 'none' });
});

test('unknown job status → queued, no action (never guess an action)', () => {
  assert.deepEqual(view({ jobStatus: 'wat' }), { status: 'queued', action: 'none' });
});

test('failed without a reason still reports failed (no spurious empty reason key)', () => {
  assert.deepEqual(view({ jobStatus: 'failed' }), { status: 'failed', action: 'none' });
});

test('unrecoverableJobGoneView: failed + unrecoverable with a failure reason', () => {
  const v = unrecoverableJobGoneView();
  assert.equal(v.status, 'failed');
  assert.equal(v.action, 'none');
  assert.equal(v.unrecoverable, true);
  assert.ok(v.failureReason, 'the card needs a human-readable reason');
});

test('viewForJobGone: latches only after consecutive 404 ticks', () => {
  // One spurious 404 (wrong orchestrator briefly answering) must not brand the
  // reel unrecoverable — the card's advice (delete + re-forge) destroys the
  // rendered artifact, so a single bad poll leaves the view untouched.
  const cases: Array<{ strikes: number; latched: boolean }> = [
    { strikes: 0, latched: false },
    { strikes: 1, latched: false },
    { strikes: 2, latched: true },
    { strikes: 3, latched: true },
  ];
  for (const { strikes, latched } of cases) {
    const view = viewForJobGone(strikes);
    if (latched) {
      assert.deepEqual(view, unrecoverableJobGoneView(), `strikes=${strikes} should latch`);
    } else {
      assert.equal(view, null, `strikes=${strikes} should leave the view untouched`);
    }
  }
});

test('a normal job failure stays recoverable (retry can re-drive it)', () => {
  const v = view({ jobStatus: 'failed', jobFailureReason: 'recorder exited with code 1' });
  assert.equal('unrecoverable' in v, false);
});

test('a normal render failure stays recoverable (retry can re-drive it)', () => {
  const v = view({ jobStatus: 'recorded', renderStatus: 'failed', renderFailureReason: 'ffmpeg error' });
  assert.equal('unrecoverable' in v, false);
});

test('canHaveRenderState: true only once a render POST can have been driven', () => {
  for (const s of ['recorded', 'composing', 'composed', 'done']) {
    assert.equal(canHaveRenderState(s), true, `${s} should allow a render GET`);
  }
});

test('canHaveRenderState: includes failed (a render can be ready before the job fails)', () => {
  // deriveReelView surfaces a finished render as ready even when the job later
  // flags failed, so the render GET must still fire for a failed job.
  assert.equal(canHaveRenderState('failed'), true);
});

test('canHaveRenderState: false for every pre-recorded status (skip the guaranteed 404)', () => {
  for (const s of ['queued', 'scanning', 'scanned', 'parsing', 'parsed', 'recording', 'wat']) {
    assert.equal(canHaveRenderState(s), false, `${s} must not issue a render GET`);
  }
});
