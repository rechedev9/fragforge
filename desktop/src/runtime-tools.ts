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
  kind: 'zip' | 'exe';
  exeRel: string;
  requiredRel: readonly string[];
  timeoutMs: number;
}

const FFMPEG_RELEASE_DIR = 'ffmpeg-n8.1.2-21-gce3c09c101-win64-gpl-shared-8.1';
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
    version: 'n8.1.2',
    url: 'https://github.com/BtbN/FFmpeg-Builds/releases/download/autobuild-2026-07-03-13-21/ffmpeg-n8.1.2-21-gce3c09c101-win64-gpl-shared-8.1.zip',
    sha256: 'e0337e822bc66d01747bfa917080561739252aaceef3bccc049bcb299d6f9be0',
    kind: 'zip',
    exeRel: FFMPEG_EXE,
    requiredRel: [FFMPEG_EXE, FFPROBE_EXE],
    timeoutMs: 300_000,
  },
  ytdlp: {
    version: '2026.06.09',
    url: 'https://github.com/yt-dlp/yt-dlp/releases/download/2026.06.09/yt-dlp.exe',
    sha256: '3a48cb955d55c8821b60ccbdbbc6f61bc958f2f3d3b7ad5eaf3d83a543293a27',
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
  let legacyFallback = false;
  throwIfProvisioningAborted(options.signal);
  try {
    restoreInterruptedPromotion(installDir);
    cleanupStagingInstall(installDir, options.logLine);
    if (completeInstall(installDir, tool)) {
      cleanupPreviousInstall(installDir, options.logLine);
      return executable;
    }
    // Markerless releases predate atomic publication. Keep them in place as
    // an offline fallback, but do not bless a potentially partial shared-tool
    // extraction: refresh through staging and publish a verified replacement.
    legacyFallback = !fs.existsSync(path.join(installDir, INSTALL_MARKER))
      && requiredFilesExist(installDir, tool);
  } catch (err) {
    options.logLine(`[tools] ${name} cache inspection failed: ${String(err)}\n`);
    return '';
  }

  onStatus?.(name);
  const controller = new AbortController();
  const abortFromCaller = (): void => controller.abort();
  options.signal?.addEventListener('abort', abortFromCaller, { once: true });
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
    if (legacyFallback) {
      options.logLine(
        `[tools] ${name} verified refresh failed (${reason}); using markerless legacy install until retry\n`,
      );
      return executable;
    }
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
    writeInstallMarker(stagingDir, tool);
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

function completeInstall(installDir: string, tool: RuntimeToolSpec): boolean {
  if (!requiredFilesExist(installDir, tool)) return false;
  try {
    const value: unknown = JSON.parse(fs.readFileSync(path.join(installDir, INSTALL_MARKER), 'utf8'));
    return isRecord(value) && value.version === tool.version && value.sha256 === tool.sha256;
  } catch {
    return false;
  }
}

function writeInstallMarker(installDir: string, tool: RuntimeToolSpec): void {
  fs.writeFileSync(
    path.join(installDir, INSTALL_MARKER),
    JSON.stringify({ version: tool.version, sha256: tool.sha256 }),
  );
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null;
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
