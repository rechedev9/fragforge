import test from 'node:test';
import assert from 'node:assert/strict';
import * as fs from 'node:fs';
import * as os from 'node:os';
import * as path from 'node:path';
import { createHash } from 'node:crypto';
import { fileURLToPath } from 'node:url';
import { provisionMusicLibrary } from './music-library.ts';

const VIRAL_CC0_TRACK_IDS = [
  'pop-hook',
  'club-jump-beat',
  'dark-electroshuffle',
  'percussive-party',
  'hard-rap-loop',
  'acid-beat',
  'urban-funk',
  'retro-fireworks',
] as const;

function sha256(value: string): string {
  return createHash('sha256').update(value).digest('hex');
}

function temporaryLibrary(t: test.TestContext): {
  bundledMusicDir: string;
  musicDir: string;
} {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'fragforge-music-'));
  t.after(() => fs.rmSync(root, { recursive: true, force: true }));
  return {
    bundledMusicDir: path.join(root, 'bundled'),
    musicDir: path.join(root, 'library'),
  };
}

test('does nothing when the bundled catalog is absent', async (t) => {
  const paths = temporaryLibrary(t);

  await provisionMusicLibrary({
    ...paths,
    signal: new AbortController().signal,
    logLine: () => {},
  });

  assert.equal(fs.existsSync(paths.musicDir), false);
});

test('copies bundled tracks and downloads remote tracks sequentially', async (t) => {
  const paths = temporaryLibrary(t);
  fs.mkdirSync(paths.bundledMusicDir, { recursive: true });
  fs.mkdirSync(paths.musicDir, { recursive: true });
  fs.writeFileSync(path.join(paths.bundledMusicDir, 'local.mp3'), 'bundled audio');
  fs.writeFileSync(path.join(paths.musicDir, 'existing.mp3'), 'keep me');
  const catalog = {
    tracks: [
      { id: 'existing', ext: 'mp3', downloadUrl: 'https://example.test/existing', sha256: sha256('keep me') },
      { id: 'local', ext: 'mp3' },
      { id: 'missing-local', ext: 'ogg' },
      { id: 'first', ext: 'mp3', downloadUrl: 'https://example.test/first', sha256: sha256('https://example.test/first') },
      { id: 'broken', ext: 'mp3', downloadUrl: 'https://example.test/broken', sha256: sha256('unused') },
      { id: 'last', ext: 'wav', downloadUrl: 'https://example.test/last', sha256: sha256('https://example.test/last') },
      { id: '', ext: 'mp3', downloadUrl: 'https://example.test/invalid' },
      'not a track',
    ],
  };
  fs.writeFileSync(path.join(paths.bundledMusicDir, 'catalog.json'), JSON.stringify(catalog));
  const downloads: string[] = [];
  const logs: string[] = [];
  let activeDownloads = 0;
  let maximumConcurrentDownloads = 0;

  await provisionMusicLibrary({
    ...paths,
    signal: new AbortController().signal,
    logLine: (line) => logs.push(line),
    download: async (url, destination) => {
      downloads.push(url);
      activeDownloads += 1;
      maximumConcurrentDownloads = Math.max(maximumConcurrentDownloads, activeDownloads);
      await new Promise<void>((resolve) => setImmediate(resolve));
      try {
        if (url.endsWith('/broken')) throw new Error('offline');
        fs.writeFileSync(destination, url);
        return sha256(url);
      } finally {
        activeDownloads -= 1;
      }
    },
  });

  assert.deepEqual(downloads, [
    'https://example.test/first',
    'https://example.test/broken',
    'https://example.test/last',
  ]);
  assert.equal(maximumConcurrentDownloads, 1);
  assert.equal(fs.readFileSync(path.join(paths.musicDir, 'existing.mp3'), 'utf8'), 'keep me');
  assert.equal(fs.readFileSync(path.join(paths.musicDir, 'local.mp3'), 'utf8'), 'bundled audio');
  assert.equal(
    fs.readFileSync(path.join(paths.musicDir, 'first.mp3'), 'utf8'),
    'https://example.test/first',
  );
  assert.equal(
    fs.readFileSync(path.join(paths.musicDir, 'last.wav'), 'utf8'),
    'https://example.test/last',
  );
  assert.equal(fs.existsSync(path.join(paths.musicDir, 'broken.mp3')), false);
  assert.deepEqual(
    JSON.parse(fs.readFileSync(path.join(paths.musicDir, 'catalog.json'), 'utf8')),
    catalog,
  );
  assert.deepEqual(logs, [
    '[music] copied bundled local.mp3\n',
    '[music] skip missing-local: no downloadUrl and no bundled audio\n',
    '[music] downloaded first.mp3\n',
    '[music] skip broken: Error: offline\n',
    '[music] downloaded last.wav\n',
  ]);
});

test('stops before the next track when boot is cancelled', async (t) => {
  const paths = temporaryLibrary(t);
  fs.mkdirSync(paths.bundledMusicDir, { recursive: true });
  fs.writeFileSync(path.join(paths.bundledMusicDir, 'catalog.json'), JSON.stringify({
    tracks: [
      { id: 'first', ext: 'mp3', downloadUrl: 'https://example.test/first', sha256: sha256('https://example.test/first') },
      { id: 'second', ext: 'mp3', downloadUrl: 'https://example.test/second', sha256: sha256('https://example.test/second') },
    ],
  }));
  const controller = new AbortController();
  const downloads: string[] = [];

  await provisionMusicLibrary({
    ...paths,
    signal: controller.signal,
    logLine: () => {},
    download: async (url, destination) => {
      downloads.push(url);
      fs.writeFileSync(destination, url);
      controller.abort();
      return sha256(url);
    },
  });

  assert.deepEqual(downloads, ['https://example.test/first']);
  assert.equal(fs.existsSync(path.join(paths.musicDir, 'second.mp3')), false);
});

