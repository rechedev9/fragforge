// Unit tests for the pure intent coercion (corrupt-storage tolerance).
// Run: node --test reel-store.test.mjs
import test from 'node:test';
import assert from 'node:assert/strict';
import { coerceIntents } from './reel-store.ts';

const valid = {
  videoId: 'job__seg-001',
  jobId: 'job',
  segmentId: 'seg-001',
  mode: 'music',
  variant: 'clean-pov-60',
  songId: 's1',
  title: 'Ace',
  map: 'de_dust2',
  score: '13-7',
  createdAt: 123,
  published: true,
};

test('keeps a well-formed intent verbatim', () => {
  assert.deepEqual(coerceIntents([valid]), [valid]);
});

test('non-array input → empty', () => {
  assert.deepEqual(coerceIntents(null), []);
  assert.deepEqual(coerceIntents({}), []);
  assert.deepEqual(coerceIntents('nope'), []);
});

test('drops entries missing required ids', () => {
  assert.deepEqual(coerceIntents([{ jobId: 'j', segmentId: 's' }, 42, null]), []);
});

test('defaults soft fields, normalizes mode, migrates missing variant', () => {
  assert.deepEqual(coerceIntents([{ videoId: 'v', jobId: 'j', segmentId: 's', mode: 'weird' }]), [
    { videoId: 'v', jobId: 'j', segmentId: 's', mode: 'clean', variant: 'viral-60-clean', songId: undefined, title: 'Highlight', map: 'Unknown', score: '', createdAt: 0, published: false },
  ]);
});
