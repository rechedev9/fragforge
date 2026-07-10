// FragForge Studio - Electron main process.
//
// This is the desktop wrapper around Local Studio: it boots the same two
// processes the local-studio.ps1 launcher does (the Go orchestrator and the
// Next.js server in local mode), then shows the web UI in a native window.
// Capture (HLAE/CS2) is driven by the orchestrator exactly as before; Electron
// only replaces the "install Node, run a script, open a browser" friction.
//
// Both children bind loopback on dynamically-chosen free ports, so two installs
// (or a stray process) never collide on a fixed port.

import { app, BrowserWindow, shell, session } from 'electron';
import { spawn, spawnSync, type ChildProcess } from 'node:child_process';
import { createHash } from 'node:crypto';
import * as path from 'node:path';
import * as fs from 'node:fs';
import * as http from 'node:http';
import * as https from 'node:https';
import * as net from 'node:net';
import { pathToFileURL } from 'node:url';
import { escapeHtml, psQuote } from './escaping';
import { PINNED_HLAE_TOOL } from './hlae-tool';
import { validateWindowState, type WindowState } from './window-state';
import { lastLines } from './log-tail';

// Every loopback server and health check binds/targets this host; named once so
// the value that couples all the URLs below is not a scattered magic string.
const LOOPBACK_HOST = '127.0.0.1';

// A single running instance owns the orchestrator; a second launch just focuses
// the existing window instead of spawning a duplicate backend on new ports.
if (!app.requestSingleInstanceLock()) {
  app.quit();
  process.exit(0);
}

// main.ts compiles to dist/main.js, so __dirname is <app>/dist at runtime while
// loading.html and (in dev) build-resources still live one level up at the app
// root. appRoot restores the original main.js-at-root resolution, so every
// bundled-file path below stays exactly what it was before the TS move.
const appRoot = path.join(__dirname, '..');

/**
 * Resolves a bundled resource for both the packaged app and `electron .` (dev).
 * Both read the same assembled layout: packaged from process.resourcesPath, dev
 * from ./build-resources (run `npm run assemble` first to produce it), so the
 * Next server always has its .next/static staged next to server.js.
 */
function resourcePath(...parts: string[]): string {
  const base = app.isPackaged ? process.resourcesPath : path.join(appRoot, 'build-resources');
  return path.join(base, ...parts);
}

// Spawn zv-orchestrator directly instead of `zv serve`: zv only delegates to
// the orchestrator binary next to it, and killing the zv intermediary on app
// quit would leave the real server running as an orphan (holding its port and
// the SQLite job db) since child.kill() does not reach grandchildren.
const orchestratorExe = resourcePath(
  'bin',
  process.platform === 'win32' ? 'zv-orchestrator.exe' : 'zv-orchestrator',
);
const nextServer = resourcePath('web', 'server.js');
const dataDir = path.join(app.getPath('userData'), 'data');
const musicDir = path.join(dataDir, 'music');

// All child output is mirrored to this file so a failed boot is diagnosable
// from a user report: the packaged app has no console, so stdout alone is
// invisible. The error screen shows its tail.
const logFile = path.join(app.getPath('userData'), 'studio.log');
let logStream: fs.WriteStream | null = null;
function logLine(text: string): void {
  process.stdout.write(text);
  try {
    if (!logStream) {
      // Rotate rather than truncate: a crash right before this run's launch
      // (e.g. an install that never got past its previous boot) would
      // otherwise vanish the moment this run's first line is written, so
      // keep one previous run's log around for a bug report.
      try {
        fs.renameSync(logFile, `${logFile}.1`);
      } catch {
        // no previous log (first launch ever) or rename failed; either way
        // this run's log still gets written below
      }
      logStream = fs.createWriteStream(logFile, { flags: 'w' });
    }
    logStream.write(text);
  } catch {
    // Logging must never break the app; stdout still has the line in dev.
  }
}

// Narrows a JSON.parse'd (or otherwise untrusted) value to an indexable object
// before any property is read. Used at every on-disk-JSON trust boundary below.
function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null;
}

interface DownloadOptions {
  signal?: AbortSignal;
  onProgress?: (received: number, total: number | undefined) => void;
}

// Third-party tools the pipeline needs at runtime but that cannot ship inside
// the installer (size, licensing hygiene): they are provisioned into userData
// on first boot and handed to the orchestrator via env vars (env always wins
// over its auto-detection). Every tool uses a pinned release URL and sha256
// digest, including the official HLAE release. Every download is best-effort:
// a failure just leaves that feature unconfigured, the UI explains, and the
// next boot retries.
//
// - HLAE (advancedfx, MIT): drives CS2 capture. Zip ships its LICENSES/.
// - FFmpeg (BtbN autobuild, GPL): every render (reels and stream clips) shells
//   out to ffmpeg/ffprobe; without it nothing can be rendered on a clean PC.
//   Pinned to a dated autobuild tag because the "latest" tag's assets are
//   replaced daily and would break the hash.
// - yt-dlp (Unlicense): fetches Twitch/YouTube sources for Stream Clips.
const toolsRoot = (): string => path.join(app.getPath('userData'), 'tools');

type ToolName = 'hlae' | 'ffmpeg' | 'ytdlp';

