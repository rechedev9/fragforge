// Unit tests for the pure Partidas index logic (job listing → Matches/series).
// Run: node --test jobs-index.test.ts
import test from 'node:test';
import assert from 'node:assert/strict';
import type { DemoPlayer } from './types.ts';
import {
  jobHasRoster,
  jobCreatedAtMs,
  listableJobs,
  summarizeSeries,
  statsFromPlayer,
  jobToMatch,
  ZERO_STATS,
  type IndexedJob,
} from './jobs-index.ts';

/** Builds an IndexedJob with sane defaults so each case sets only what it tests. */
function job(overrides: Partial<IndexedJob> & { jobId: string }): IndexedJob {
  return {
    jobId: overrides.jobId,
    status: overrides.status ?? 'done',
    failureReason: overrides.failureReason,
    fileName: overrides.fileName,
    seriesId: overrides.seriesId,
    targetSteamId: overrides.targetSteamId,
    createdAt: overrides.createdAt,
  };
}

/** Builds a DemoPlayer with zeroed defaults so each case sets only what it tests. */
function player(overrides: Partial<DemoPlayer> & { steamId: string }): DemoPlayer {
  return {
    steamId: overrides.steamId,
    name: overrides.name ?? 'player',
    team: overrides.team ?? 'CT',
    kills: overrides.kills ?? 0,
    deaths: overrides.deaths ?? 0,
    assists: overrides.assists ?? 0,
    headshots: overrides.headshots ?? 0,
    mvps: overrides.mvps ?? 0,
    rounds: overrides.rounds ?? 0,
    adr: overrides.adr ?? 0,
    hsPct: overrides.hsPct ?? 0,
    kast: overrides.kast ?? 0,
    rating: overrides.rating ?? 0,
  };
}

test('jobHasRoster admits scanned..done and rejects queued/scanning/failed', () => {
  for (const status of ['scanned', 'parsing', 'parsed', 'recording', 'recorded', 'composing', 'composed', 'done']) {
    assert.equal(jobHasRoster(status), true, status);
  }
  for (const status of ['queued', 'scanning', 'failed', 'something-new']) {
    assert.equal(jobHasRoster(status), false, status);
  }
});

test('listableJobs keeps only roster-ready jobs, newest first', () => {
  const jobs = [
    job({ jobId: 'a', status: 'done', createdAt: '2026-07-10T10:00:00Z' }),
    job({ jobId: 'b', status: 'scanning', createdAt: '2026-07-16T10:00:00Z' }),
    job({ jobId: 'c', status: 'failed', createdAt: '2026-07-15T10:00:00Z' }),
    job({ jobId: 'd', status: 'scanned', createdAt: '2026-07-14T10:00:00Z' }),
    job({ jobId: 'e', status: 'parsed', createdAt: '2026-07-12T10:00:00Z' }),
  ];
  assert.deepEqual(
    listableJobs(jobs).map((j) => j.jobId),
    ['d', 'e', 'a'],
  );
});

test('listableJobs sorts a job with no createdAt last', () => {
  const jobs = [
    job({ jobId: 'none' }),
    job({ jobId: 'new', createdAt: '2026-07-16T10:00:00Z' }),
    job({ jobId: 'old', createdAt: '2026-01-01T10:00:00Z' }),
  ];
  assert.deepEqual(
    listableJobs(jobs).map((j) => j.jobId),
    ['new', 'old', 'none'],
  );
});

test('jobCreatedAtMs returns 0 for missing or unparseable timestamps', () => {
  assert.equal(jobCreatedAtMs(job({ jobId: 'a' })), 0);
  assert.equal(jobCreatedAtMs(job({ jobId: 'b', createdAt: 'not-a-date' })), 0);
  assert.equal(jobCreatedAtMs(job({ jobId: 'c', createdAt: '2026-07-16T10:00:00Z' })), Date.parse('2026-07-16T10:00:00Z'));
});

