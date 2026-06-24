// Unit tests for the pure intent coercion (corrupt-storage tolerance).
// Run: node --test reel-store.test.mjs
import test from 'node:test';
import assert from 'node:assert/strict';
import { coerceEditConfig, coerceIntents, DEFAULT_EDIT_CONFIG } from './reel-store.ts';

const valid = {
  videoId: 'job__seg-001',
  jobId: 'job',
  segmentId: 'seg-001',
  mode: 'music',
  variant: 'clean-pov-60',
  editConfig: { format: 'landscape-16x9', killEffect: 'velocity', transition: 'whip', intro: true, outro: false },
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
    { videoId: 'v', jobId: 'j', segmentId: 's', mode: 'clean', variant: 'viral-60-clean', editConfig: DEFAULT_EDIT_CONFIG, songId: undefined, title: 'Highlight', map: 'Unknown', score: '', createdAt: 0, published: false },
  ]);
});

test('coerces edit config independently', () => {
  assert.deepEqual(coerceEditConfig({ format: 'landscape-16x9', killEffect: 'freeze-flash', transition: 'dip', intro: true, outro: true }), {
    format: 'landscape-16x9',
    killEffect: 'freeze-flash',
    transition: 'dip',
    intro: true,
    outro: true,
  });
  assert.deepEqual(coerceEditConfig({ format: 'square', killEffect: 'bad', transition: 'spin', intro: 'yes' }), DEFAULT_EDIT_CONFIG);
});
