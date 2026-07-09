import test from 'node:test';
import assert from 'node:assert/strict';
import { lastLines } from './log-tail.ts';

test('returns all lines when there are fewer than the limit', () => {
  assert.equal(lastLines('a\nb\nc', 40), 'a\nb\nc');
});

test('returns exactly the last N lines when there are more', () => {
  assert.equal(lastLines('1\n2\n3\n4\n5', 2), '4\n5');
});

test('returns everything when the count equals the number of lines', () => {
  assert.equal(lastLines('x\ny\nz', 3), 'x\ny\nz');
});

test('handles a single line', () => {
  assert.equal(lastLines('only', 40), 'only');
});

test('preserves blank lines within the tail window', () => {
  assert.equal(lastLines('a\n\nb\n\nc', 3), 'b\n\nc');
});
