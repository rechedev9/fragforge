// Unit tests for the job-summary → Match mapping used by the matches list.
// Run: node --test map.test.ts
import test from 'node:test';
import assert from 'node:assert/strict';
import { jobSummaryToMatch, jobSummariesToMatches } from './map.ts';
import type { JobSummary } from './types.ts';

const JOB = '11111111-1111-4111-8111-111111111111';

test('jobSummaryToMatch maps a plan-ready summary', () => {
  const summary: JobSummary = {
    id: JOB,
    status: 'parsed',
    created_at: '2026-07-11T10:00:00Z',
    map: 'de_inferno',
    target_kills: 24,
    segment_count: 5,
  };
  const match = jobSummaryToMatch(summary);
  assert.equal(match.id, JOB);
  assert.equal(match.map, 'Inferno');
  assert.equal(match.score, '');
  assert.equal(match.playedAt, '2026-07-11T10:00:00Z');
  assert.equal(match.stats.kills, 24);
  assert.equal(match.stats.deaths, 0);
  assert.equal(match.stats.kd, 24); // deaths 0 → kd is the kill count.
  assert.equal(match.stats.mvps, 0);
  assert.equal(match.stats.rating, undefined);
  assert.equal(match.stats.adr, undefined);
  assert.equal(match.stats.kast, undefined);
  assert.equal(match.stats.hsPct, undefined);
  assert.equal(match.decentPlays, 5);
  assert.equal(match.source, 'upload');
  assert.ok(match.thumbnailUrl);
});

test('jobSummaryToMatch degrades gracefully when plan-derived fields are absent', () => {
  const match = jobSummaryToMatch({ id: JOB, status: 'parsed', created_at: '2026-07-11T10:00:00Z' });
  assert.equal(match.map, ''); // prettifyMap('') stays empty.
  assert.equal(match.stats.kills, 0);
  assert.equal(match.stats.kd, 0);
  assert.equal(match.decentPlays, 0);
});

test('jobSummariesToMatches drops failed and plan-less jobs and sorts newest first', () => {
  const jobs: JobSummary[] = [
    { id: 'a', status: 'parsed', created_at: '2026-07-11T09:00:00Z', map: 'de_dust2', target_kills: 10, segment_count: 2 },
    { id: 'b', status: 'scanning', created_at: '2026-07-11T11:00:00Z' }, // no map → dropped.
    { id: 'c', status: 'failed', created_at: '2026-07-11T12:00:00Z', map: 'de_mirage' }, // failed → dropped.
    { id: 'd', status: 'done', created_at: '2026-07-11T10:00:00Z', map: 'de_nuke', target_kills: 30, segment_count: 8 },
  ];
  const matches = jobSummariesToMatches(jobs);
  assert.deepEqual(
    matches.map((m) => m.id),
    ['d', 'a'], // newest (created_at) first: d (10:00) before a (09:00).
  );
});