interface ToolSpec {
  version: string;
  url: string;
  sha256: string;
  kind: 'zip' | 'exe';
  exeRel: string;
  timeoutMs: number;
}

// Fires once per tool that actually needs a download (detail undefined), then
// again as coarse progress trickles in (detail a short string like "45%").
type StatusReporter = (name: ToolName, detail?: string) => void;

const TOOLS: Record<ToolName, ToolSpec> = {
  hlae: PINNED_HLAE_TOOL,
  ffmpeg: {
    version: 'n8.1.2',
    url: 'https://github.com/BtbN/FFmpeg-Builds/releases/download/autobuild-2026-07-03-13-21/ffmpeg-n8.1.2-21-gce3c09c101-win64-gpl-shared-8.1.zip',
    sha256: 'e0337e822bc66d01747bfa917080561739252aaceef3bccc049bcb299d6f9be0',
    kind: 'zip',
    exeRel: path.join('ffmpeg-n8.1.2-21-gce3c09c101-win64-gpl-shared-8.1', 'bin', 'ffmpeg.exe'),
    timeoutMs: 300_000, // ~80 MB; generous for slow lines, capped so boot never wedges
  },
  ytdlp: {
    version: '2026.06.09',
    url: 'https://github.com/yt-dlp/yt-dlp/releases/download/2026.06.09/yt-dlp.exe',
    sha256: '3a48cb955d55c8821b60ccbdbbc6f61bc958f2f3d3b7ad5eaf3d83a543293a27',
    kind: 'exe',
    exeRel: 'yt-dlp.exe',
    timeoutMs: 90_000,
  },
};

/**
 * Downloads url to destPath, staging through `<destPath>.tmp` and renaming
 * only once every byte has arrived, so a request that dies mid-stream (a
 * network drop, or the caller's own abort on timeout) never leaves a partial
 * file sitting at destPath. Returns the sha256 hex digest of the downloaded
 * bytes. onProgress(bytesSoFar, totalBytes), if given, fires as data arrives
 * (totalBytes is undefined when the server omits Content-Length). Pass an
 * AbortSignal to cancel an in-flight download: the request and response
 * stream are destroyed so nothing writes after the signal fires.
 */
async function downloadFile(url: string, destPath: string, { signal, onProgress }: DownloadOptions = {}): Promise<string> {
  const res = await fetchStream(url, { signal });
  const total = Number(res.headers['content-length']) || undefined;
  const tmp = `${destPath}.tmp`;
  const hash = createHash('sha256');
  try {
    let received = 0;
    await new Promise<void>((resolve, reject) => {
      const out = fs.createWriteStream(tmp);
      // Shared by the stream-error path and the abort-signal path below so a
      // cancellation always tears down both ends instead of leaving the
      // response free to keep writing into `out`.
      const fail = (err: Error): void => {
        res.destroy();
        out.destroy();
        reject(err);
      };
      res.on('data', (chunk: Buffer) => {
        hash.update(chunk);
        received += chunk.length;
        if (onProgress) onProgress(received, total);
      });
      res.pipe(out);
      res.on('error', fail);
      out.on('error', fail);
      out.on('finish', resolve);
      if (signal) {
        if (signal.aborted) {
          fail(new Error('download aborted'));
        } else {
          signal.addEventListener('abort', () => fail(new Error('download aborted')), { once: true });
        }
      }
    });
    fs.renameSync(tmp, destPath);
    return hash.digest('hex');
  } catch (err) {
    fs.rmSync(tmp, { force: true });
    throw err;
  }
}

/** Expand-Archive via PowerShell (ships with every Windows 10/11). */
function expandArchive(zipPath: string, destDir: string): Promise<void> {
  return new Promise((resolve, reject) => {
    const ps = spawn(
      'powershell.exe',
      ['-NoProfile', '-NonInteractive', '-Command',
        `Expand-Archive -LiteralPath ${psQuote(zipPath)} -DestinationPath ${psQuote(destDir)} -Force`],
      { windowsHide: true },
    );
    let stderr = '';
    ps.stderr?.on('data', (b: Buffer) => { stderr += String(b); });
    ps.on('error', reject);
    ps.on('exit', (code) => (code === 0 ? resolve() : reject(new Error(`Expand-Archive exited ${code}: ${stderr.trim()}`))));
  });
}

// Throttle for download-progress status updates: frequent enough to look
// alive, infrequent enough not to repaint the loading screen on every chunk.
const PROGRESS_REPORT_MIN_INTERVAL_MS = 1000;

/**
 * Ensures one tool from TOOLS is installed under userData\tools and returns
 * its exe path, or '' on failure (offline, bad hash, timeout); the caller then
 * simply omits the env var and the orchestrator falls back to auto-detection.
 * Idempotent: a cached install returns instantly. Each tool is capped by its
 * own timeout so one stalled download can never wedge the boot; on timeout
 * the in-flight HTTP request is aborted (not just abandoned), so nothing
 * writes to disk after this function has already told its caller it failed.
 */
