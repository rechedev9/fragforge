// Unit tests for the series grouping helpers.
// Run: node --test series-grouping.test.ts
import test from 'node:test';
import assert from 'node:assert/strict';
import { groupSeriesDemos, parseSeriesFileName, representativeSeriesStatus } from './series-grouping.ts';

test('parses part, base and map order from an HLTV-style name', () => {
  assert.deepEqual(parseSeriesFileName('3dmax-vs-heroic-m1-inferno-p2.dem'), {
    base: '3dmax-vs-heroic-m1-inferno',
    mapOrder: 1,
    part: 2,
  });
});

test('treats the .dem extension and part suffix as optional and case-insensitive', () => {
  // No extension, uppercase part suffix.
  assert.deepEqual(parseSeriesFileName('team-m3-nuke-P1'), {
    base: 'team-m3-nuke',
    mapOrder: 3,
    part: 1,
  });
  // No part suffix at all: base keeps the whole extension-less name.
  assert.deepEqual(parseSeriesFileName('team-m2-cache.dem'), {
    base: 'team-m2-cache',
    mapOrder: 2,
    part: null,
  });
  // No map-number token: mapOrder is null.
  assert.deepEqual(parseSeriesFileName('just-inferno-p1.dem'), {
    base: 'just-inferno',
    mapOrder: null,
    part: 1,
  });
});

test('groups shuffled parts into ordered map cards (the 3dmax bo3 case)', () => {
  const demos = [
    { fileName: '3dmax-vs-heroic-m3-nuke.dem', jobId: 'nuke' },
    { fileName: '3dmax-vs-heroic-m1-inferno-p2.dem', jobId: 'inf2' },
    { fileName: '3dmax-vs-heroic-m2-cache.dem', jobId: 'cache' },
    { fileName: '3dmax-vs-heroic-m1-inferno-p1.dem', jobId: 'inf1' },
  ];
  const groups = groupSeriesDemos(demos);

  assert.equal(groups.length, 3);
  // Ordered by map number regardless of input order.
  assert.deepEqual(
    groups.map((g) => g.mapOrder),
    [1, 2, 3],
  );
  // The inferno parts fold into one group, sorted p1 then p2.
  assert.deepEqual(
    groups[0].demos.map((d) => d.jobId),
    ['inf1', 'inf2'],
  );
  assert.deepEqual(
    groups[1].demos.map((d) => d.jobId),
    ['cache'],
  );
  assert.deepEqual(
    groups[2].demos.map((d) => d.jobId),
    ['nuke'],
  );
});

test('demos without a part suffix each stay a singleton group', () => {
  const demos = [
    { fileName: 'a-m1-inferno.dem', jobId: 'a' },
    { fileName: 'a-m2-cache.dem', jobId: 'b' },
  ];
  const groups = groupSeriesDemos(demos);
  assert.equal(groups.length, 2);
  assert.deepEqual(
    groups.map((g) => g.demos.map((d) => d.jobId)),
    [['a'], ['b']],
  );
});

test('folds parts whose base differs only in case', () => {
  const demos = [
    { fileName: 'Team-M1-Inferno-P1.dem', jobId: 'a' },
    { fileName: 'team-m1-inferno-p2.dem', jobId: 'b' },
  ];
  const groups = groupSeriesDemos(demos);
  assert.equal(groups.length, 1);
  assert.deepEqual(
    groups[0].demos.map((d) => d.jobId),
    ['a', 'b'],
  );
});

test('keeps two same-named maps with different bases in separate groups', () => {
  // Same map name (inferno) and same part suffix, but different match bases:
  // these are two distinct maps, not two parts of one.
  const demos = [
    { fileName: 'faze-vs-nav-m1-inferno-p1.dem', jobId: 'a' },
    { fileName: 'g2-vs-vitality-m1-inferno-p1.dem', jobId: 'b' },
  ];
  const groups = groupSeriesDemos(demos);
  assert.equal(groups.length, 2);
  assert.deepEqual(
    groups.map((g) => g.demos.map((d) => d.jobId)),
    [['a'], ['b']],
  );
});

test('preserves input order when any group lacks a map-number token', () => {
  const demos = [
    { fileName: 'second-m2-cache.dem', jobId: 'a' },
    { fileName: 'first-inferno.dem', jobId: 'b' }, // no m<n> token
  ];
  const groups = groupSeriesDemos(demos);
  assert.deepEqual(
    groups.map((g) => g.demos[0].jobId),
    ['a', 'b'],
  );
});

test('treats a demo without a file name as its own singleton group', () => {
  const demos = [
    { jobId: 'a', fileName: undefined },
    { jobId: 'b', fileName: undefined },
  ];
  const groups = groupSeriesDemos(demos);
  assert.equal(groups.length, 2);
  assert.deepEqual(
    groups.map((g) => g.demos.map((d) => d.jobId)),
    [['a'], ['b']],
  );
});

test('representativeSeriesStatus prefers forgeable, then pending, then failed', () => {
  // A map with one forgeable part reads as ready even while a sibling parses.
  assert.equal(representativeSeriesStatus(['parsing', 'parsed']), 'parsed');
  assert.equal(representativeSeriesStatus(['recording', 'queued']), 'recording');
  // No forgeable part: still working wins over settled failures.
  assert.equal(representativeSeriesStatus(['failed', 'parsing']), 'parsing');
  assert.equal(representativeSeriesStatus(['failed', 'failed']), 'failed');
  // Settled without the player, and the empty-input fallback.
  assert.equal(representativeSeriesStatus(['scanned']), 'scanned');
  assert.equal(representativeSeriesStatus([]), 'scanned');
});
