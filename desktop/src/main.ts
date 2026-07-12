// FragForge Studio - Electron main process.
//
// This is the desktop wrapper around Local Studio: it boots the same two
// processes the local-studio.ps1 launcher does (the Go orchestrator and the
// Next.js server in local mode), then shows the web UI in a native window.
// Capture (HLAE/CS2) is driven by the orchestrator exactly as before; Electron
// only replaces the "install Node, run a script, open a browser" friction.
//
// Both children bind loopback on per-install ports persisted in user data. The
// ports stay stable across launches when available and are reallocated if a
// stray process has claimed one.

import { app, BrowserWindow, shell, session } from 'electron';
import * as path from 'node:path';
import * as fs from 'node:fs';
import { pathToFileURL } from 'node:url';
import { escapeHtml } from './escaping';
import { validateWindowState, type WindowState } from './window-state';
import { lastLines } from './log-tail';
import { provisionRuntimeTools, RUNTIME_TOOL_LABELS } from './runtime-tools';
import { ProcessSession, type LaunchedProcess } from './process-session';
import { waitForDesktopServices } from './service-health';
import { provisionMusicLibrary } from './music-library';
import { allocateStableServicePorts } from './stable-ports';
import { resolveXAIAPIKey } from './xai-api-key';

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
const bundledTeamXAIKeyPath = resourcePath('team', 'xai-api-key');

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

const portsFile = path.join(app.getPath('userData'), 'ports.json');

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

function createWindow(): BrowserWindow {
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
  void win.loadURL(url).catch((loadErr: unknown) => {
    logLine(`[window] could not load error screen: ${String(loadErr)}\n`);
  });
}

interface BootAttempt {
  controller: AbortController;
  processes: ProcessSession;
}

interface BootFailureDetails {
  title?: string;
  hint?: string;
  logLabel?: string;
}

let activeBootAttempt: BootAttempt | null = null;

/** Sends the window to the app shell once boot is done. */
async function loadMatches(webPort: number): Promise<void> {
  loadingScreenShowing = false;
  const win = aliveWindow();
  if (win === null) throw new Error('main window is unavailable');
  await win.loadURL(`http://${LOOPBACK_HOST}:${webPort}/matches`);
}

// Generous overall deadline for each server to answer its health check.
const BOOT_HEALTH_TIMEOUT_MS = 60_000;

async function boot(): Promise<void> {
  if (quitting) return;
  if (activeBootAttempt !== null) throw new Error('cannot start a boot while another attempt is active');
  const attempt: BootAttempt = {
    controller: new AbortController(),
    processes: new ProcessSession({ logLine }),
  };
  activeBootAttempt = attempt;

  try {
    await runBootAttempt(attempt);
  } catch (err) {
    if (quitting || attempt.controller.signal.aborted || activeBootAttempt !== attempt) return;
    failBootAttempt(attempt, err);
  }
}