async function provisionTool(name: ToolName, onStatus?: StatusReporter): Promise<string> {
  const tool = TOOLS[name];
  const dir = path.join(toolsRoot(), name, tool.version);
  const exe = path.join(dir, tool.exeRel);
  if (fs.existsSync(exe)) return exe;
  // Only fires when a real download is about to start (cache hits above never
  // reach here), so the loading screen doesn't flash a status for instant boots.
  if (onStatus) onStatus(name);

  const controller = new AbortController();
  let lastReportAt = 0;
  let lastPct = -1;
  const onProgress = (done: number, total: number | undefined): void => {
    if (!onStatus) return;
    const now = Date.now();
    if (now - lastReportAt < PROGRESS_REPORT_MIN_INTERVAL_MS) return;
    if (total) {
      const pct = Math.floor((done / total) * 100);
      if (pct === lastPct) return;
      lastPct = pct;
      lastReportAt = now;
      onStatus(name, `${pct}%`);
    } else {
      lastReportAt = now;
      onStatus(name, `${(done / (1024 * 1024)).toFixed(0)} MB`);
    }
  };

  const work = (async (): Promise<string> => {
    fs.mkdirSync(dir, { recursive: true });
    const part = path.join(dir, 'download.part');
    const zipPath = path.join(dir, 'download.zip');
    try {
      logLine(`[tools] downloading ${name} ${tool.version}...\n`);
      const digest = await downloadFile(tool.url, part, { signal: controller.signal, onProgress });
      if (digest !== tool.sha256) {
        throw new Error(`sha256 mismatch: got ${digest}, want ${tool.sha256}`);
      }
      if (tool.kind === 'exe') {
        fs.renameSync(part, exe);
      } else {
        // Only a fully-downloaded, hash-verified archive gets the .zip name:
        // Expand-Archive refuses any other extension, and the rename doubles
        // as the "download completed" marker a .part convention provides.
        fs.renameSync(part, zipPath);
        await expandArchive(zipPath, dir);
      }
      if (!fs.existsSync(exe)) throw new Error(`${tool.exeRel} missing after install in ${dir}`);
      logLine(`[tools] installed ${exe}\n`);
      return exe;
    } finally {
      fs.rmSync(part, { force: true });
      fs.rmSync(zipPath, { force: true });
    }
  })();

  try {
    return await Promise.race([
      work,
      new Promise<string>((_resolve, reject) =>
        setTimeout(() => {
          // Abort the underlying request so the download actually stops
          // instead of continuing to stream (and write .part bytes) in the
          // background after this function has already reported failure.
          controller.abort();
          reject(new Error(`timed out after ${tool.timeoutMs}ms`));
        }, tool.timeoutMs)),
    ]);
  } catch (err) {
    logLine(`[tools] ${name} provisioning failed (feature stays unconfigured, retried next boot): ${String(err)}\n`);
    return '';
  }
}

// Display names for the loading-screen per-tool status lines; TOOLS keys are
// the internal provisioning names, these are what a user recognizes.
const TOOL_LABELS: Record<ToolName, string> = { hlae: 'HLAE', ffmpeg: 'FFmpeg', ytdlp: 'yt-dlp' };

/**
 * Provisions all runtime tools concurrently and returns the env vars to hand
 * the orchestrator. ffprobe.exe sits next to ffmpeg.exe in the same archive.
 * onStatus(name, detail), if given, fires once per tool that actually needs a
 * download (detail undefined), then again as coarse progress trickles in
 * (detail a short string like "45%" or "12 MB").
 */
async function provisionTools(onStatus?: StatusReporter): Promise<Record<string, string>> {
  if (process.platform !== 'win32') return {};
  const [hlae, ffmpeg, ytdlp] = await Promise.all([
    provisionTool('hlae', onStatus),
    provisionTool('ffmpeg', onStatus),
    provisionTool('ytdlp', onStatus),
  ]);
  const env: Record<string, string> = {};
  if (hlae) env.ZV_HLAE_PATH = hlae;
  if (ffmpeg) {
    env.ZV_FFMPEG_PATH = ffmpeg;
    const ffprobe = path.join(path.dirname(ffmpeg), 'ffprobe.exe');
    if (fs.existsSync(ffprobe)) env.ZV_FFPROBE_PATH = ffprobe;
  }
  if (ytdlp) env.ZV_YTDLP_PATH = ytdlp;
  return env;
}

// Idle-socket timeout for a download in progress: generous (large archives on
// a slow line), but stalled sockets are a real failure mode worth cutting off
// rather than hanging until the per-tool timeout in provisionTool.
const DOWNLOAD_SOCKET_IDLE_TIMEOUT_MS = 60_000;

interface FetchStreamOptions {
  redirectsLeft?: number;
  signal?: AbortSignal;
}

/**
 * GET that follows redirects (archive.org download links bounce to mirrors).
 * Pass signal to abort the request (and any pending redirect chain) so a
 * caller-side timeout actually stops the network activity instead of just
 * abandoning the promise.
 */
function fetchStream(url: string, { redirectsLeft = 5, signal }: FetchStreamOptions = {}): Promise<http.IncomingMessage> {
  return new Promise((resolve, reject) => {
    const handler = (res: http.IncomingMessage): void => {
      const code = res.statusCode;
      if (code !== undefined && code >= 300 && code < 400 && res.headers.location && redirectsLeft > 0) {
        res.resume();
        resolve(fetchStream(new URL(res.headers.location, url).toString(), { redirectsLeft: redirectsLeft - 1, signal }));
        return;
      }
      if (code !== 200) {
        res.resume();
        reject(new Error(`GET ${url}: HTTP ${code}`));
        return;
      }
      resolve(res);
    };
    const req = url.startsWith('https:')
      ? https.get(url, { signal }, handler)
      : http.get(url, { signal }, handler);
    req.on('error', reject);
    req.setTimeout(DOWNLOAD_SOCKET_IDLE_TIMEOUT_MS, () => req.destroy(new Error(`GET ${url}: timed out`)));
  });
}

