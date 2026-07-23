import test from 'node:test';
import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import * as fs from 'node:fs';
import * as os from 'node:os';
import * as path from 'node:path';
import {
  provisionRuntimeTools,
  runtimeToolEnvironment,
  type RuntimeToolName,
  type RuntimeToolProvisioningOptions,
} from './runtime-tools.ts';

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
    hlae: path.join(toolsDir, 'hlae', '2.191.1', 'HLAE.exe'),
    ffmpeg: path.join(
      toolsDir,
      'ffmpeg',
      'n8.1.2-30-g45f1910444-20260723',
      'ffmpeg-n8.1-latest-win64-gpl-shared-8.1',
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
  writeCompleteMarker(path.join(toolsDir, 'hlae', '2.191.1'), 'hlae');
  writeCompleteMarker(path.join(toolsDir, 'ffmpeg', 'n8.1.2-30-g45f1910444-20260723'), 'ffmpeg');
  writeCompleteMarker(path.join(toolsDir, 'ytdlp', '2026.06.09'), 'ytdlp');
  const staleStaging = path.join(toolsDir, 'ytdlp', '2026.06.09.installing');
  fs.mkdirSync(staleStaging, { recursive: true });
  fs.writeFileSync(path.join(staleStaging, 'partial'), 'stale');
  const logs: string[] = [];
  const statuses: string[] = [];

  const env = await provisionRuntimeTools(
    withFixtureTrust({ toolsDir, logLine: (line) => logs.push(line), platform: 'win32' }),
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
  assert.equal(fs.existsSync(path.join(toolsDir, 'hlae', '2.191.1', '.fragforge-install.json')), true);
  assert.equal(fs.existsSync(path.join(toolsDir, 'ffmpeg', 'n8.1.2-30-g45f1910444-20260723', '.fragforge-install.json')), true);
  assert.equal(fs.existsSync(path.join(toolsDir, 'ytdlp', '2026.06.09', '.fragforge-install.json')), true);
  assert.equal(fs.existsSync(staleStaging), false);
});

test('retires markerless legacy tools instead of using them after a failed refresh', async (t) => {
  const toolsDir = fs.mkdtempSync(path.join(os.tmpdir(), 'fragforge-tools-'));
  t.after(() => fs.rmSync(toolsDir, { recursive: true, force: true }));
  seedLegacyHLAE(toolsDir);
  seedLegacyFFmpeg(toolsDir);
  seedLegacyYtdlp(toolsDir);
  const logs: string[] = [];
  const statuses: string[] = [];

  const env = await provisionRuntimeTools(
    withFixtureTrust({
      toolsDir,
      logLine: (line) => logs.push(line),
      platform: 'win32',
      download: async () => {
        throw new Error('offline');
      },
    }),
    (name) => statuses.push(name),
  );

  assert.deepEqual(env, {});
  assert.deepEqual(statuses.sort(), ['ffmpeg', 'hlae', 'ytdlp']);
  assert.equal(fs.existsSync(path.join(toolsDir, 'hlae', '2.191.1', '.fragforge-install.json')), false);
  assert.equal(fs.existsSync(path.join(toolsDir, 'ffmpeg', 'n8.1.2-30-g45f1910444-20260723', '.fragforge-install.json')), false);
  assert.equal(fs.existsSync(path.join(toolsDir, 'ytdlp', '2026.06.09', '.fragforge-install.json')), false);
  assert.equal(logs.filter((line) => line.includes('no valid per-file digest manifest')).length, 3);
  assert.equal(logs.filter((line) => line.includes('feature stays unconfigured')).length, 3);
});

