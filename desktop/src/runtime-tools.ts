import { spawn } from 'node:child_process';
import { createHash } from 'node:crypto';
import * as fs from 'node:fs';
import * as path from 'node:path';
import { downloadFile } from './http-download.ts';
import { psQuote } from './escaping.ts';
import { PINNED_HLAE_TOOL } from './hlae-tool.ts';

export type RuntimeToolName = 'hlae' | 'ffmpeg' | 'ytdlp';

export type RuntimeToolStatusReporter = (name: RuntimeToolName, detail?: string) => void;

export interface RuntimeToolProvisioningOptions {
  toolsDir: string;
  logLine: (text: string) => void;
  platform?: NodeJS.Platform;
  signal?: AbortSignal;
  maxInstallTimeMs?: number;
  bundledHLAEArchive?: string;
  sha256File?: (filePath: string) => string;
  testOnlyTreeSha256?: Partial<Record<RuntimeToolName, string>>;
  download?: typeof downloadFile;
  extractArchive?: (archive: string, destination: string, signal: AbortSignal) => Promise<void>;
}

export interface RuntimeToolPaths {
  hlae: string;
  ffmpeg: string;
  ytdlp: string;
}

export interface RuntimeToolEnvironment {
  ZV_HLAE_PATH?: string;
  ZV_FFMPEG_PATH?: string;
  ZV_FFPROBE_PATH?: string;
  ZV_YTDLP_PATH?: string;
}

interface RuntimeToolSpec {
  version: string;
  url: string;
  sha256: string;
  treeSha256: string;
  kind: 'zip' | 'exe';
  exeRel: string;
  requiredRel: readonly string[];
  timeoutMs: number;
}

interface RuntimeToolFileDigest {
  path: string;
  sha256: string;
}

interface RuntimeToolInstallMarker {
  files: readonly RuntimeToolFileDigest[];
  schemaVersion: 2;
  sourceSha256: string;
  version: string;
}

const FFMPEG_RELEASE_DIR = 'ffmpeg-n8.1-latest-win64-gpl-shared-8.1';
const FFMPEG_EXE = path.join(FFMPEG_RELEASE_DIR, 'bin', 'ffmpeg.exe');
const FFPROBE_EXE = path.join(FFMPEG_RELEASE_DIR, 'bin', 'ffprobe.exe');

// Runtime tools are pinned and installed below userData. HLAE is sourced from
// the installer bundle when present; larger tools retain verified downloads.
// Explicit paths keep the desktop boot boundary independent from host PATH.
const RUNTIME_TOOLS: Record<RuntimeToolName, RuntimeToolSpec> = {
  hlae: {
    ...PINNED_HLAE_TOOL,
    requiredRel: [PINNED_HLAE_TOOL.exeRel],
  },
  ffmpeg: {
    version: 'n8.1.2-30-g45f1910444-20260723',
    url: 'https://github.com/rechedev9/fragforge/releases/download/v2.2.12/ffmpeg-n8.1-win64-gpl-shared.zip',
    sha256: 'c22260c1b2d5f2e499e5bb9c5ab32224ff6bf3da79beb7543a955b4b31a4c03c',
    treeSha256: '8f5c301e5f090feee829b23b0ba1bb478d6377f7d8778e38f50b50f613bfa53e',
    kind: 'zip',
    exeRel: FFMPEG_EXE,
    requiredRel: [FFMPEG_EXE, FFPROBE_EXE],
    timeoutMs: 300_000,
  },
  ytdlp: {
    version: '2026.06.09',
    url: 'https://github.com/yt-dlp/yt-dlp/releases/download/2026.06.09/yt-dlp.exe',
    sha256: '3a48cb955d55c8821b60ccbdbbc6f61bc958f2f3d3b7ad5eaf3d83a543293a27',
    treeSha256: 'ebfc17314ddb5f84e52a223824c5659e92afa6c3934dfc8fdaea5d17c2303397',
    kind: 'exe',
    exeRel: 'yt-dlp.exe',
    requiredRel: ['yt-dlp.exe'],
    timeoutMs: 90_000,
  },
};

export const RUNTIME_TOOL_LABELS: Record<RuntimeToolName, string> = {
  hlae: 'HLAE',
  ffmpeg: 'FFmpeg',
  ytdlp: 'yt-dlp',
};