/**
 * Provisions the music catalog into the user's data dir (the Node port of
 * scripts/fetch-music.sh): copies the bundled catalog.json and downloads each
 * CC0/CC-BY track to <musicDir>/<id>.<ext> if missing. Idempotent and
 * best-effort - an offline first boot just means the song picker shows fewer
 * tracks until the next launch. Runs concurrently with the backend boot; the
 * orchestrator rescans the dir on every /api/songs request, so tracks appear
 * as they land.
 */
async function provisionMusic(): Promise<void> {
  const bundledCatalog = resourcePath('music', 'catalog.json');
  if (!fs.existsSync(bundledCatalog)) return;
  fs.mkdirSync(musicDir, { recursive: true });
  fs.copyFileSync(bundledCatalog, path.join(musicDir, 'catalog.json'));

  let tracks: unknown[];
  try {
    const parsed: unknown = JSON.parse(fs.readFileSync(bundledCatalog, 'utf8'));
    tracks = isRecord(parsed) && Array.isArray(parsed.tracks) ? parsed.tracks : [];
  } catch (err) {
    logLine(`[music] bad catalog.json: ${String(err)}\n`);
    return;
  }
  // Downloads run one at a time on purpose: this is the faithful port of the
  // original sequential loop, and the orchestrator picks up tracks as they land,
  // so there is no need to hammer several release hosts at once during boot.
  for (const track of tracks) {
    if (!isRecord(track)) continue;
    const { id, ext, downloadUrl } = track;
    if (typeof id !== 'string' || !id || typeof ext !== 'string' || !ext) continue;
    const dest = path.join(musicDir, `${id}.${ext}`);
    if (fs.existsSync(dest)) continue;
    // Local-only tracks (no downloadUrl, e.g. AI-generated) ship inside the
    // installer's music resources; copy them instead of downloading.
    if (typeof downloadUrl !== 'string' || !downloadUrl) {
      const bundledAudio = resourcePath('music', `${id}.${ext}`);
      if (fs.existsSync(bundledAudio)) {
        fs.copyFileSync(bundledAudio, dest);
        logLine(`[music] copied bundled ${id}.${ext}\n`);
      } else {
        logLine(`[music] skip ${id}: no downloadUrl and no bundled audio\n`);
      }
      continue;
    }
    // downloadFile stages through a temp name and renames on success, so a
    // half-written file never shows up as a playable track.
    try {
      await downloadFile(downloadUrl, dest);
      logLine(`[music] downloaded ${id}.${ext}\n`);
    } catch (err) {
      logLine(`[music] skip ${id}: ${String(err)}\n`);
    }
  }
}

/** Grabs an OS-assigned free loopback port, then releases it for the child. */
function freePort(): Promise<number> {
  return new Promise((resolve, reject) => {
    const srv = net.createServer();
    srv.unref();
    srv.on('error', reject);
    srv.listen(0, LOOPBACK_HOST, () => {
      const addr = srv.address();
      if (addr === null || typeof addr === 'string') {
        srv.close(() => reject(new Error('freePort: server has no assigned address')));
        return;
      }
      const { port } = addr;
      srv.close(() => resolve(port));
    });
  });
}

/** Reports whether a specific loopback port is currently free. */
function portFree(port: number): Promise<boolean> {
  return new Promise((resolve) => {
    const srv = net.createServer();
    srv.unref();
    srv.on('error', () => resolve(false));
    srv.listen(port, LOOPBACK_HOST, () => srv.close(() => resolve(true)));
  });
}

const portsFile = path.join(app.getPath('userData'), 'ports.json');

/**
 * Returns a per-install stable loopback port for the given service, falling
 * back to a fresh OS-assigned one when the saved port is taken. The web port
 * MUST stay stable across launches: the reel library lives in the renderer's
 * localStorage, which is keyed by origin (host:port), so a random port per
 * launch would empty the library on every restart even though the job state
 * survives in SQLite.
 */
async function stablePort(key: string): Promise<number> {
  let saved: Record<string, unknown> = {};
  try {
    const parsed: unknown = JSON.parse(fs.readFileSync(portsFile, 'utf8'));
    if (isRecord(parsed)) saved = parsed;
  } catch {
    // first launch or unreadable file; fall through to picking a new port
  }
  const savedPort = saved[key];
  if (typeof savedPort === 'number' && Number.isInteger(savedPort)) {
    if (await portFree(savedPort)) return savedPort;
    // Something else grabbed the saved port (another instance that ignored
    // the single-instance lock, a stray leftover process, an unrelated app);
    // picking a fresh port is safe but changes the origin the web UI depends
    // on, so make that visible instead of silently shifting under the user.
    const hint = key === 'web'
      ? ' the reel library kept in the browser localStorage is keyed by origin, so it may appear empty on the new port'
      : '';
    logLine(`[ports] saved ${key} port ${savedPort} was taken, picking a new one;${hint}\n`);
  }
  const port = await freePort();
  try {
    fs.writeFileSync(portsFile, JSON.stringify({ ...saved, [key]: port }));
  } catch (err) {
    logLine(`[ports] could not persist ${key} port: ${String(err)}\n`);
  }
  return port;
}

