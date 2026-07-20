import assert from 'node:assert/strict';
import test from 'node:test';
import { planToPlays } from './map.ts';

test('planToPlays removes duplicate timeline segments but preserves distinct plays', () => {
  const plays = planToPlays('job-1', {
    segments: [
      { id: 'first', round: 4, tick_start: 100, tick_end: 200, kills: [{ weapon: 'ak47' }] },
      { id: 'duplicate', round: 4, tick_start: 100, tick_end: 200, kills: [{ weapon: 'ak47' }] },
      { id: 'second', round: 4, tick_start: 210, tick_end: 260, kills: [{ weapon: 'awp' }] },
    ],
  });
  assert.deepEqual(plays.map((play) => play.id), ['first', 'second']);
});
