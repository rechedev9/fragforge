import test from 'node:test';
import assert from 'node:assert/strict';
import * as fs from 'node:fs';
import * as os from 'node:os';
import * as path from 'node:path';
import { provisionRuntimeTools, runtimeToolEnvironment } from './runtime-tools.ts';

test('maps resolved runtime tools to the orchestrator environment', (t) => {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'fragforge-tools-'));
  t.after(() => fs.rmSync(directory, { recursive: true, force: true }));
  const ffmpeg = path.join(directory, 'ffmpeg.exe');
  fs.writeFileSync(path.join(directory, 'ffprobe.exe'), '');

  assert.deepEqual(runtimeToolEnvironment({
    hlae: 'C:\\tools\\HLAE.exe',
    ffmpeg,
    ytdlp: 'C:\\tools\\yt-dlp.exe',
  }), {
    ZV_HLAE_PATH: 'C:\\tools\\HLAE.exe',
    ZV_FFMPEG_PATH: ffmpeg,
    ZV_FFPROBE_PATH: path.join(directory, 'ffprobe.exe'),
    ZV_YTDLP_PATH: 'C:\\tools\\yt-dlp.exe',
  });
});

test('omits missing and unavailable runtime tools', () => {
  assert.deepEqual(runtimeToolEnvironment({ hlae: '', ffmpeg: '', ytdlp: '' }), {});
});

test('skips Windows runtime provisioning on other platforms', async () => {
  const env = await provisionRuntimeTools({
    toolsDir: 'unused',
    logLine: () => {},
    platform: 'linux',
  });
  assert.deepEqual(env, {});
});

test('reuses complete cached installations without download work', async (t) => {
  const toolsDir = fs.mkdtempSync(path.join(os.tmpdir(), 'fragforge-tools-'));
  t.after(() => fs.rmSync(toolsDir, { recursive: true, force: true }));
  const paths = {
    hlae: path.join(toolsDir, 'hlae', '2.191.0', 'HLAE.exe'),
    ffmpeg: path.join(
      toolsDir,
      'ffmpeg',
      'n8.1.2',
      'ffmpeg-n8.1.2-21-gce3c09c101-win64-gpl-shared-8.1',
      'bin',
      'ffmpeg.exe',
    ),
    ytdlp: path.join(toolsDir, 'ytdlp', '2026.06.09', 'yt-dlp.exe'),
  };
  for (const executable of Object.values(paths)) {
    fs.mkdirSync(path.dirname(executable), { recursive: true });
    fs.writeFileSync(executable, 'cached');
  }
  const ffprobe = path.join(path.dirname(paths.ffmpeg), 'ffprobe.exe');
  fs.writeFileSync(ffprobe, 'cached');
  writeCompleteMarker(path.join(toolsDir, 'hlae', '2.191.0'), 'hlae');
  writeCompleteMarker(path.join(toolsDir, 'ffmpeg', 'n8.1.2'), 'ffmpeg');
  writeCompleteMarker(path.join(toolsDir, 'ytdlp', '2026.06.09'), 'ytdlp');
  const staleStaging = path.join(toolsDir, 'ytdlp', '2026.06.09.installing');
  fs.mkdirSync(staleStaging, { recursive: true });
  fs.writeFileSync(path.join(staleStaging, 'partial'), 'stale');
  const logs: string[] = [];
  const statuses: string[] = [];

  const env = await provisionRuntimeTools(
    { toolsDir, logLine: (line) => logs.push(line), platform: 'win32' },
    (name) => statuses.push(name),
  );

  assert.deepEqual(env, {
    ZV_HLAE_PATH: paths.hlae,
    ZV_FFMPEG_PATH: paths.ffmpeg,
    ZV_FFPROBE_PATH: ffprobe,
    ZV_YTDLP_PATH: paths.ytdlp,
  });
  assert.deepEqual(logs, []);
  assert.deepEqual(statuses, []);
  assert.equal(fs.existsSync(path.join(toolsDir, 'hlae', '2.191.0', '.fragforge-install.json')), true);
  assert.equal(fs.existsSync(path.join(toolsDir, 'ffmpeg', 'n8.1.2', '.fragforge-install.json')), true);
  assert.equal(fs.existsSync(path.join(toolsDir, 'ytdlp', '2026.06.09', '.fragforge-install.json')), true);
  assert.equal(fs.existsSync(staleStaging), false);
});