const INSTALL_MARKER = '.fragforge-install.json';
const INSTALL_MARKER_SCHEMA_VERSION = 2;
const SHA256_PATTERN = /^[a-f0-9]{64}$/;
const PROGRESS_REPORT_MIN_INTERVAL_MS = 1000;

/**
 * Installs every required runtime tool concurrently and returns the environment
 * variables understood by zv-orchestrator. Cached installations do no network
 * work; failed installations are omitted so the orchestrator can auto-detect.
 */
export async function provisionRuntimeTools(
  options: RuntimeToolProvisioningOptions,
  onStatus?: RuntimeToolStatusReporter,
): Promise<RuntimeToolEnvironment> {
  if ((options.platform ?? process.platform) !== 'win32') return {};
  throwIfProvisioningAborted(options.signal);

  const [hlae, ffmpeg, ytdlp] = await Promise.all([
    provisionRuntimeTool(options, 'hlae', onStatus),
    provisionRuntimeTool(options, 'ffmpeg', onStatus),
    provisionRuntimeTool(options, 'ytdlp', onStatus),
  ]);
  throwIfProvisioningAborted(options.signal);
  if (hlae) cleanupObsoleteHLAEVersions(options.toolsDir, options.logLine);
  return runtimeToolEnvironment({ hlae, ffmpeg, ytdlp });
}

/** Converts resolved executable paths into the orchestrator's tool contract. */
export function runtimeToolEnvironment(paths: RuntimeToolPaths): RuntimeToolEnvironment {
  const env: RuntimeToolEnvironment = {};
  if (paths.hlae) env.ZV_HLAE_PATH = paths.hlae;
  if (paths.ffmpeg) {
    env.ZV_FFMPEG_PATH = paths.ffmpeg;
    const ffprobe = path.join(path.dirname(paths.ffmpeg), 'ffprobe.exe');
    if (fs.existsSync(ffprobe)) env.ZV_FFPROBE_PATH = ffprobe;
  }
  if (paths.ytdlp) env.ZV_YTDLP_PATH = paths.ytdlp;
  return env;
}

async function provisionRuntimeTool(
  options: RuntimeToolProvisioningOptions,
  name: RuntimeToolName,
  onStatus?: RuntimeToolStatusReporter,
): Promise<string> {
  const tool = RUNTIME_TOOLS[name];
  const installDir = path.join(options.toolsDir, name, tool.version);
  const executable = path.join(installDir, tool.exeRel);
  throwIfProvisioningAborted(options.signal);
  try {
    restoreInterruptedPromotion(installDir);
    cleanupStagingInstall(installDir, options.logLine);
    if (await completeInstall(installDir, tool, trustedTreeSha256(options, name, tool))) {
      cleanupPreviousInstall(installDir, options.logLine);
      return executable;
    }
    if (requiredFilesExist(installDir, tool)) {
      options.logLine(
        `[tools] ${name} cache has no valid per-file digest manifest; replacing it from the pinned source\n`,
      );
    }
  } catch (err) {
    options.logLine(`[tools] ${name} cache inspection failed: ${String(err)}\n`);
    return '';
  }

  throwIfProvisioningAborted(options.signal);
  onStatus?.(name);
  const controller = new AbortController();
  const abortFromCaller = (): void => controller.abort();
  if (options.signal?.aborted) controller.abort();
  else options.signal?.addEventListener('abort', abortFromCaller, { once: true });
  const timeoutMs = Math.max(1, Math.min(tool.timeoutMs, options.maxInstallTimeMs ?? tool.timeoutMs));
  let timedOut = false;
  const timeout = setTimeout(() => {
    timedOut = true;
    controller.abort();
  }, timeoutMs);

  try {
    return await installRuntimeTool(
      options,
      name,
      tool,
      installDir,
      controller.signal,
      progressReporter(name, onStatus),
    );
  } catch (err) {
    if (options.signal?.aborted) throw new Error('runtime tool provisioning aborted');
    const reason = timedOut ? `timed out after ${timeoutMs}ms` : String(err);
    options.logLine(
      `[tools] ${name} provisioning failed (feature stays unconfigured, retried next boot): ${reason}\n`,
    );
    return '';
  } finally {
    clearTimeout(timeout);
    options.signal?.removeEventListener('abort', abortFromCaller);
  }
}

function throwIfProvisioningAborted(signal?: AbortSignal): void {
  if (signal?.aborted) throw new Error('runtime tool provisioning aborted');
}

