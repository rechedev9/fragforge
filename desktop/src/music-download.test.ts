import test from 'node:test';
import assert from 'node:assert/strict';
import { musicDownloadHeaders } from './music-download.ts';

test('uses the catalog source page as the audio download referer', () => {
  assert.deepEqual(musicDownloadHeaders('https://music.example/track'), {
    Referer: 'https://music.example/track',
    'User-Agent': 'FragForge Studio music provisioner',
  });
});

test('omits headers when a catalog track has no source page', () => {
  assert.equal(musicDownloadHeaders(undefined), undefined);
  assert.equal(musicDownloadHeaders(''), undefined);
});