test('keeps markerless legacy tools only as an offline fallback', async (t) => {
  const toolsDir = fs.mkdtempSync(path.join(os.tmpdir(), 'fragforge-tools-'));
  t.after(() => fs.rmSync(toolsDir, { recursive: true, force: true }));
  seedLegacyHLAE(toolsDir);
  seedLegacyFFmpeg(toolsDir);
  seedLegacyYtdlp(toolsDir);
  const logs: string[] = [];
  const statuses: string[] = [];

  const env = await provisionRuntimeTools(
    {
      toolsDir,
      logLine: (line) => logs.push(line),
      platform: 'win32',
      download: async () => {
        throw new Error('offline');
      },
    },
    (name) => statuses.push(name),
  );

  assert.equal(typeof env.ZV_HLAE_PATH, 'string');
  assert.equal(typeof env.ZV_FFMPEG_PATH, 'string');
  assert.equal(typeof env.ZV_YTDLP_PATH, 'string');
  assert.deepEqual(statuses.sort(), ['ffmpeg', 'hlae', 'ytdlp']);
  assert.equal(fs.existsSync(path.join(toolsDir, 'hlae', '2.191.0', '.fragforge-install.json')), false);
  assert.equal(fs.existsSync(path.join(toolsDir, 'ffmpeg', 'n8.1.2', '.fragforge-install.json')), false);
  assert.equal(fs.existsSync(path.join(toolsDir, 'ytdlp', '2026.06.09', '.fragforge-install.json')), false);
  assert.equal(logs.filter((line) => line.includes('using markerless legacy install until retry')).length, 3);
});

test('installs uncached tools through staging and publishes only complete versions', async (t) => {
  const toolsDir = fs.mkdtempSync(path.join(os.tmpdir(), 'fragforge-tools-'));
  t.after(() => fs.rmSync(toolsDir, { recursive: true, force: true }));
  const statuses: string[] = [];
  const bundledHLAEArchive = path.join(toolsDir, 'bundled', 'hlae_2_191_0.zip');
  fs.mkdirSync(path.dirname(bundledHLAEArchive), { recursive: true });
  fs.writeFileSync(bundledHLAEArchive, 'bundled official archive fixture');

  const env = await provisionRuntimeTools(
    {
      toolsDir,
      bundledHLAEArchive,
      logLine: () => {},
      platform: 'win32',
      download: async (url, destination, { onProgress } = {}) => {
        assert.doesNotMatch(url, /advancedfx/);
        fs.writeFileSync(destination, 'downloaded');
        onProgress?.(10, 10);
        return digestFor(url);
      },
      sha256File: () => digestFor('advancedfx'),
      extractArchive: async (_archive, destination) => {
        if (destination.includes(`${path.sep}hlae${path.sep}`)) {
          fs.writeFileSync(path.join(destination, 'HLAE.exe'), 'installed');
          return;
        }
        const binDir = path.join(
          destination,
          'ffmpeg-n8.1.2-21-gce3c09c101-win64-gpl-shared-8.1',
          'bin',
        );
        fs.mkdirSync(binDir, { recursive: true });
        fs.writeFileSync(path.join(binDir, 'ffmpeg.exe'), 'installed');
        fs.writeFileSync(path.join(binDir, 'ffprobe.exe'), 'installed');
      },
    },
    (name, detail) => statuses.push(`${name}:${detail ?? 'start'}`),
  );

  assert.equal(typeof env.ZV_HLAE_PATH, 'string');
  assert.equal(typeof env.ZV_FFMPEG_PATH, 'string');
  assert.equal(typeof env.ZV_FFPROBE_PATH, 'string');
  assert.equal(typeof env.ZV_YTDLP_PATH, 'string');
  assert.deepEqual(
    statuses.filter((status) => status.endsWith(':start')).sort(),
    ['ffmpeg:start', 'hlae:start', 'ytdlp:start'],
  );
  const versions = { hlae: '2.191.0', ffmpeg: 'n8.1.2', ytdlp: '2026.06.09' };
  for (const [name, version] of Object.entries(versions)) {
    const installDir = path.join(toolsDir, name, version);
    assert.equal(fs.existsSync(`${installDir}.installing`), false);
    assert.equal(fs.existsSync(`${installDir}.previous`), false);
    assert.equal(fs.existsSync(path.join(installDir, '.fragforge-install.json')), true);
  }
});

test('removes obsolete versioned HLAE caches after the pinned version is ready', async (t) => {
  const toolsDir = fs.mkdtempSync(path.join(os.tmpdir(), 'fragforge-tools-'));
  t.after(() => fs.rmSync(toolsDir, { recursive: true, force: true }));
  seedCompleteHLAE(toolsDir);
  seedCompleteFFmpeg(toolsDir);
  seedCompleteYtdlp(toolsDir);
  const obsolete = path.join(toolsDir, 'hlae', '2.190.2');
  const unrelated = path.join(toolsDir, 'hlae', 'manual-backup');
  fs.mkdirSync(obsolete, { recursive: true });
  fs.mkdirSync(unrelated, { recursive: true });
  fs.writeFileSync(path.join(obsolete, 'HLAE.exe'), 'old');
  const logs: string[] = [];

  const env = await provisionRuntimeTools({
    toolsDir,
    logLine: (line) => logs.push(line),
    platform: 'win32',
  });

  assert.equal(typeof env.ZV_HLAE_PATH, 'string');
  assert.equal(fs.existsSync(obsolete), false);
  assert.equal(fs.existsSync(unrelated), true);
  assert.match(logs.join(''), /removed obsolete HLAE 2\.190\.2/);
});

