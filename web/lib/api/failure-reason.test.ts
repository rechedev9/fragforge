// Unit tests for classifying a reel's failureReason into a typed result.
// Run: node --test failure-reason.test.ts
import test from 'node:test';
import assert from 'node:assert/strict';
import { DEMO_INCOMPATIBLE_PREFIX, parseFailureReason } from './failure-reason.ts';

test('demo-incompatible reason with a captured clause yields counts and no retry', () => {
  const reason =
    'demo_incompatible: cs2 cannot replay this demo (it was recorded on an older cs2 build); captured 1/16 segments before the failure';
  const result = parseFailureReason(reason);
  assert.equal(result.kind, 'demo-incompatible');
  assert.equal(result.retryCanHelp, false);
  assert.deepEqual(result.counts, { captured: 1, requested: 16 });
  assert.match(result.message, /versión antigua de CS2/);
  assert.match(result.message, /Se capturaron 1 de 16 jugadas/);
});

test('demo-incompatible reason without a captured clause has no counts', () => {
  const reason = 'demo_incompatible: cs2 cannot replay this demo (it was recorded on an older cs2 build)';
  const result = parseFailureReason(reason);
  assert.equal(result.kind, 'demo-incompatible');
  assert.equal(result.retryCanHelp, false);
  assert.equal(result.counts, undefined);
  assert.doesNotMatch(result.message, /Se capturaron/);
});

test('a generic reason stays generic and retryable', () => {
  const reason = 'ffmpeg exited with code 1';
  const result = parseFailureReason(reason);
  assert.equal(result.kind, 'generic');
  assert.equal(result.retryCanHelp, true);
  assert.equal(result.message, reason);
  assert.equal(result.counts, undefined);
});

test('undefined and empty reasons fall back to a generic retryable message', () => {
  for (const reason of [undefined, '', '   ']) {
    const result = parseFailureReason(reason);
    assert.equal(result.kind, 'generic');
    assert.equal(result.retryCanHelp, true);
    assert.equal(result.message, 'El reel falló en tu equipo.');
  }
});

test('the exported prefix is the exact orchestrator token', () => {
  assert.equal(DEMO_INCOMPATIBLE_PREFIX, 'demo_incompatible:');
});
