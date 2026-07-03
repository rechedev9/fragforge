// Unit tests for the pure reel-reconcile core. Run: node --test reel-reconcile.test.ts
// Type-checked TypeScript run directly by Node's native type stripping, so the
// reconcile state machine is testable with zero new dependencies (Node 24 node:test).
import test from 'node:test';
import assert from 'node:assert/strict';
import { deriveReelView } from './reel-reconcile.ts';
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

// The render state is shared per job+variant: after reel A of a demo finishes,
// a newer reel B sees renderStatus 'ready' although B's own video does not
// exist. B must be driven through its own capture (record = the generate flow),
// never shown as a ready card whose download 404s.
test('render ready but this reel video missing → drive record', () => {
  assert.deepEqual(
    view({ jobStatus: 'recorded', renderStatus: 'ready', reelVideoMissing: true }),
    { status: 'queued', action: 'record' },
  );
});

test('render ready, reel video missing, another capture running → wait', () => {
  assert.deepEqual(
    view({ jobStatus: 'recording', renderStatus: 'ready', reelVideoMissing: true }),
    { status: 'recording', action: 'none' },
  );
});

test('render ready, reel video missing, job failed → failed with reason', () => {
  assert.deepEqual(
    view({ jobStatus: 'failed', jobFailureReason: 'capture died', renderStatus: 'ready', reelVideoMissing: true }),
    { status: 'failed', action: 'none', failureReason: 'capture died' },
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