test('rejects a hash mismatch without publishing the failed tool', async (t) => {
  const toolsDir = fs.mkdtempSync(path.join(os.tmpdir(), 'fragforge-tools-'));
  t.after(() => fs.rmSync(toolsDir, { recursive: true, force: true }));
  seedCompleteHLAE(toolsDir);
  seedCompleteFFmpeg(toolsDir);
  const logs: string[] = [];

  const env = await provisionRuntimeTools({
    toolsDir,
    logLine: (line) => logs.push(line),
    platform: 'win32',
    download: async (_url, destination) => {
      fs.writeFileSync(destination, 'untrusted');
      return 'wrong-digest';
    },
  });

  assert.equal(env.ZV_YTDLP_PATH, undefined);
  assert.equal(typeof env.ZV_HLAE_PATH, 'string');
  assert.equal(typeof env.ZV_FFMPEG_PATH, 'string');
  assert.equal(fs.existsSync(path.join(toolsDir, 'ytdlp', '2026.06.09')), false);
  assert.match(logs.join(''), /sha256 mismatch/);
});

test('cleans staging when archive extraction fails', async (t) => {
  const toolsDir = fs.mkdtempSync(path.join(os.tmpdir(), 'fragforge-tools-'));
  t.after(() => fs.rmSync(toolsDir, { recursive: true, force: true }));
  seedCompleteHLAE(toolsDir);
  seedCompleteYtdlp(toolsDir);
  const logs: string[] = [];

  const env = await provisionRuntimeTools({
    toolsDir,
    logLine: (line) => logs.push(line),
    platform: 'win32',
    download: async (url, destination) => {
      fs.writeFileSync(destination, 'archive');
      return digestFor(url);
    },
    extractArchive: async () => {
      throw new Error('test extraction failure');
    },
  });

  assert.equal(env.ZV_FFMPEG_PATH, undefined);
  assert.equal(env.ZV_FFPROBE_PATH, undefined);
  assert.equal(fs.existsSync(path.join(toolsDir, 'ffmpeg', 'n8.1.2.installing')), false);
  assert.equal(fs.existsSync(path.join(toolsDir, 'ffmpeg', 'n8.1.2')), false);
  assert.match(logs.join(''), /test extraction failure/);
});

test('aborts and cleans an installation that exceeds its time budget', async (t) => {
  const toolsDir = fs.mkdtempSync(path.join(os.tmpdir(), 'fragforge-tools-'));
  t.after(() => fs.rmSync(toolsDir, { recursive: true, force: true }));
  seedCompleteHLAE(toolsDir);
  seedCompleteFFmpeg(toolsDir);
  const logs: string[] = [];

  const env = await provisionRuntimeTools({
    toolsDir,
    logLine: (line) => logs.push(line),
    platform: 'win32',
    maxInstallTimeMs: 5,
    download: async (_url, _destination, { signal } = {}) =>
      new Promise<string>((_resolve, reject) => {
        if (!signal) {
          reject(new Error('missing abort signal'));
          return;
        }
        const abort = (): void => reject(new Error('download aborted'));
        if (signal.aborted) {
          abort();
        } else {
          signal.addEventListener('abort', abort, { once: true });
        }
      }),
  });

  assert.equal(env.ZV_YTDLP_PATH, undefined);
  assert.equal(fs.existsSync(path.join(toolsDir, 'ytdlp', '2026.06.09.installing')), false);
  assert.match(logs.join(''), /timed out after 5ms/);
});

test('caller cancellation aborts work instead of activating legacy fallbacks', async (t) => {
  const toolsDir = fs.mkdtempSync(path.join(os.tmpdir(), 'fragforge-tools-'));
  t.after(() => fs.rmSync(toolsDir, { recursive: true, force: true }));
  seedLegacyHLAE(toolsDir);
  seedLegacyFFmpeg(toolsDir);
  seedLegacyYtdlp(toolsDir);
  const controller = new AbortController();

  const provisioning = provisionRuntimeTools({
    toolsDir,
    logLine: () => {},
    platform: 'win32',
    signal: controller.signal,
    download: async (_url, _destination, { signal } = {}) =>
      new Promise<string>((_resolve, reject) => {
        if (!signal) {
          reject(new Error('missing abort signal'));
          return;
        }
        const abort = (): void => reject(new Error('download aborted'));
        if (signal.aborted) {
          abort();
        } else {
          signal.addEventListener('abort', abort, { once: true });
        }
      }),
  });
  controller.abort();

  await assert.rejects(provisioning, /runtime tool provisioning aborted/);
  for (const [name, version] of Object.entries({ hlae: '2.191.0', ffmpeg: 'n8.1.2', ytdlp: '2026.06.09' })) {
    assert.equal(fs.existsSync(path.join(toolsDir, name, `${version}.installing`)), false);
  }
});