test('installs uncached tools through staging and publishes only complete versions', async (t) => {
  const toolsDir = fs.mkdtempSync(path.join(os.tmpdir(), 'fragforge-tools-'));
  t.after(() => fs.rmSync(toolsDir, { recursive: true, force: true }));
  const statuses: string[] = [];
  const bundledHLAEArchive = path.join(toolsDir, 'bundled', 'hlae_2_191_1.zip');
  fs.mkdirSync(path.dirname(bundledHLAEArchive), { recursive: true });
  fs.writeFileSync(bundledHLAEArchive, 'bundled official archive fixture');

  const env = await provisionRuntimeTools(
    withFixtureTrust({
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
          'ffmpeg-n8.1-latest-win64-gpl-shared-8.1',
          'bin',
        );
        fs.mkdirSync(binDir, { recursive: true });
        fs.writeFileSync(path.join(binDir, 'ffmpeg.exe'), 'installed');
        fs.writeFileSync(path.join(binDir, 'ffprobe.exe'), 'installed');
      },
    }, {
      hlae: fixtureTreeDigest({ 'HLAE.exe': 'installed' }),
      ffmpeg: fixtureTreeDigest({
        'ffmpeg-n8.1-latest-win64-gpl-shared-8.1/bin/ffmpeg.exe': 'installed',
        'ffmpeg-n8.1-latest-win64-gpl-shared-8.1/bin/ffprobe.exe': 'installed',
      }),
      ytdlp: fixtureTreeDigest({ 'yt-dlp.exe': 'downloaded' }),
    }),
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
  const versions = { hlae: '2.191.1', ffmpeg: 'n8.1.2-30-g45f1910444-20260723', ytdlp: '2026.06.09' };
  for (const [name, version] of Object.entries(versions)) {
    const installDir = path.join(toolsDir, name, version);
    assert.equal(fs.existsSync(`${installDir}.installing`), false);
    assert.equal(fs.existsSync(`${installDir}.previous`), false);
    assert.equal(fs.existsSync(path.join(installDir, '.fragforge-install.json')), true);
    const marker = JSON.parse(fs.readFileSync(path.join(installDir, '.fragforge-install.json'), 'utf8'));
    assert.equal(marker.schemaVersion, 2);
    assert.ok(Array.isArray(marker.files));
    assert.ok(marker.files.length > 0);
  }
});

test('rehashes every cached file and refuses a modified executable when refresh fails', async (t) => {
  const toolsDir = fs.mkdtempSync(path.join(os.tmpdir(), 'fragforge-tools-'));
  t.after(() => fs.rmSync(toolsDir, { recursive: true, force: true }));
  seedCompleteHLAE(toolsDir);
  seedCompleteFFmpeg(toolsDir);
  seedCompleteYtdlp(toolsDir);
  const trustedTrees = fixtureTreeSha256(toolsDir);
  const ytdlp = path.join(toolsDir, 'ytdlp', '2026.06.09', 'yt-dlp.exe');
  fs.writeFileSync(ytdlp, 'tampered after installation');
  // Simulate an attacker updating the writable marker to match the replacement.
  // The code-pinned tree digest, not this marker, remains the root of trust.
  writeCompleteMarker(path.dirname(ytdlp), 'ytdlp');
  const logs: string[] = [];

  const env = await provisionRuntimeTools(withFixtureTrust({
    toolsDir,
    logLine: (line) => logs.push(line),
    platform: 'win32',
    download: async () => {
      throw new Error('offline');
    },
  }, trustedTrees));

  assert.equal(env.ZV_YTDLP_PATH, undefined);
  assert.equal(typeof env.ZV_HLAE_PATH, 'string');
  assert.equal(typeof env.ZV_FFMPEG_PATH, 'string');
  assert.match(logs.join(''), /ytdlp cache has no valid per-file digest manifest/);
});