test('removes a downloaded remote track when its sha256 mismatches', async (t) => {
  const paths = temporaryLibrary(t);
  fs.mkdirSync(paths.bundledMusicDir, { recursive: true });
  fs.writeFileSync(path.join(paths.bundledMusicDir, 'catalog.json'), JSON.stringify({
    tracks: [{
      id: 'remote',
      ext: 'mp3',
      downloadUrl: 'https://example.test/remote',
      sha256: sha256('expected audio'),
    }],
  }));
  const logs: string[] = [];

  await provisionMusicLibrary({
    ...paths,
    signal: new AbortController().signal,
    logLine: (line) => logs.push(line),
    download: async (_url, destination) => {
      fs.writeFileSync(destination, 'substituted audio');
      return sha256('substituted audio');
    },
  });

  assert.equal(fs.existsSync(path.join(paths.musicDir, 'remote.mp3')), false);
  assert.deepEqual(logs, ['[music] skip remote: Error: sha256 mismatch\n']);
});

test('rejects a remote track without a valid sha256 and removes an unverified cached file', async (t) => {
  const paths = temporaryLibrary(t);
  fs.mkdirSync(paths.bundledMusicDir, { recursive: true });
  fs.mkdirSync(paths.musicDir, { recursive: true });
  fs.writeFileSync(path.join(paths.musicDir, 'remote.mp3'), 'legacy unverified audio');
  fs.writeFileSync(path.join(paths.bundledMusicDir, 'catalog.json'), JSON.stringify({
    tracks: [{ id: 'remote', ext: 'mp3', downloadUrl: 'https://example.test/remote' }],
  }));
  let downloads = 0;
  const logs: string[] = [];

  await provisionMusicLibrary({
    ...paths,
    signal: new AbortController().signal,
    logLine: (line) => logs.push(line),
    download: async () => {
      downloads += 1;
      return sha256('should not run');
    },
  });

  assert.equal(downloads, 0);
  assert.equal(fs.existsSync(path.join(paths.musicDir, 'remote.mp3')), false);
  assert.deepEqual(logs, ['[music] skip remote: remote track has no valid sha256\n']);
});

test('every remote track in the shipped catalog has a lowercase sha256', () => {
  const sourceFile = fileURLToPath(import.meta.url);
  const catalogPath = path.join(path.dirname(sourceFile), '..', '..', 'data', 'music', 'catalog.json');
  const catalog = JSON.parse(fs.readFileSync(catalogPath, 'utf8')) as { tracks?: unknown[] };
  const remoteTracks = (catalog.tracks ?? []).filter(
    (track): track is { id: unknown; downloadUrl: string; sha256?: unknown } =>
      typeof track === 'object' && track !== null && 'downloadUrl' in track
      && typeof track.downloadUrl === 'string' && track.downloadUrl !== '',
  );

  assert.ok(remoteTracks.length > 0);
  for (const track of remoteTracks) {
    assert.match(typeof track.sha256 === 'string' ? track.sha256 : '', /^[a-f0-9]{64}$/, String(track.id));
  }
});

test('the shipped catalog includes the verified viral CC0 pack', () => {
  const sourceFile = fileURLToPath(import.meta.url);
  const catalogPath = path.join(path.dirname(sourceFile), '..', '..', 'data', 'music', 'catalog.json');
  const catalog = JSON.parse(fs.readFileSync(catalogPath, 'utf8')) as { tracks?: unknown[] };
  const tracks = catalog.tracks ?? [];

  for (const id of VIRAL_CC0_TRACK_IDS) {
    const track = tracks.find((candidate): candidate is Record<string, unknown> =>
      typeof candidate === 'object' && candidate !== null && 'id' in candidate && candidate.id === id,
    );
    assert.ok(track, `missing viral track ${id}`);
    assert.equal(track.license, 'CC0', `${id} license`);
    assert.equal(track.attributionRequired, false, `${id} attributionRequired`);
    assert.equal(track.ext, 'mp3', `${id} ext`);
    assert.match(String(track.downloadUrl), /^https:\/\/archive\.org\/download\/freepd\//, `${id} downloadUrl`);
    assert.match(String(track.sha256), /^[a-f0-9]{64}$/, `${id} sha256`);
  }
});

test('logs an invalid catalog without blocking startup', async (t) => {
  const paths = temporaryLibrary(t);
  fs.mkdirSync(paths.bundledMusicDir, { recursive: true });
  fs.writeFileSync(path.join(paths.bundledMusicDir, 'catalog.json'), '{');
  const logs: string[] = [];

  await provisionMusicLibrary({
    ...paths,
    signal: new AbortController().signal,
    logLine: (line) => logs.push(line),
  });

  assert.equal(logs.length, 1);
  assert.match(logs[0] ?? '', /^\[music\] bad catalog\.json:/);
});