// Per-attempt socket timeout and delay between polls for waitForHttp; short
// relative to the overall deadline the caller passes in, so a single slow
// attempt never eats a meaningful chunk of the boot budget.
const HEALTH_REQUEST_TIMEOUT_MS = 2000;
const HEALTH_POLL_INTERVAL_MS = 400;

/** Polls an HTTP URL until it answers 2xx/3xx or the timeout elapses. */
function waitForHttp(url: string, timeoutMs: number): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  return new Promise((resolve, reject) => {
    const attempt = (): void => {
      const req = http.get(url, (res) => {
        res.resume();
        if (res.statusCode && res.statusCode < 400) {
          resolve();
          return;
        }
        retry();
      });
      req.on('error', retry);
      req.setTimeout(HEALTH_REQUEST_TIMEOUT_MS, () => req.destroy());
    };
    const retry = (): void => {
      if (Date.now() > deadline) {
        reject(new Error(`timed out waiting for ${url}`));
        return;
      }
      setTimeout(attempt, HEALTH_POLL_INTERVAL_MS);
    };
    attempt();
  });
}

const children: ChildProcess[] = [];

interface Launched {
  child: ChildProcess;
  // Rejects the moment the child dies or fails to spawn; never resolves.
  exited: Promise<never>;
}

/**
 * Spawns a child, tracks it for shutdown, and prefixes its logs (mirrored to
 * studio.log). The returned exited promise rejects the moment the child dies
 * or fails to spawn, so boot() can fail fast with the real cause (an AV
 * quarantine or a crashing backend exits in milliseconds) instead of sitting
 * out the full healthz timeout and reporting a useless "timed out".
 */
function launch(label: string, exe: string, args: string[], env: Record<string, string>): Launched {
  const child = spawn(exe, args, { env: { ...process.env, ...env }, windowsHide: true });
  children.push(child);
  const tag = (buf: Buffer): void => logLine(`[${label}] ${String(buf)}`);
  child.stdout?.on('data', tag);
  child.stderr?.on('data', tag);
  const exited = new Promise<never>((_resolve, reject) => {
    child.on('error', (err) => {
      logLine(`[${label}] failed to start: ${String(err)}\n`);
      // User-facing strings (here and in the loading/error screens) are in
      // Spanish: FragForge Studio targets the Spanish-speaking CS2
      // content-creator market, so the desktop chrome speaks their language.
      reject(new Error(`${label} no pudo iniciarse: ${err.message}`));
    });
    child.on('exit', (code) => {
      logLine(`[${label}] exited (${code})\n`);
      reject(new Error(`${label} terminó inesperadamente (código ${code})`));
    });
  });
  exited.catch(() => {}); // observed selectively during boot; never unhandled
  return { child, exited };
}

/** Last lines of studio.log, HTML-escaped for the error screen. */
function logTail(maxLines = 40): string {
  try {
    return escapeHtml(lastLines(fs.readFileSync(logFile, 'utf8'), maxLines));
  } catch {
    return '(sin registro)';
  }
}

let mainWindow: BrowserWindow | null = null;

/**
 * Returns the main window only if it exists and Electron hasn't torn it down
 * yet, otherwise null. The window can disappear out from under any async
 * continuation (boot() awaits ports, downloads, child-process health checks;
 * the user can close the window, or a fatal renderer crash can destroy it, at
 * any point during those awaits), so every place that touches the window after
 * an await must go through this and bail on null instead of assuming it is
 * still there.
 */
function aliveWindow(): BrowserWindow | null {
  return mainWindow !== null && !mainWindow.isDestroyed() ? mainWindow : null;
}

// Origins the window is allowed to navigate to on its own, populated once the
// boot-time ports are known (see boot()). Referenced by the handlers below via
// closure, so it is safe to register those handlers before the ports exist.
const allowedOrigins = new Set<string>();

/** True if url's origin is one of the loopback servers we just spawned. */
function isLoopbackOrigin(url: string): boolean {
  try {
    return allowedOrigins.has(new URL(url).origin);
  } catch {
    return false;
  }
}

// loading.html lives at the app root (one level up from dist/).
const loadingHtmlPath = path.join(appRoot, 'loading.html');

// The exact file:// URL for loading.html, computed once: the will-navigate
// guard below allows navigating straight to this URL and nothing else under
// file:/data:, so a page can never navigate the window to an arbitrary local
// file or an attacker-crafted data: payload.
const loadingFileUrl = pathToFileURL(loadingHtmlPath).href;

// data: URLs the main process itself generated and is currently showing (only
// showErrorScreen creates these). Cleared and repopulated with just the
// latest one on every call, so at most one data: URL is ever "trusted" at a
// time; that is the (b) half of the will-navigate allowlist alongside (a)
// loadingFileUrl above.
const allowedInternalUrls = new Set<string>();

// Fake, unresolvable "URL" the error screen's retry button links to. The
// renderer runs sandboxed with no preload/IPC, so a plain <a href> navigation
// intercepted by will-navigate is the only way for a button click to reach
// the main process; Chromium never actually resolves this host.
const RETRY_URL = 'https://retry.fragforge.invalid/';