test('migrates a legacy archive-only marker only through a fresh pinned installation', async (t) => {
  const toolsDir = fs.mkdtempSync(path.join(os.tmpdir(), 'fragforge-tools-'));
  t.after(() => fs.rmSync(toolsDir, { recursive: true, force: true }));
  seedCompleteHLAE(toolsDir);
  seedCompleteFFmpeg(toolsDir);
  seedLegacyYtdlp(toolsDir);
  const installDir = path.join(toolsDir, 'ytdlp', '2026.06.09');
  fs.writeFileSync(path.join(installDir, '.fragforge-install.json'), JSON.stringify({
    version: '2026.06.09',
    sha256: '3a48cb955d55c8821b60ccbdbbc6f61bc958f2f3d3b7ad5eaf3d83a543293a27',
  }));

  const env = await provisionRuntimeTools(withFixtureTrust({
    toolsDir,
    logLine: () => {},
    platform: 'win32',
    download: async (_url, destination) => {
      fs.writeFileSync(destination, 'fresh pinned ytdlp');
      return '3a48cb955d55c8821b60ccbdbbc6f61bc958f2f3d3b7ad5eaf3d83a543293a27';
    },
  }, {
    ytdlp: fixtureTreeDigest({ 'yt-dlp.exe': 'fresh pinned ytdlp' }),
  }));

  assert.equal(env.ZV_YTDLP_PATH, path.join(installDir, 'yt-dlp.exe'));
  const marker = JSON.parse(fs.readFileSync(path.join(installDir, '.fragforge-install.json'), 'utf8'));
  assert.equal(marker.schemaVersion, 2);
  assert.equal(marker.files[0].path, 'yt-dlp.exe');
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

  const env = await provisionRuntimeTools(withFixtureTrust({
    toolsDir,
    logLine: (line) => logs.push(line),
    platform: 'win32',
  }));

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

  const env = await provisionRuntimeTools(withFixtureTrust({
    toolsDir,
    logLine: (line) => logs.push(line),
    platform: 'win32',
    download: async (_url, destination) => {
      fs.writeFileSync(destination, 'untrusted');
      return 'wrong-digest';
    },
  }));

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

  const env = await provisionRuntimeTools(withFixtureTrust({
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
  }));

  assert.equal(env.ZV_FFMPEG_PATH, undefined);
  assert.equal(env.ZV_FFPROBE_PATH, undefined);
  assert.equal(fs.existsSync(path.join(toolsDir, 'ffmpeg', 'n8.1.2.installing')), false);
  assert.equal(fs.existsSync(path.join(toolsDir, 'ffmpeg', 'n8.1.2-30-g45f1910444-20260723')), false);
  assert.match(logs.join(''), /test extraction failure/);
});

test('aborts and cleans an installation that exceeds its time budget', async (t) => {
  const toolsDir = fs.mkdtempSync(path.join(os.tmpdir(), 'fragforge-tools-'));
  t.after(() => fs.rmSync(toolsDir, { recursive: true, force: true }));
  seedCompleteHLAE(toolsDir);
  seedCompleteFFmpeg(toolsDir);
  const logs: string[] = [];

  const env = await provisionRuntimeTools(withFixtureTrust({
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
  }));

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

  const provisioning = provisionRuntimeTools(withFixtureTrust({
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
  }));
  controller.abort();

  await assert.rejects(provisioning, /runtime tool provisioning aborted/);
  for (const [name, version] of Object.entries({ hlae: '2.191.1', ffmpeg: 'n8.1.2-30-g45f1910444-20260723', ytdlp: '2026.06.09' })) {
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

  const env = await provisionRuntimeTools(withFixtureTrust({
    toolsDir,
    logLine: () => {},
    platform: 'win32',
    download: async () => {
      throw new Error('recovery should not download');
    },
  }, {
    ytdlp: fixtureTreeDigest({ 'yt-dlp.exe': 'previous working install' }),
  }));

  assert.equal(env.ZV_YTDLP_PATH, path.join(installDir, 'yt-dlp.exe'));
  assert.equal(fs.existsSync(path.join(installDir, '.fragforge-install.json')), true);
  assert.equal(fs.existsSync(previousDir), false);
});

function digestFor(url: string): string {
  if (url.includes('advancedfx')) {
    return '307ba9170b151a7df9b7e5604b335c2d8b8df5bf5cb8d6700ae3fd01069da514';
  }
  if (url.includes('ffmpeg-n8.1-win64-gpl-shared')) {
    return 'c22260c1b2d5f2e499e5bb9c5ab32224ff6bf3da79beb7543a955b4b31a4c03c';
  }
  return '3a48cb955d55c8821b60ccbdbbc6f61bc958f2f3d3b7ad5eaf3d83a543293a27';
}

function seedLegacyHLAE(toolsDir: string): void {
  const executable = path.join(toolsDir, 'hlae', '2.191.1', 'HLAE.exe');
  fs.mkdirSync(path.dirname(executable), { recursive: true });
  fs.writeFileSync(executable, 'cached');
}

function seedCompleteHLAE(toolsDir: string): void {
  seedLegacyHLAE(toolsDir);
  writeCompleteMarker(path.join(toolsDir, 'hlae', '2.191.1'), 'hlae');
}

function seedLegacyFFmpeg(toolsDir: string): void {
  const binDir = path.join(
    toolsDir,
    'ffmpeg',
    'n8.1.2-30-g45f1910444-20260723',
    'ffmpeg-n8.1-latest-win64-gpl-shared-8.1',
    'bin',
  );
  fs.mkdirSync(binDir, { recursive: true });
  fs.writeFileSync(path.join(binDir, 'ffmpeg.exe'), 'cached');
  fs.writeFileSync(path.join(binDir, 'ffprobe.exe'), 'cached');
}

function seedCompleteFFmpeg(toolsDir: string): void {
  seedLegacyFFmpeg(toolsDir);
  writeCompleteMarker(path.join(toolsDir, 'ffmpeg', 'n8.1.2-30-g45f1910444-20260723'), 'ffmpeg');
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
    version: '2.191.1',
    sha256: '307ba9170b151a7df9b7e5604b335c2d8b8df5bf5cb8d6700ae3fd01069da514',
  },
  ffmpeg: {
    version: 'n8.1.2-30-g45f1910444-20260723',
    sha256: 'c22260c1b2d5f2e499e5bb9c5ab32224ff6bf3da79beb7543a955b4b31a4c03c',
  },
  ytdlp: {
    version: '2026.06.09',
    sha256: '3a48cb955d55c8821b60ccbdbbc6f61bc958f2f3d3b7ad5eaf3d83a543293a27',
  },
} as const;

