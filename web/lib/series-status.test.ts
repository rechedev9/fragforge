// Unit tests for the series status presentation helpers.
// Run: node --test series-status.test.ts
import test from 'node:test';
import assert from 'node:assert/strict';
import {
  seriesStatusLabel,
  seriesStatusTone,
  seriesStatusIsPending,
  seriesStatusIsForgeable,
  summarizeSeriesStatuses,
} from './series-status.ts';

test('maps every known status to its Spanish label', () => {
  assert.equal(seriesStatusLabel('queued'), 'analizando');
  assert.equal(seriesStatusLabel('scanning'), 'analizando');
  // Settled, not progress: after the pick, a map without the chosen player
  // parks at 'scanned' forever, so it must not read as active work.
  assert.equal(seriesStatusLabel('scanned'), 'sin jugador elegido');
  assert.equal(seriesStatusLabel('parsing'), 'analizando');
  assert.equal(seriesStatusLabel('parsed'), 'lista para forjar');
  assert.equal(seriesStatusLabel('recording'), 'grabando');
  assert.equal(seriesStatusLabel('recorded'), 'grabando');
  assert.equal(seriesStatusLabel('composing'), 'renderizando');
  assert.equal(seriesStatusLabel('composed'), 'renderizando');
  assert.equal(seriesStatusLabel('done'), 'completada');
  assert.equal(seriesStatusLabel('failed'), 'fallida');
});

test('an unknown status falls back to "analizando"', () => {
  assert.equal(seriesStatusLabel('something-new'), 'analizando');
  assert.equal(seriesStatusTone('something-new'), 'pending');
});

test('tone distinguishes ready, progress, done and failed from pending', () => {
  assert.equal(seriesStatusTone('queued'), 'pending');
  assert.equal(seriesStatusTone('scanned'), 'pending');
  assert.equal(seriesStatusTone('parsed'), 'ready');
  assert.equal(seriesStatusTone('recording'), 'progress');
  assert.equal(seriesStatusTone('composing'), 'progress');
  assert.equal(seriesStatusTone('done'), 'done');
  assert.equal(seriesStatusTone('failed'), 'failed');
});

test('pending drives polling and excludes the stuck/transient statuses', () => {
  assert.equal(seriesStatusIsPending('queued'), true);
  assert.equal(seriesStatusIsPending('scanning'), true);
  assert.equal(seriesStatusIsPending('parsing'), true);
  assert.equal(seriesStatusIsPending('recording'), true);
  assert.equal(seriesStatusIsPending('composing'), true);
  // A skipped demo parks at 'scanned' forever; it must not keep the loop alive.
  assert.equal(seriesStatusIsPending('scanned'), false);
  assert.equal(seriesStatusIsPending('recorded'), false);
  assert.equal(seriesStatusIsPending('parsed'), false);
  assert.equal(seriesStatusIsPending('done'), false);
  assert.equal(seriesStatusIsPending('failed'), false);
});

test('forgeable matches statuses at or past a ready kill plan', () => {
  assert.equal(seriesStatusIsForgeable('parsed'), true);
  assert.equal(seriesStatusIsForgeable('recording'), true);
  assert.equal(seriesStatusIsForgeable('done'), true);
  assert.equal(seriesStatusIsForgeable('scanned'), false);
  assert.equal(seriesStatusIsForgeable('scanning'), false);
  assert.equal(seriesStatusIsForgeable('failed'), false);
});

test('summarize buckets statuses disjointly for the header', () => {
  assert.deepEqual(summarizeSeriesStatuses([]), { ready: 0, pending: 0, failed: 0, skipped: 0 });
  assert.deepEqual(summarizeSeriesStatuses(['parsed', 'done', 'recording']), {
    ready: 3,
    pending: 0,
    failed: 0,
    skipped: 0,
  });
  assert.deepEqual(summarizeSeriesStatuses(['parsing', 'scanned', 'failed', 'parsed']), {
    ready: 1,
    pending: 1,
    failed: 1,
    skipped: 1,
  });
});

test('summarize never calls a settled map pending', () => {
  // 'scanned' (no chosen player) and 'failed' are settled: they must land in
  // their own buckets, never in pending, so the header cannot claim they are
  // still processing.
  const summary = summarizeSeriesStatuses(['scanned', 'scanned', 'failed']);
  assert.equal(summary.pending, 0);
  assert.deepEqual(summary, { ready: 0, pending: 0, failed: 1, skipped: 2 });
});

test('summarize keeps an unknown status consistent with its "analizando" pill', () => {
  assert.deepEqual(summarizeSeriesStatuses(['something-new']), { ready: 0, pending: 1, failed: 0, skipped: 0 });
});