const windowFile = path.join(app.getPath('userData'), 'window.json');

/** Reads saved window bounds and maximize state, falling back to sane defaults if missing, corrupt, or implausibly small. */
function loadWindowState(): WindowState {
  try {
    return validateWindowState(JSON.parse(fs.readFileSync(windowFile, 'utf8')));
  } catch {
    // Missing file or unparseable JSON; validateWindowState(undefined) returns
    // the same fallback the inline check used.
    return validateWindowState(undefined);
  }
}

/** Best-effort save of the window's current size/position/maximize state so the app reopens where the user left it. */
function saveWindowBounds(): void {
  const win = aliveWindow();
  if (win === null) return;
  try {
    // getNormalBounds(), not getBounds(): while maximized, getBounds() would
    // report the full-screen size, so un-maximizing next launch would
    // restore into that instead of a sane windowed size.
    const bounds = win.getNormalBounds();
    fs.writeFileSync(windowFile, JSON.stringify({ ...bounds, isMaximized: win.isMaximized() }));
  } catch (err) {
    logLine(`[window] could not persist bounds: ${String(err)}\n`);
  }
}

// Guards mainWindow.reload() so a crash-loop in the renderer reloads once
// instead of hammering a dead server forever.
let renderProcessGoneReloaded = false;
let renderProcessGoneResetTimer: NodeJS.Timeout | null = null;

// How long a reloaded page must stay up before an unrelated crash hours later
// gets its own free reload again, instead of the stale flag from a long-past
// crash sending it straight to the error screen.
const RENDER_CRASH_RESET_DELAY_MS = 60_000;