function writeCompleteMarker(installDir: string, name: keyof typeof MARKERS): void {
  const files = collectFixtureFiles(installDir).map((relativePath) => ({
    path: relativePath,
    sha256: createFixtureDigest(path.join(installDir, ...relativePath.split('/'))),
  }));
  fs.writeFileSync(path.join(installDir, '.fragforge-install.json'), JSON.stringify({
    files,
    schemaVersion: 2,
    sourceSha256: MARKERS[name].sha256,
    version: MARKERS[name].version,
  }));
}

function collectFixtureFiles(directory: string, relativeDirectory = ''): string[] {
  const files: string[] = [];
  for (const entry of fs.readdirSync(directory, { withFileTypes: true })) {
    if (entry.name === '.fragforge-install.json') continue;
    const relativePath = relativeDirectory === '' ? entry.name : `${relativeDirectory}/${entry.name}`;
    if (entry.isDirectory()) {
      files.push(...collectFixtureFiles(path.join(directory, entry.name), relativePath));
    } else if (entry.isFile()) {
      files.push(relativePath);
    }
  }
  return files.sort();
}

function createFixtureDigest(filePath: string): string {
  return createHash('sha256').update(fs.readFileSync(filePath)).digest('hex');
}

function withFixtureTrust(
  options: RuntimeToolProvisioningOptions,
  overrides: Partial<Record<RuntimeToolName, string>> = {},
): RuntimeToolProvisioningOptions {
  return {
    ...options,
    testOnlyTreeSha256: {
      ...fixtureTreeSha256(options.toolsDir),
      ...overrides,
    },
  };
}

function fixtureTreeSha256(toolsDir: string): Partial<Record<RuntimeToolName, string>> {
  const directories: Record<RuntimeToolName, string> = {
    hlae: path.join(toolsDir, 'hlae', '2.191.1'),
    ffmpeg: path.join(toolsDir, 'ffmpeg', 'n8.1.2-30-g45f1910444-20260723'),
    ytdlp: path.join(toolsDir, 'ytdlp', '2026.06.09'),
  };
  const result: Partial<Record<RuntimeToolName, string>> = {};
  for (const [name, directory] of Object.entries(directories) as [RuntimeToolName, string][]) {
    if (!fs.existsSync(directory)) continue;
    const entries = Object.fromEntries(
      collectFixtureFiles(directory).map((relativePath) => [
        relativePath,
        fs.readFileSync(path.join(directory, ...relativePath.split('/'))),
      ]),
    );
    result[name] = fixtureTreeDigest(entries);
  }
  return result;
}

function fixtureTreeDigest(entries: Record<string, string | Buffer>): string {
  const tree = createHash('sha256');
  for (const relativePath of Object.keys(entries).sort((left, right) => left.localeCompare(right))) {
    const digest = createHash('sha256').update(entries[relativePath]).digest('hex');
    tree.update(relativePath, 'utf8');
    tree.update('\0', 'utf8');
    tree.update(digest, 'utf8');
    tree.update('\n', 'utf8');
  }
  return tree.digest('hex');
}