test('summarizeSeries groups by series id, counts every map, newest series first', () => {
  const jobs = [
    job({ jobId: 'a1', seriesId: 'S1', status: 'done', createdAt: '2026-07-10T10:00:00Z' }),
    job({ jobId: 'a2', seriesId: 'S1', status: 'scanning', createdAt: '2026-07-10T10:05:00Z' }),
    job({ jobId: 'a3', seriesId: 'S1', status: 'parsed', createdAt: '2026-07-10T10:02:00Z' }),
    job({ jobId: 'b1', seriesId: 'S2', status: 'done', createdAt: '2026-07-16T09:00:00Z' }),
    job({ jobId: 'solo', status: 'done', createdAt: '2026-07-16T12:00:00Z' }),
  ];
  const summaries = summarizeSeries(jobs);
  // Standalone (no seriesId) never becomes a series; S2 (newer start) leads.
  assert.deepEqual(summaries, [
    { seriesId: 'S2', mapCount: 1, createdAt: Date.parse('2026-07-16T09:00:00Z') },
    { seriesId: 'S1', mapCount: 3, createdAt: Date.parse('2026-07-10T10:00:00Z') },
  ]);
});

test('summarizeSeries with no series jobs returns an empty list', () => {
  assert.deepEqual(summarizeSeries([job({ jobId: 'a' }), job({ jobId: 'b' })]), []);
});

test('statsFromPlayer maps the target scoreboard and computes K/D', () => {
  const stats = statsFromPlayer(player({ steamId: '1', kills: 24, deaths: 12, assists: 5, mvps: 3, rating: 1.3, adr: 92, kast: 78, hsPct: 61 }));
  assert.deepEqual(stats, { kills: 24, deaths: 12, assists: 5, mvps: 3, kd: 2, rating: 1.3, adr: 92, kast: 78, hsPct: 61 });
});

test('statsFromPlayer uses kills as K/D when deaths is zero', () => {
  assert.equal(statsFromPlayer(player({ steamId: '1', kills: 7, deaths: 0 })).kd, 7);
});

test('jobToMatch enriches map (prettified) and the target player stats', () => {
  const match = jobToMatch(
    job({ jobId: 'job-1', targetSteamId: '765', createdAt: '2026-07-16T10:00:00Z' }),
    { map: 'de_inferno', player: player({ steamId: '765', kills: 20, deaths: 10, assists: 4 }) },
  );
  assert.equal(match.id, 'job-1');
  assert.equal(match.map, 'Inferno');
  assert.equal(match.score, '');
  assert.equal(match.playedAt, '2026-07-16T10:00:00Z');
  assert.equal(match.source, 'upload');
  assert.equal(match.decentPlays, 0);
  assert.equal(match.stats.kills, 20);
  assert.equal(match.stats.kd, 2);
});

test('jobToMatch falls back to a filename-titled, zeroed entry without enrichment', () => {
  const match = jobToMatch(job({ jobId: 'job-2', fileName: 'match730.dem', createdAt: '2026-07-16T10:00:00Z' }));
  assert.equal(match.map, 'match730.dem');
  assert.deepEqual(match.stats, ZERO_STATS);
  assert.equal(match.source, 'upload');
});

test('jobToMatch titles a roster-less, nameless job as "Partida"', () => {
  assert.equal(jobToMatch(job({ jobId: 'job-3' })).map, 'Partida');
});

test('jobToMatch zeroes stats when the target is not in the roster (map only)', () => {
  const match = jobToMatch(job({ jobId: 'job-4', createdAt: '2026-07-16T10:00:00Z' }), { map: 'de_nuke' });
  assert.equal(match.map, 'Nuke');
  assert.deepEqual(match.stats, ZERO_STATS);
});

test('summarizeSeries folds split demo parts into one logical map', () => {
  const jobs = [
    job({ jobId: 'a', seriesId: 'S1', fileName: 'x-m1-inferno-p1.dem', createdAt: '2026-07-16T10:00:00Z' }),
    job({ jobId: 'b', seriesId: 'S1', fileName: 'x-m1-inferno-p2.dem', createdAt: '2026-07-16T10:01:00Z' }),
    job({ jobId: 'c', seriesId: 'S1', fileName: 'x-m2-cache.dem', createdAt: '2026-07-16T10:02:00Z' }),
  ];
  assert.deepEqual(summarizeSeries(jobs), [
    { seriesId: 'S1', mapCount: 2, createdAt: Date.parse('2026-07-16T10:00:00Z') },
  ]);
});