test('restores an install interrupted during atomic publication', async (t) => {
  const toolsDir = fs.mkdtempSync(path.join(os.tmpdir(), 'fragforge-tools-'));
  t.after(() => fs.rmSync(toolsDir, { recursive: true, force: true }));
  seedCompleteHLAE(toolsDir);
  seedCompleteFFmpeg(toolsDir);
  const installDir = path.join(toolsDir, 'ytdlp', '2026.06.09');
  const previousDir = `${installDir}.previous`;
  fs.mkdirSync(previousDir, { recursive: true });
  fs.writeFileSync(path.join(previousDir, 'yt-dlp.exe'), 'previous working install');
  writeCompleteMarker(previousDir, 'ytdlp');

  const env = await provisionRuntimeTools({
    toolsDir,
    logLine: () => {},
    platform: 'win32',
    download: async () => {
      throw new Error('recovery should not download');
    },
  });

  assert.equal(env.ZV_YTDLP_PATH, path.join(installDir, 'yt-dlp.exe'));
  assert.equal(fs.existsSync(path.join(installDir, '.fragforge-install.json')), true);
  assert.equal(fs.existsSync(previousDir), false);
});

function digestFor(url: string): string {
  if (url.includes('advancedfx')) {
    return '78efa377a2bac9522c3771a79c2503fec57e106432fc11d32244fe25b7c5b6cc';
  }
  if (url.includes('FFmpeg-Builds')) {
    return 'e0337e822bc66d01747bfa917080561739252aaceef3bccc049bcb299d6f9be0';
  }
  return '3a48cb955d55c8821b60ccbdbbc6f61bc958f2f3d3b7ad5eaf3d83a543293a27';
}

function seedLegacyHLAE(toolsDir: string): void {
  const executable = path.join(toolsDir, 'hlae', '2.191.0', 'HLAE.exe');
  fs.mkdirSync(path.dirname(executable), { recursive: true });
  fs.writeFileSync(executable, 'cached');
}

function seedCompleteHLAE(toolsDir: string): void {
  seedLegacyHLAE(toolsDir);
  writeCompleteMarker(path.join(toolsDir, 'hlae', '2.191.0'), 'hlae');
}

function seedLegacyFFmpeg(toolsDir: string): void {
  const binDir = path.join(
    toolsDir,
    'ffmpeg',
    'n8.1.2',
    'ffmpeg-n8.1.2-21-gce3c09c101-win64-gpl-shared-8.1',
    'bin',
  );
  fs.mkdirSync(binDir, { recursive: true });
  fs.writeFileSync(path.join(binDir, 'ffmpeg.exe'), 'cached');
  fs.writeFileSync(path.join(binDir, 'ffprobe.exe'), 'cached');
}

function seedCompleteFFmpeg(toolsDir: string): void {
  seedLegacyFFmpeg(toolsDir);
  writeCompleteMarker(path.join(toolsDir, 'ffmpeg', 'n8.1.2'), 'ffmpeg');
}

function seedLegacyYtdlp(toolsDir: string): void {
  const executable = path.join(toolsDir, 'ytdlp', '2026.06.09', 'yt-dlp.exe');
  fs.mkdirSync(path.dirname(executable), { recursive: true });
  fs.writeFileSync(executable, 'cached');
}


function seedCompleteYtdlp(toolsDir: string): void {
  seedLegacyYtdlp(toolsDir);
  writeCompleteMarker(path.join(toolsDir, 'ytdlp', '2026.06.09'), 'ytdlp');
}

const MARKERS = {
  hlae: {
    version: '2.191.0',
    sha256: '78efa377a2bac9522c3771a79c2503fec57e106432fc11d32244fe25b7c5b6cc',
  },
  ffmpeg: {
    version: 'n8.1.2',
    sha256: 'e0337e822bc66d01747bfa917080561739252aaceef3bccc049bcb299d6f9be0',
  },
  ytdlp: {
    version: '2026.06.09',
    sha256: '3a48cb955d55c8821b60ccbdbbc6f61bc958f2f3d3b7ad5eaf3d83a543293a27',
  },
} as const;

function writeCompleteMarker(installDir: string, name: keyof typeof MARKERS): void {
  fs.writeFileSync(path.join(installDir, '.fragforge-install.json'), JSON.stringify(MARKERS[name]));
}