function progressReporter(
  name: RuntimeToolName,
  onStatus?: RuntimeToolStatusReporter,
): (received: number, total: number | undefined) => void {
  let lastReportAt = 0;
  let lastPercentage = -1;

  return (received, total): void => {
    if (!onStatus) return;
    const now = Date.now();
    if (now - lastReportAt < PROGRESS_REPORT_MIN_INTERVAL_MS) return;

    if (total) {
      const percentage = Math.floor((received / total) * 100);
      if (percentage === lastPercentage) return;
      lastPercentage = percentage;
      lastReportAt = now;
      onStatus(name, `${percentage}%`);
      return;
    }

    lastReportAt = now;
    onStatus(name, `${(received / (1024 * 1024)).toFixed(0)} MB`);
  };
}

async function installRuntimeTool(
  options: RuntimeToolProvisioningOptions,
  name: RuntimeToolName,
  tool: RuntimeToolSpec,
  installDir: string,
  signal: AbortSignal,
  onProgress: (received: number, total: number | undefined) => void,
): Promise<string> {
  const stagingDir = `${installDir}.installing`;
  fs.rmSync(stagingDir, { recursive: true, force: true });
  fs.mkdirSync(stagingDir, { recursive: true });
  const partialDownload = path.join(stagingDir, 'download.part');
  const archive = path.join(stagingDir, 'download.zip');

  try {
    const bundledArchive = name === 'hlae' ? options.bundledHLAEArchive : undefined;
    let digest: string;
    if (bundledArchive && fs.existsSync(bundledArchive)) {
      options.logLine(`[tools] installing ${name} ${tool.version} from bundled archive...\n`);
      digest = copyBundledArchive(
        bundledArchive,
        partialDownload,
        signal,
        onProgress,
        options.sha256File,
      );
    } else {
      if (bundledArchive) {
        options.logLine(`[tools] bundled ${name} archive missing; falling back to verified download\n`);
      }
      options.logLine(`[tools] downloading ${name} ${tool.version}...\n`);
      digest = await (options.download ?? downloadFile)(tool.url, partialDownload, { signal, onProgress });
    }
    if (digest !== tool.sha256) {
      throw new Error(`sha256 mismatch: got ${digest}, want ${tool.sha256}`);
    }

    if (tool.kind === 'exe') {
      const stagedExecutable = path.join(stagingDir, tool.exeRel);
      fs.mkdirSync(path.dirname(stagedExecutable), { recursive: true });
      fs.renameSync(partialDownload, stagedExecutable);
    } else {
      fs.renameSync(partialDownload, archive);
      await (options.extractArchive ?? expandArchive)(archive, stagingDir, signal);
      fs.rmSync(archive, { force: true });
    }

    if (!requiredFilesExist(stagingDir, tool)) {
      throw new Error(`installation is missing required files in ${stagingDir}`);
    }
    const installedTreeSha256 = await sha256InstallTree(stagingDir);
    const expectedTreeSha256 = trustedTreeSha256(options, name, tool);
    if (installedTreeSha256 !== expectedTreeSha256) {
      throw new Error(
        `installed file tree sha256 mismatch: got ${installedTreeSha256}, want ${expectedTreeSha256}`,
      );
    }
    await writeInstallMarker(stagingDir, tool);
    promoteInstall(stagingDir, installDir, options.logLine);

    const executable = path.join(installDir, tool.exeRel);
    options.logLine(`[tools] installed ${executable}\n`);
    return executable;
  } finally {
    fs.rmSync(stagingDir, { recursive: true, force: true });
  }
}

function copyBundledArchive(
  source: string,
  destination: string,
  signal: AbortSignal,
  onProgress: (received: number, total: number | undefined) => void,
  digestFile: (filePath: string) => string = sha256File,
): string {
  throwIfProvisioningAborted(signal);
  fs.copyFileSync(source, destination);
  throwIfProvisioningAborted(signal);
  const size = fs.statSync(destination).size;
  onProgress(size, size);
  return digestFile(destination);
}

function sha256File(filePath: string): string {
  return createHash('sha256').update(fs.readFileSync(filePath)).digest('hex');
}