function createWindow(loadingOnly: boolean): BrowserWindow {
  const { bounds, isMaximized } = loadWindowState();
  const win = new BrowserWindow({
    ...bounds,
    backgroundColor: '#0a0a0a',
    title: 'FragForge Studio',
    webPreferences: {
      contextIsolation: true,
      nodeIntegration: false,
      sandbox: true,
      // Packaged users have no menu and should never land in DevTools by
      // accident; dev keeps it open for diagnosis (packaged-side diagnosis
      // is the studio.log tail shown on the error screen instead).
      devTools: !app.isPackaged,
    },
  });
  mainWindow = win;
  win.removeMenu();
  if (isMaximized) win.maximize();
  win.on('close', saveWindowBounds);
  // The window can be destroyed (user closes it, or Chromium tears it down
  // after a fatal render-process crash) while boot() or a post-boot watcher
  // is mid-await; clearing the reference lets aliveWindow() catch every one
  // of those spots instead of them throwing into a dead BrowserWindow.
  win.on('closed', () => {
    mainWindow = null;
  });

  // Deny every window.open (no popups in a kiosk-style desktop wrapper);
  // an http(s) target that isn't our own loopback server is handed to the
  // user's real browser instead of silently vanishing.
  win.webContents.setWindowOpenHandler(({ url }) => {
    if (/^https?:\/\//i.test(url) && !isLoopbackOrigin(url)) shell.openExternal(url);
    return { action: 'deny' };
  });

  // Same idea for in-window navigation: only our own orchestrator/web
  // origins, the loading.html file, and the error screen's own current data:
  // URL may navigate the window directly. Everything else - including any
  // other data:/file: URL - is blocked and, if http(s), opened externally
  // instead. The retry link on the error screen is a special case: it is
  // intercepted here and turned into a real retryBoot() call rather than
  // ever being allowed to "navigate" anywhere.
  win.webContents.on('will-navigate', (event, url) => {
    if (url === RETRY_URL) {
      event.preventDefault();
      retryBoot();
      return;
    }
    if (url === loadingFileUrl || allowedInternalUrls.has(url) || isLoopbackOrigin(url)) return;
    event.preventDefault();
    if (/^https?:\/\//i.test(url)) shell.openExternal(url);
  });

  win.webContents.on('render-process-gone', (_event, details) => {
    logLine(`[window] render process gone: ${JSON.stringify(details)}\n`);
    if (quitting) return;
    if (renderProcessGoneResetTimer) {
      clearTimeout(renderProcessGoneResetTimer);
      renderProcessGoneResetTimer = null;
    }
    if (!renderProcessGoneReloaded) {
      renderProcessGoneReloaded = true;
      const alive = aliveWindow();
      if (alive !== null) alive.reload();
      // Only treat the crash flag as "used up" for a while: once the reload
      // has stood for RENDER_CRASH_RESET_DELAY_MS without a further crash,
      // reset it so a later, unrelated crash still gets its own free reload
      // instead of going straight to the error screen.
      renderProcessGoneResetTimer = setTimeout(() => {
        renderProcessGoneReloaded = false;
        renderProcessGoneResetTimer = null;
      }, RENDER_CRASH_RESET_DELAY_MS);
      return;
    }
    // A second crash within the window above means the reload didn't fix
    // whatever is wrong (a fatal Chromium bug, a corrupted profile); show the
    // error screen instead of silently reloading forever and leaving a dead
    // window.
    showErrorScreen(
      `La interfaz se ha bloqueado repetidamente (motivo: ${details.reason}).`,
      'La interfaz se ha bloqueado repetidamente',
      'Cierra FragForge Studio y vuelve a abrirlo. Si el problema persiste, revisa el registro.',
    );
  });

  if (loadingOnly) win.loadFile(loadingHtmlPath);
  return win;
}

// True while the loading.html screen is the thing on screen, so
// setLoadingStatus knows whether its target element can even exist.
let loadingScreenShowing = false;

/** Updates the loading screen's #status line; a silent no-op once we've navigated away from it (real app, or error screen). */
function setLoadingStatus(text: string): void {
  const win = aliveWindow();
  if (win === null || !loadingScreenShowing) return;
  win.webContents
    .executeJavaScript(
      `(() => { const el = document.getElementById('status'); if (el) el.textContent = ${JSON.stringify(text)}; })()`,
    )
    .catch(() => {}); // the page may already be gone; never block boot on this
}

/** Renders the fatal-error screen as a data: URL, so it never depends on the servers that just failed or died. */
function showErrorScreen(err: unknown, title?: string, hint?: string): void {
  loadingScreenShowing = false;
  const win = aliveWindow();
  if (win === null) return;
  const defaultTitle = 'FragForge Studio no pudo arrancar';
  const defaultHint =
    'Si un antivirus ha bloqueado o puesto en cuarentena archivos de FragForge, restáuralos y vuelve a abrir la app.';
  const html = `<!doctype html><html><head><meta charset="utf-8">
    <meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'">
    <style>
      body{font:16px system-ui;background:#0a0a0a;color:#eee;padding:2rem}
      a.retry{display:inline-block;margin-top:1rem;padding:.6rem 1.2rem;background:#22d9ee;color:#04121a;
        font-weight:600;text-decoration:none;border-radius:4px}
    </style></head>
    <body>
      <h2>${escapeHtml(title || defaultTitle)}</h2>
      <p>${escapeHtml(err)}</p>
      <p style="color:#999">${hint || defaultHint} Registro completo: ${escapeHtml(logFile)}</p>
      <a class="retry" href="${RETRY_URL}">Reintentar</a>
      <pre style="background:#111;padding:1rem;overflow:auto;max-height:40vh;font-size:12px">${logTail()}</pre>
    </body></html>`;
  const url = 'data:text/html;charset=utf-8,' + encodeURIComponent(html);
  // Only the error screen currently on display is a trusted navigation
  // target; drop whatever the previous error screen (if any) allowed.
  allowedInternalUrls.clear();
  allowedInternalUrls.add(url);
  win.loadURL(url);
}

// Web port of the server spawned by the boot currently in flight, set at the
// top of boot() as soon as it's known. loadMatches() (invoked later, after the
// boot's own local variables are out of scope) needs it to know where
// "/matches" is.
let currentWebPort: number | null = null;

/** Sends the window to the app shell once boot is done. */
function loadMatches(): void {
  loadingScreenShowing = false;
  const win = aliveWindow();
  if (currentWebPort !== null && win !== null) {
    win.loadURL(`http://${LOOPBACK_HOST}:${currentWebPort}/matches`);
  }
}

// Generous overall deadline for each server to answer its health check: a
// first-ever boot can still be mid-way through provisioning a stalled tool
// download inside its own per-tool timeout, so this only needs to bound how
// long a genuinely wedged/crash-looping child is waited on.
const BOOT_HEALTH_TIMEOUT_MS = 60_000;

async function boot(): Promise<void> {
  // Reuse the existing window on a retry (see retryBoot()) instead of
  // spawning a second BrowserWindow on top of the one showing the error
  // screen; only the very first boot (or a fresh boot after the window was
  // fully closed) needs to create one.
  const existing = aliveWindow();
  if (existing !== null) {
    existing.loadFile(loadingHtmlPath);
  } else {
    createWindow(true);
  }
  loadingScreenShowing = true;

  setLoadingStatus('Eligiendo puertos libres…');
  // Sequential, not Promise.all: both calls read and rewrite the same
  // ports.json ({...saved, [key]: port}), so running them concurrently would
  // race on that file and drop one service's saved port.
  const orchPort = await stablePort('orchestrator');
  const webPort = await stablePort('web');
  const orchestratorUrl = `http://${LOOPBACK_HOST}:${orchPort}`;
  currentWebPort = webPort;
  // Ports are only known now, so this is the earliest point the navigation
  // guards in createWindow() have anything real to allow.
  allowedOrigins.add(`http://${LOOPBACK_HOST}:${orchPort}`);
  allowedOrigins.add(`http://${LOOPBACK_HOST}:${webPort}`);

  // Fire-and-forget: tracks land while the app boots (and even mid-session);
  // /api/songs rescans the dir per request.
  provisionMusic().catch((err: unknown) => logLine(`[music] provision failed: ${String(err)}\n`));

  // Runtime tools (HLAE, FFmpeg, yt-dlp) must be resolved before the
  // orchestrator spawns (tool paths are read once at startup). First boot
  // downloads ~110 MB total; later boots return the cached installs instantly.
  // Each tool has its own timeout, so worst case the app boots without one and
  // the UI says which feature is unconfigured.
  setLoadingStatus('Descargando herramientas (~110 MB, solo el primer arranque)…');
  const toolEnv = await provisionTools((name, detail) =>
    setLoadingStatus(`Descargando ${TOOL_LABELS[name] || name}${detail ? ` (${detail})` : ''}…`));

  // The orchestrator: SQLite job repo on disk (so reels survive an app restart)
  // + inline queue, capture and render tools from the provisioned env above,
  // anything missing auto-detected (CS2/recorder/editor). "sqlite" with no
  // path stores <dataDir>/jobs.db. Loopback bind needs no mutation token.
  setLoadingStatus('Iniciando el orquestador…');
  const orch = launch('orchestrator', orchestratorExe, [], {
    ZV_DATABASE_URL: 'sqlite',
    ZV_DATA_DIR: dataDir,
    ZV_HTTP_ADDR: `${LOOPBACK_HOST}:${orchPort}`,
    ZV_MUSIC_DIR: musicDir,
    ...toolEnv,
  });

  // The Next.js standalone server, run by Electron's own Node (no separate Node
  // runtime shipped). NEXT_PUBLIC_FRAGFORGE_MODE is baked into the client bundle
  // at build time; it is set here too for the server route handlers.
  setLoadingStatus('Iniciando el servidor web…');
  const web = launch('web', process.execPath, [nextServer], {
    ELECTRON_RUN_AS_NODE: '1',
    NODE_ENV: 'production',
    PORT: String(webPort),
    HOSTNAME: LOOPBACK_HOST,
    NEXT_PUBLIC_FRAGFORGE_MODE: 'local',
    ORCHESTRATOR_URL: orchestratorUrl,
  });

  try {
    // Racing against child exit turns "timed out waiting for healthz" into the
    // real cause when a backend dies at startup instead of coming up slowly.
    await Promise.race([waitForHttp(`${orchestratorUrl}/healthz`, BOOT_HEALTH_TIMEOUT_MS), orch.exited]);
    await Promise.race([waitForHttp(`http://${LOOPBACK_HOST}:${webPort}/`, BOOT_HEALTH_TIMEOUT_MS), web.exited]);
  } catch (err) {
    logLine(`[boot] failed: ${String(err)}\n`);
    showErrorScreen(err);
    return;
  }

  // Boot succeeded, but the children can still die later (the orchestrator
  // loses its capture device, Next.js hits a fatal error, etc.); watch both
  // for the rest of the session so a post-boot crash shows the same error
  // screen instead of leaving the window frozen on stale content.
  const watchPostBoot = (child: Launched): void => {
    child.exited.catch((err: unknown) => {
      if (quitting) return;
      logLine(`[boot] post-boot crash: ${String(err)}\n`);
      showErrorScreen(
        err,
        'FragForge Studio se ha detenido',
        'El backend se detuvo de forma inesperada. Cierra y vuelve a abrir la app.',
      );
    });
  };
  watchPostBoot(orch);
  watchPostBoot(web);

  // Land on the dashboard (the app shell), not a single flow: Studio has both
  // the demo-upload flow and the Twitch stream-clips flow, and the sidebar is
  // the only place that offers both.
  setLoadingStatus('Abriendo la interfaz…');
  loadMatches();
}

// Guards against overlapping boot() runs: the initial app.whenReady() boot
// and a user mashing the error screen's retry button both go through
// runBoot(), which is a no-op while a boot is already in flight.
let booting = false;

/** Runs boot(), refusing to start a second one concurrently (ports/children would race). */
function runBoot(): void {
  if (booting) return;
  booting = true;
  boot()
    .catch((err: unknown) => logLine(`[boot] unexpected error: ${String(err)}\n`))
    .finally(() => {
      booting = false;
    });
}

/**
 * Retry handler for the error screen's "Reintentar" button (wired through
 * will-navigate, see createWindow()). Kills off whatever children the failed
 * attempt left running, drops them from the tracking list, and re-enters
 * boot() from the top on the same window.
 */
function retryBoot(): void {
  if (booting) return;
  for (const child of children) killTree(child);
  children.length = 0;
  runBoot();
}

// Set in 'before-quit' so post-boot crash watching (above) and
// render-process-gone (in createWindow) don't fight an intentional shutdown
// by throwing up an error screen or reloading a window that's going away.
let quitting = false;

/** Kills one tracked child and, on Windows, its whole descendant tree (the orchestrator spawns zv-recorder -> HLAE -> cs2.exe). */
function killTree(child: ChildProcess | null): void {
  if (!child || !child.pid) return;
  if (child.killed || child.exitCode !== null) return;
  if (process.platform === 'win32') {
    // child.kill() only signals the direct process; grandchildren would
    // survive as orphans holding the GPU/capture device, so ask Windows to
    // tear down the whole process tree instead. Synchronous so this finishes
    // before 'before-quit'/'exit' lets the app close.
    spawnSync('taskkill', ['/pid', String(child.pid), '/T', '/F'], { windowsHide: true });
  } else {
    child.kill();
  }
}

function shutdown(): void {
  for (const child of children) killTree(child);
}

app.on('second-instance', () => {
  const win = aliveWindow();
  if (win === null) return;
  if (win.isMinimized()) win.restore();
  win.focus();
});

app.whenReady().then(() => {
  // Local app needs no browser permissions (camera/mic/geolocation/notifications/etc).
  session.defaultSession.setPermissionRequestHandler((_wc, _permission, callback) => callback(false));
  runBoot();
});

app.on('window-all-closed', () => app.quit());
app.on('before-quit', () => {
  quitting = true;
  shutdown();
});
process.on('exit', shutdown);
