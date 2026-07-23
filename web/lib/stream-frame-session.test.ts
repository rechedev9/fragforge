import assert from 'node:assert/strict';
import test from 'node:test';
import { LatestFrameRequest } from './stream-frame-session.ts';

test('coalesces rapid scrubs and seeks only the latest requested frame', () => {
  const requests = new LatestFrameRequest();
  requests.request(2);
  assert.equal(requests.next(0, 30), 2);

  requests.request(4);
  requests.request(8);
  assert.equal(requests.next(2, 30), null);
  assert.equal(requests.settled(2, 30), 8);
  assert.equal(requests.settled(8, 30), null);
});

test('reset clears an in-flight source seek and clamps to the final frame', () => {
  const requests = new LatestFrameRequest();
  requests.request(10);
  assert.equal(requests.next(0, 30), 10);

  requests.reset(12);
  assert.equal(requests.next(0, 5), 4.999);
});