function cleanupObsoleteHLAEVersions(toolsDir: string, logLine: (text: string) => void): void {
  const parent = path.join(toolsDir, 'hlae');
  let entries: fs.Dirent[];
  try {
    entries = fs.readdirSync(parent, { withFileTypes: true });
  } catch (err) {
    logLine(`[tools] could not inspect obsolete HLAE installs in ${parent}: ${String(err)}\n`);
    return;
  }

  for (const entry of entries) {
    if (!entry.isDirectory() || entry.name === PINNED_HLAE_TOOL.version) continue;
    if (!/^\d+\.\d+\.\d+$/.test(entry.name)) continue;
    const obsolete = path.join(parent, entry.name);
    try {
      fs.rmSync(obsolete, { recursive: true, force: true });
      logLine(`[tools] removed obsolete HLAE ${entry.name}\n`);
    } catch (err) {
      logLine(`[tools] could not remove obsolete HLAE ${obsolete}: ${String(err)}\n`);
    }
  }
}

function requiredFilesExist(installDir: string, tool: RuntimeToolSpec): boolean {
  return tool.requiredRel.every((relativePath) => fs.existsSync(path.join(installDir, relativePath)));
}

async function completeInstall(
  installDir: string,
  tool: RuntimeToolSpec,
  expectedTreeSha256: string,
): Promise<boolean> {
  if (!requiredFilesExist(installDir, tool)) return false;
  try {
    const markerPath = path.join(installDir, INSTALL_MARKER);
    const markerInfo = fs.lstatSync(markerPath);
    if (!markerInfo.isFile() || markerInfo.isSymbolicLink()) return false;
    const value: unknown = JSON.parse(fs.readFileSync(path.join(installDir, INSTALL_MARKER), 'utf8'));
    if (!isInstallMarker(value, tool)) return false;
    const files = collectInstallFiles(installDir);
    if (files.length !== value.files.length) return false;
    const declared = new Map(value.files.map((file) => [file.path, file.sha256]));
    if (declared.size !== value.files.length || files.some((file) => !declared.has(file))) return false;
    return await sha256InstallTree(installDir, files) === expectedTreeSha256;
  } catch {
    return false;
  }
}

async function writeInstallMarker(installDir: string, tool: RuntimeToolSpec): Promise<void> {
  const files: RuntimeToolFileDigest[] = [];
  for (const relativePath of collectInstallFiles(installDir)) {
    files.push({
      path: relativePath,
      sha256: await sha256InstallFile(path.join(installDir, ...relativePath.split('/'))),
    });
  }
  const marker: RuntimeToolInstallMarker = {
    files,
    schemaVersion: INSTALL_MARKER_SCHEMA_VERSION,
    sourceSha256: tool.sha256,
    version: tool.version,
  };
  fs.writeFileSync(
    path.join(installDir, INSTALL_MARKER),
    JSON.stringify(marker),
  );
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null;
}

function isInstallMarker(value: unknown, tool: RuntimeToolSpec): value is RuntimeToolInstallMarker {
  if (!isRecord(value)
    || value.schemaVersion !== INSTALL_MARKER_SCHEMA_VERSION
    || value.version !== tool.version
    || value.sourceSha256 !== tool.sha256
    || !Array.isArray(value.files)) return false;
  return value.files.every((file): file is RuntimeToolFileDigest =>
    isRecord(file)
    && typeof file.path === 'string'
    && isSafeManifestPath(file.path)
    && typeof file.sha256 === 'string'
    && SHA256_PATTERN.test(file.sha256));
}

function isSafeManifestPath(value: string): boolean {
  return value !== ''
    && !value.includes('\\')
    && !value.startsWith('/')
    && value.split('/').every((part) => part !== '' && part !== '.' && part !== '..');
}

function collectInstallFiles(installDir: string): string[] {
  const files: string[] = [];
  const visit = (directory: string, relativeDirectory: string): void => {
    const entries = fs.readdirSync(directory, { withFileTypes: true })
      .sort((left, right) => left.name.localeCompare(right.name));
    for (const entry of entries) {
      const relativePath = relativeDirectory === '' ? entry.name : `${relativeDirectory}/${entry.name}`;
      if (relativePath === INSTALL_MARKER) continue;
      if (entry.isSymbolicLink()) throw new Error(`runtime tool cache contains a link: ${relativePath}`);
      if (entry.isDirectory()) {
        visit(path.join(directory, entry.name), relativePath);
      } else if (entry.isFile()) {
        files.push(relativePath);
      } else {
        throw new Error(`runtime tool cache contains an unsupported entry: ${relativePath}`);
      }
    }
  };
  visit(installDir, '');
  return files;
}

async function sha256InstallFile(filePath: string): Promise<string> {
  const hash = createHash('sha256');
  for await (const chunk of fs.createReadStream(filePath)) hash.update(chunk);
  return hash.digest('hex');
}