async function runBootAttempt(attempt: BootAttempt): Promise<void> {
  assertBootAttemptActive(attempt);
  // Reuse the existing window on retry instead of opening another one over the
  // error screen from the failed attempt.
  const existing = aliveWindow();
  const bootWindow = existing ?? createWindow();
  await bootWindow.loadFile(loadingHtmlPath);
  assertBootAttemptActive(attempt);
  loadingScreenShowing = true;
  allowedOrigins.clear();
  allowedInternalUrls.clear();

  // The normal build stages an empty file; the internal team build stages the
  // shared subtitle credential. A local environment value wins for emergency
  // rotation, and the result is passed only to the Go orchestrator below.
  const xaiAPIKey = resolveXAIAPIKey({
    environmentValue: process.env.XAI_API_KEY,
    bundledPath: bundledTeamXAIKeyPath,
  });

  // Tracks can land in the background; the API rescans the music dir per request.
  provisionMusicLibrary({
    bundledMusicDir: resourcePath('music'),
    musicDir,
    signal: attempt.controller.signal,
    logLine,
  }).catch((err: unknown) => {
    if (!attempt.controller.signal.aborted) logLine(`[music] provision failed: ${String(err)}\n`);
  });

  setLoadingStatus('Descargando herramientas (~110 MB, solo el primer arranque)…');
  const toolEnv = await provisionRuntimeTools(
    {
      toolsDir: path.join(app.getPath('userData'), 'tools'),
      logLine,
      signal: attempt.controller.signal,
    },
    (name, detail) =>
      setLoadingStatus(`Descargando ${RUNTIME_TOOL_LABELS[name]}${detail ? ` (${detail})` : ''}…`),
  );
  assertBootAttemptActive(attempt);

  // Probe immediately before launch. Runtime provisioning can take minutes on
  // first boot, so selecting ports before it would leave a long window for an
  // unrelated process to claim a released probe port.
  setLoadingStatus('Eligiendo puertos libres…');
  const { orchestrator: orchPort, web: webPort } = await allocateStableServicePorts({
    host: LOOPBACK_HOST,
    portsFile,
    logLine,
    signal: attempt.controller.signal,
  });
  assertBootAttemptActive(attempt);
  const orchestratorUrl = `http://${LOOPBACK_HOST}:${orchPort}`;
  allowedOrigins.add(`http://${LOOPBACK_HOST}:${orchPort}`);
  allowedOrigins.add(`http://${LOOPBACK_HOST}:${webPort}`);

  setLoadingStatus('Iniciando el orquestador…');
  const orch = attempt.processes.launch('orchestrator', orchestratorExe, [], {
    ZV_DATABASE_URL: 'sqlite',
    ZV_DATA_DIR: dataDir,
    ZV_HTTP_ADDR: `${LOOPBACK_HOST}:${orchPort}`,
    ZV_MUSIC_DIR: musicDir,
    XAI_API_KEY: xaiAPIKey,
    ...toolEnv,
  });

  setLoadingStatus('Iniciando el servidor web…');
  const web = attempt.processes.launch('web', process.execPath, [nextServer], {
    ELECTRON_RUN_AS_NODE: '1',
    NODE_ENV: 'production',
    PORT: String(webPort),
    HOSTNAME: LOOPBACK_HOST,
    ORCHESTRATOR_URL: orchestratorUrl,
    // ProcessSession normally inherits the desktop environment. Explicitly
    // remove this server-irrelevant secret from the Next child.
    XAI_API_KEY: undefined,
  });

  // Either child dying is terminal during either health wait. Cancelling the
  // attempt also tears down whichever HTTP poll loses the race.
  const childExited = Promise.race([orch.exited, web.exited]);
  await waitForDesktopServices({
    orchestratorUrl,
    webUrl: `http://${LOOPBACK_HOST}:${webPort}/`,
    timeoutMs: BOOT_HEALTH_TIMEOUT_MS,
    signal: attempt.controller.signal,
    childExited,
  });
  assertBootAttemptActive(attempt);

  const watchPostBoot = (child: LaunchedProcess): void => {
    attempt.processes.watchUnexpectedExit(child, (err: unknown) => {
      if (quitting || activeBootAttempt !== attempt) return;
      failBootAttempt(attempt, err, {
        title: 'FragForge Studio se ha detenido',
        hint: 'El backend se detuvo de forma inesperada. Cierra y vuelve a abrir la app.',
        logLabel: 'post-boot crash',
      });
    });
  };
  watchPostBoot(orch);
  watchPostBoot(web);

  setLoadingStatus('Abriendo la interfaz…');
  allowedInternalUrls.clear();
  await loadMatches(webPort);
  assertBootAttemptActive(attempt);
}

function failBootAttempt(attempt: BootAttempt, err: unknown, details: BootFailureDetails = {}): void {
  if (activeBootAttempt !== attempt) return;
  attempt.controller.abort();
  const stopped = attempt.processes.stop();
  if (stopped) activeBootAttempt = null;
  allowedOrigins.clear();
  allowedInternalUrls.clear();
  logLine(`[boot] ${details.logLabel ?? 'failed'}: ${String(err)}\n`);
  if (!quitting) showErrorScreen(err, details.title, details.hint);
}

function assertBootAttemptActive(attempt: BootAttempt): void {
  if (quitting || attempt.controller.signal.aborted || activeBootAttempt !== attempt) {
    throw new Error('boot attempt cancelled');
  }
}

function stopActiveBootAttempt(): boolean {
  const attempt = activeBootAttempt;
  allowedOrigins.clear();
  allowedInternalUrls.clear();
  if (attempt === null) return true;
  attempt.controller.abort();
  const stopped = attempt.processes.stop();
  if (stopped && activeBootAttempt === attempt) activeBootAttempt = null;
  return stopped;
}

// Guards against overlapping boot() runs from startup and Retry.
let booting = false;

function runBoot(): void {
  if (booting || quitting) return;
  booting = true;
  boot()
    .catch((err: unknown) => logLine(`[boot] unexpected error: ${String(err)}\n`))
    .finally(() => {
      booting = false;
    });
}

function retryBoot(): void {
  if (booting || quitting) return;
  if (!stopActiveBootAttempt()) {
    logLine('[boot] retry deferred because an existing process tree could not be stopped\n');
    return;
  }
  runBoot();
}

// Prevent crash watchers and retries from fighting an intentional shutdown.
let quitting = false;

function shutdown(): void {
  stopActiveBootAttempt();
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
