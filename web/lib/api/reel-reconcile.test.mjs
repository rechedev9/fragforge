// Unit tests for the pure reel-reconcile core. Run: node --test reel-reconcile.test.mjs
// Plain .mjs (invisible to tsc/Next) importing the type-stripped .ts module, so the
// reconcile state machine is testable with zero new dependencies (Node 24 node:test).
import test from 'node:test';
import assert from 'node:assert/strict';
import { deriveReelView } from './reel-reconcile.ts';

/** deriveReelView with sane defaults so each case states only what it varies. */
function view(over) {
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