async function sha256InstallTree(
  installDir: string,
  files: string[] = collectInstallFiles(installDir),
): Promise<string> {
  const tree = createHash('sha256');
  for (const relativePath of files) {
    const digest = await sha256InstallFile(path.join(installDir, ...relativePath.split('/')));
    tree.update(relativePath, 'utf8');
    tree.update('\0', 'utf8');
    tree.update(digest, 'utf8');
    tree.update('\n', 'utf8');
  }
  return tree.digest('hex');
}

function trustedTreeSha256(
  options: RuntimeToolProvisioningOptions,
  name: RuntimeToolName,
  tool: RuntimeToolSpec,
): string {
  const expected = options.testOnlyTreeSha256?.[name] ?? tool.treeSha256;
  if (!SHA256_PATTERN.test(expected)) {
    throw new Error(`invalid trusted file tree sha256 for ${name}`);
  }
  return expected;
}

// Rename happens on the same volume and is the publication point: the desktop
// never observes the staging directory as a valid tool installation.
function promoteInstall(stagingDir: string, installDir: string, logLine: (text: string) => void): void {
  const previousDir = `${installDir}.previous`;
  try {
    fs.rmSync(previousDir, { recursive: true, force: true });
  } catch (err) {
    logLine(`[tools] could not remove previous install ${previousDir}: ${String(err)}\n`);
  }
  const hadPrevious = fs.existsSync(installDir);
  if (hadPrevious) fs.renameSync(installDir, previousDir);

  try {
    fs.renameSync(stagingDir, installDir);
  } catch (err) {
    if (hadPrevious && !fs.existsSync(installDir) && fs.existsSync(previousDir)) {
      fs.renameSync(previousDir, installDir);
    }
    throw err;
  }
  try {
    fs.rmSync(previousDir, { recursive: true, force: true });
  } catch (err) {
    logLine(`[tools] could not remove previous install ${previousDir}: ${String(err)}\n`);
  }
}

// A process can die between moving the old install aside and publishing the
// staged one. Restore that known path before evaluating cache validity.
function restoreInterruptedPromotion(installDir: string): void {
  const previousDir = `${installDir}.previous`;
  if (!fs.existsSync(installDir) && fs.existsSync(previousDir)) {
    fs.renameSync(previousDir, installDir);
  }
}

function cleanupPreviousInstall(installDir: string, logLine: (text: string) => void): void {
  const previousDir = `${installDir}.previous`;
  try {
    fs.rmSync(previousDir, { recursive: true, force: true });
  } catch (err) {
    logLine(`[tools] could not remove previous install ${previousDir}: ${String(err)}\n`);
  }
}

function cleanupStagingInstall(installDir: string, logLine: (text: string) => void): void {
  const stagingDir = `${installDir}.installing`;
  try {
    fs.rmSync(stagingDir, { recursive: true, force: true });
  } catch (err) {
    logLine(`[tools] could not remove interrupted install ${stagingDir}: ${String(err)}\n`);
  }
}

/** Uses the PowerShell archive implementation shipped with supported Windows versions. */
function expandArchive(archive: string, destination: string, signal: AbortSignal): Promise<void> {
  if (signal.aborted) return Promise.reject(new Error('archive extraction aborted'));

  return new Promise((resolve, reject) => {
    const powershell = spawn(
      'powershell.exe',
      [
        '-NoProfile',
        '-NonInteractive',
        '-Command',
        `Expand-Archive -LiteralPath ${psQuote(archive)} -DestinationPath ${psQuote(destination)} -Force`,
      ],
      { windowsHide: true },
    );
    let stderr = '';
    let aborted = false;
    let settled = false;
    const onAbort = (): void => {
      aborted = true;
      powershell.kill();
    };
    const finish = (err?: Error): void => {
      if (settled) return;
      settled = true;
      signal.removeEventListener('abort', onAbort);
      if (err) {
        reject(err);
      } else {
        resolve();
      }
    };

    signal.addEventListener('abort', onAbort, { once: true });
    powershell.stderr?.on('data', (chunk: Buffer) => {
      stderr += String(chunk);
    });
    powershell.once('error', (err) => finish(err));
    powershell.once('exit', (code) => {
      if (aborted) {
        finish(new Error('archive extraction aborted'));
      } else if (code === 0) {
        finish();
      } else {
        finish(new Error(`Expand-Archive exited ${code}: ${stderr.trim()}`));
      }
    });
  });
}
