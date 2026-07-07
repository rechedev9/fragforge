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

const { app, BrowserWindow, shell, session } = require('electron');
const { spawn, spawnSync } = require('child_process');
const crypto = require('crypto');
const path = require('path');
const fs = require('fs');
const http = require('http');
const https = require('https');
const net = require('net');

// A single running instance owns the orchestrator; a second launch just focuses
// the existing window instead of spawning a duplicate backend on new ports.
if (!app.requestSingleInstanceLock()) {
  app.quit();
  process.exit(0);
}

/**
 * Resolves a bundled resource for both the packaged app and `electron .` (dev).
 * Both read the same assembled layout: packaged from process.resourcesPath, dev
 * from ./build-resources (run `npm run assemble` first to produce it), so the
 * Next server always has its .next/static staged next to server.js.
 */
function resourcePath(...parts) {
  const base = app.isPackaged ? process.resourcesPath : path.join(__dirname, 'build-resources');
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
// invisible. Truncated on every launch; the error screen shows its tail.
const logFile = path.join(app.getPath('userData'), 'studio.log');
let logStream = null;
function logLine(text) {
  process.stdout.write(text);
  try {
    if (!logStream) logStream = fs.createWriteStream(logFile, { flags: 'w' });
    logStream.write(text);
  } catch {
    // Logging must never break the app; stdout still has the line in dev.
  }
}

// Third-party tools the pipeline needs at runtime but that cannot ship inside
// the installer (size, licensing hygiene): they are provisioned into userData
// on first boot from pinned release URLs with pinned sha256 digests, then
// handed to the orchestrator via env vars (env always wins over its
// auto-detection). Every download is best-effort: a failure just leaves that
// feature unconfigured, the UI explains, and the next boot retries.
//
// - HLAE (advancedfx, MIT): drives CS2 capture. Zip ships its LICENSES/.
// - FFmpeg (BtbN autobuild, GPL): every render (reels and stream clips) shells
//   out to ffmpeg/ffprobe; without it nothing can be rendered on a clean PC.
//   Pinned to a dated autobuild tag because the "latest" tag's assets are
//   replaced daily and would break the hash.
// - yt-dlp (Unlicense): fetches Twitch/YouTube sources for Stream Clips.
const toolsRoot = () => path.join(app.getPath('userData'), 'tools');
const TOOLS = {
  hlae: {
    version: '2.190.1',
    url: 'https://github.com/advancedfx/advancedfx/releases/download/v2.190.1/hlae_2_190_1.zip',
    sha256: 'b8c0a6d99201ba017e877c3ba95fd1c3a60b33dc1159218828c7c0a785e59ca3',
    kind: 'zip',
    exeRel: 'HLAE.exe',
    timeoutMs: 90_000,
    // Pre-tools-dir layout kept for installs provisioned by 0.2.11/0.2.12.
    legacyDir: () => path.join(app.getPath('userData'), 'hlae', '2.190.1'),
  },
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

/** Downloads url to destPath and returns its sha256 hex digest. */
async function downloadFile(url, destPath) {
  const res = await fetchStream(url);
  const hash = crypto.createHash('sha256');
  await new Promise((resolve, reject) => {
    const out = fs.createWriteStream(destPath);
    res.on('data', (chunk) => hash.update(chunk));
    res.pipe(out);
    res.on('error', reject);
    out.on('error', reject);
    out.on('finish', resolve);
  });
  return hash.digest('hex');
}

/** Single-quote a string for a PowerShell command ('' escapes ' inside). */
function psQuote(s) {
  return `'${s.replace(/'/g, "''")}'`;
}

/** Expand-Archive via PowerShell (ships with every Windows 10/11). */
function expandArchive(zipPath, destDir) {
  return new Promise((resolve, reject) => {
    const ps = spawn(
      'powershell.exe',
      ['-NoProfile', '-NonInteractive', '-Command',
        `Expand-Archive -LiteralPath ${psQuote(zipPath)} -DestinationPath ${psQuote(destDir)} -Force`],
      { windowsHide: true },
    );
    let stderr = '';
    ps.stderr.on('data', (b) => { stderr += b; });
    ps.on('error', reject);
    ps.on('exit', (code) => (code === 0 ? resolve() : reject(new Error(`Expand-Archive exited ${code}: ${stderr.trim()}`))));
  });
}

/**
 * Ensures one tool from TOOLS is installed under userData\tools and returns
 * its exe path, or '' on failure (offline, bad hash, timeout); the caller then
 * simply omits the env var and the orchestrator falls back to auto-detection.
 * Idempotent: a cached install returns instantly. Each tool is capped by its
 * own timeout so one stalled download can never wedge the boot.
 */
async function provisionTool(name, onStatus) {
  const tool = TOOLS[name];
  const dir = path.join(toolsRoot(), name, tool.version);
  const exe = path.join(dir, tool.exeRel);
  if (fs.existsSync(exe)) return exe;
  if (tool.legacyDir) {
    const legacy = path.join(tool.legacyDir(), tool.exeRel);
    if (fs.existsSync(legacy)) return legacy;
  }
  // Only fires when a real download is about to start (cache hits above never
  // reach here), so the loading screen doesn't flash a status for instant boots.
  if (onStatus) onStatus(name);

  const work = (async () => {
    fs.mkdirSync(dir, { recursive: true });
    const part = path.join(dir, 'download.part');
    const zipPath = path.join(dir, 'download.zip');
    try {
      logLine(`[tools] downloading ${name} ${tool.version}...\n`);
      const digest = await downloadFile(tool.url, part);
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
      new Promise((_, reject) =>
        setTimeout(() => reject(new Error(`timed out after ${tool.timeoutMs}ms`)), tool.timeoutMs)),
    ]);
  } catch (err) {
    logLine(`[tools] ${name} provisioning failed (feature stays unconfigured, retried next boot): ${err}\n`);
    return '';
  }
}

// Display names for the loading-screen per-tool status lines; TOOLS keys are
// the internal provisioning names, these are what a user recognizes.
const TOOL_LABELS = { hlae: 'HLAE', ffmpeg: 'FFmpeg', ytdlp: 'yt-dlp' };

/**
 * Provisions all runtime tools concurrently and returns the env vars to hand
 * the orchestrator. ffprobe.exe sits next to ffmpeg.exe in the same archive.
 * onStatus(name), if given, fires once per tool that actually needs a download.
 */
async function provisionTools(onStatus) {
  if (process.platform !== 'win32') return {};
  const [hlae, ffmpeg, ytdlp] = await Promise.all([
    provisionTool('hlae', onStatus),
    provisionTool('ffmpeg', onStatus),
    provisionTool('ytdlp', onStatus),
  ]);
  const env = {};
  if (hlae) env.ZV_HLAE_PATH = hlae;
  if (ffmpeg) {
    env.ZV_FFMPEG_PATH = ffmpeg;
    const ffprobe = path.join(path.dirname(ffmpeg), 'ffprobe.exe');
    if (fs.existsSync(ffprobe)) env.ZV_FFPROBE_PATH = ffprobe;
  }
  if (ytdlp) env.ZV_YTDLP_PATH = ytdlp;
  return env;
}

/** GET that follows redirects (archive.org download links bounce to mirrors). */
function fetchStream(url, redirectsLeft = 5) {
  return new Promise((resolve, reject) => {
    const get = url.startsWith('https:') ? https.get : http.get;
    const req = get(url, (res) => {
      if (res.statusCode >= 300 && res.statusCode < 400 && res.headers.location && redirectsLeft > 0) {
        res.resume();
        return resolve(fetchStream(new URL(res.headers.location, url).toString(), redirectsLeft - 1));
      }
      if (res.statusCode !== 200) {
        res.resume();
        return reject(new Error(`GET ${url}: HTTP ${res.statusCode}`));
      }
      resolve(res);
    });
    req.on('error', reject);
    req.setTimeout(60000, () => req.destroy(new Error(`GET ${url}: timed out`)));
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
async function provisionMusic() {
  const bundledCatalog = resourcePath('music', 'catalog.json');
  if (!fs.existsSync(bundledCatalog)) return;
  fs.mkdirSync(musicDir, { recursive: true });
  fs.copyFileSync(bundledCatalog, path.join(musicDir, 'catalog.json'));

  let tracks;
  try {
    tracks = JSON.parse(fs.readFileSync(bundledCatalog, 'utf8')).tracks ?? [];
  } catch (err) {
    logLine(`[music] bad catalog.json: ${err}\n`);
    return;
  }
  for (const t of tracks) {
    if (!t.id || !t.ext) continue;
    const dest = path.join(musicDir, `${t.id}.${t.ext}`);
    if (fs.existsSync(dest)) continue;
    // Local-only tracks (no downloadUrl, e.g. AI-generated) ship inside the
    // installer's music resources; copy them instead of downloading.
    if (!t.downloadUrl) {
      const bundledAudio = resourcePath('music', `${t.id}.${t.ext}`);
      if (fs.existsSync(bundledAudio)) {
        fs.copyFileSync(bundledAudio, dest);
        logLine(`[music] copied bundled ${t.id}.${t.ext}\n`);
      } else {
        logLine(`[music] skip ${t.id}: no downloadUrl and no bundled audio\n`);
      }
      continue;
    }
    // Download to a temp name and rename so a half-written file never shows up
    // as a playable track.
    const tmp = `${dest}.part`;
    try {
      const res = await fetchStream(t.downloadUrl);
      await new Promise((resolve, reject) => {
        const out = fs.createWriteStream(tmp);
        res.pipe(out);
        res.on('error', reject);
        out.on('error', reject);
        out.on('finish', resolve);
      });
      fs.renameSync(tmp, dest);
      logLine(`[music] downloaded ${t.id}.${t.ext}\n`);
    } catch (err) {
      fs.rmSync(tmp, { force: true });
      logLine(`[music] skip ${t.id}: ${err}\n`);
    }
  }
}

/** Grabs an OS-assigned free loopback port, then releases it for the child. */
function freePort() {
  return new Promise((resolve, reject) => {
    const srv = net.createServer();
    srv.unref();
    srv.on('error', reject);
    srv.listen(0, '127.0.0.1', () => {
      const { port } = srv.address();
      srv.close(() => resolve(port));
    });
  });
}

/** Reports whether a specific loopback port is currently free. */
function portFree(port) {
  return new Promise((resolve) => {
    const srv = net.createServer();
    srv.unref();
    srv.on('error', () => resolve(false));
    srv.listen(port, '127.0.0.1', () => srv.close(() => resolve(true)));
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
async function stablePort(key) {
  let saved = {};
  try {
    saved = JSON.parse(fs.readFileSync(portsFile, 'utf8'));
  } catch {
    // first launch or unreadable file; fall through to picking a new port
  }
  if (Number.isInteger(saved[key]) && (await portFree(saved[key]))) return saved[key];
  const port = await freePort();
  try {
    fs.writeFileSync(portsFile, JSON.stringify({ ...saved, [key]: port }));
  } catch (err) {
    logLine(`[ports] could not persist ${key} port: ${err}\n`);
  }
  return port;
}

/** Polls an HTTP URL until it answers 2xx/3xx or the timeout elapses. */
function waitForHttp(url, timeoutMs) {
  const deadline = Date.now() + timeoutMs;
  return new Promise((resolve, reject) => {
    const attempt = () => {
      const req = http.get(url, (res) => {
        res.resume();
        if (res.statusCode && res.statusCode < 400) return resolve();
        retry();
      });
      req.on('error', retry);
      req.setTimeout(2000, () => req.destroy());
    };
    const retry = () => {
      if (Date.now() > deadline) return reject(new Error(`timed out waiting for ${url}`));
      setTimeout(attempt, 400);
    };
    attempt();
  });
}

const children = [];

/**
 * Spawns a child, tracks it for shutdown, and prefixes its logs (mirrored to
 * studio.log). The returned exited promise rejects the moment the child dies
 * or fails to spawn, so boot() can fail fast with the real cause (an AV
 * quarantine or a crashing backend exits in milliseconds) instead of sitting
 * out the full healthz timeout and reporting a useless "timed out".
 */
function launch(label, exe, args, env) {
  const child = spawn(exe, args, { env: { ...process.env, ...env }, windowsHide: true });
  children.push(child);
  const tag = (buf) => logLine(`[${label}] ${buf}`);
  child.stdout.on('data', tag);
  child.stderr.on('data', tag);
  const exited = new Promise((_, reject) => {
    child.on('error', (err) => {
      logLine(`[${label}] failed to start: ${err}\n`);
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

/** Escapes the HTML-sensitive characters in untrusted text (log lines, error messages) before it is dropped into a loaded page. */
function escapeHtml(s) {
  return String(s)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

/** Last lines of studio.log, HTML-escaped for the error screen. */
function logTail(maxLines = 40) {
  try {
    const lines = fs.readFileSync(logFile, 'utf8').split('\n');
    return escapeHtml(lines.slice(-maxLines).join('\n'));
  } catch {
    return '(sin registro)';
  }
}

let mainWindow = null;

// Origins the window is allowed to navigate to on its own, populated once the
// boot-time ports are known (see boot()). Referenced by the handlers below via
// closure, so it is safe to register those handlers before the ports exist.
const allowedOrigins = new Set();

/** True if url's origin is one of the loopback servers we just spawned. */
function isLoopbackOrigin(url) {
  try {
    return allowedOrigins.has(new URL(url).origin);
  } catch {
    return false;
  }
}

const windowFile = path.join(app.getPath('userData'), 'window.json');

/** Reads saved window bounds, falling back to a sane default if missing, corrupt, or implausibly small. */
function loadWindowBounds() {
  const fallback = { width: 1280, height: 900 };
  try {
    const saved = JSON.parse(fs.readFileSync(windowFile, 'utf8'));
    const { width, height, x, y } = saved;
    if (!Number.isFinite(width) || !Number.isFinite(height) || width < 800 || height < 600) {
      return fallback;
    }
    const bounds = { width, height };
    if (Number.isFinite(x) && Number.isFinite(y)) {
      bounds.x = x;
      bounds.y = y;
    }
    return bounds;
  } catch {
    return fallback;
  }
}

/** Best-effort save of the window's current size/position so the app reopens where the user left it. */
function saveWindowBounds() {
  if (!mainWindow) return;
  try {
    fs.writeFileSync(windowFile, JSON.stringify(mainWindow.getBounds()));
  } catch (err) {
    logLine(`[window] could not persist bounds: ${err}\n`);
  }
}

// Guards mainWindow.reload() so a crash-loop in the renderer reloads once
// instead of hammering a dead server forever.
let renderProcessGoneReloaded = false;

function createWindow(loadingOnly) {
  mainWindow = new BrowserWindow({
    ...loadWindowBounds(),
    backgroundColor: '#0a0a0a',
    title: 'FragForge Studio',
    webPreferences: { contextIsolation: true, nodeIntegration: false, sandbox: true },
  });
  mainWindow.removeMenu();
  mainWindow.on('close', saveWindowBounds);

  // Deny every window.open (no popups in a kiosk-style desktop wrapper);
  // an http(s) target that isn't our own loopback server is handed to the
  // user's real browser instead of silently vanishing.
  mainWindow.webContents.setWindowOpenHandler(({ url }) => {
    if (/^https?:\/\//i.test(url) && !isLoopbackOrigin(url)) shell.openExternal(url);
    return { action: 'deny' };
  });

  // Same idea for in-window navigation: only our own orchestrator/web origins
  // (plus the local loading.html file and the inline data: error screen) may
  // navigate the window directly; anything else opens externally instead.
  mainWindow.webContents.on('will-navigate', (event, url) => {
    if (url.startsWith('data:') || url.startsWith('file://') || isLoopbackOrigin(url)) return;
    event.preventDefault();
    if (/^https?:\/\//i.test(url)) shell.openExternal(url);
  });

  mainWindow.webContents.on('render-process-gone', (_event, details) => {
    logLine(`[window] render process gone: ${JSON.stringify(details)}\n`);
    if (!renderProcessGoneReloaded && !quitting) {
      renderProcessGoneReloaded = true;
      mainWindow.reload();
    }
  });

  if (loadingOnly) mainWindow.loadFile(path.join(__dirname, 'loading.html'));
  return mainWindow;
}

// True while the loading.html screen is the thing on screen, so
// setLoadingStatus knows whether its target element can even exist.
let loadingScreenShowing = false;

/** Updates the loading screen's #status line; a silent no-op once we've navigated away from it (real app, or error screen). */
function setLoadingStatus(text) {
  if (!mainWindow || !loadingScreenShowing) return;
  mainWindow.webContents
    .executeJavaScript(
      `(() => { const el = document.getElementById('status'); if (el) el.textContent = ${JSON.stringify(text)}; })()`,
    )
    .catch(() => {}); // the page may already be gone; never block boot on this
}

/** Renders the fatal-error screen as a data: URL, so it never depends on the servers that just failed or died. */
function showErrorScreen(err, title, hint) {
  loadingScreenShowing = false;
  if (!mainWindow) return;
  const defaultTitle = 'FragForge Studio no pudo arrancar';
  const defaultHint =
    'Si un antivirus ha bloqueado o puesto en cuarentena archivos de FragForge, restáuralos y vuelve a abrir la app.';
  const html = `<!doctype html><html><head><meta charset="utf-8">
    <meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'">
    <style>body{font:16px system-ui;background:#0a0a0a;color:#eee;padding:2rem}</style></head>
    <body>
      <h2>${escapeHtml(title || defaultTitle)}</h2>
      <p>${escapeHtml(err)}</p>
      <p style="color:#999">${hint || defaultHint} Registro completo: ${escapeHtml(logFile)}</p>
      <pre style="background:#111;padding:1rem;overflow:auto;max-height:40vh;font-size:12px">${logTail()}</pre>
    </body></html>`;
  mainWindow.loadURL('data:text/html;charset=utf-8,' + encodeURIComponent(html));
}

async function boot() {
  createWindow(true);
  loadingScreenShowing = true;

  setLoadingStatus('Eligiendo puertos libres…');
  const orchPort = await stablePort('orchestrator');
  const webPort = await stablePort('web');
  const orchestratorUrl = `http://127.0.0.1:${orchPort}`;
  // Ports are only known now, so this is the earliest point the navigation
  // guards in createWindow() have anything real to allow.
  allowedOrigins.add(`http://127.0.0.1:${orchPort}`);
  allowedOrigins.add(`http://127.0.0.1:${webPort}`);

  // Fire-and-forget: tracks land while the app boots (and even mid-session);
  // /api/songs rescans the dir per request.
  provisionMusic().catch((err) => logLine(`[music] provision failed: ${err}\n`));

  // Runtime tools (HLAE, FFmpeg, yt-dlp) must be resolved before the
  // orchestrator spawns (tool paths are read once at startup). First boot
  // downloads ~110 MB total; later boots return the cached installs instantly.
  // Each tool has its own timeout, so worst case the app boots without one and
  // the UI says which feature is unconfigured.
  setLoadingStatus('Descargando herramientas (~110 MB, solo el primer arranque)…');
  const toolEnv = await provisionTools((name) =>
    setLoadingStatus(`Descargando ${TOOL_LABELS[name] || name}…`));

  // The orchestrator: SQLite job repo on disk (so reels survive an app restart)
  // + inline queue, capture and render tools from the provisioned env above,
  // anything missing auto-detected (CS2/recorder/editor). "sqlite" with no
  // path stores <dataDir>/jobs.db. Loopback bind needs no mutation token.
  setLoadingStatus('Iniciando el orquestador…');
  const orch = launch('orchestrator', orchestratorExe, [], {
    ZV_DATABASE_URL: 'sqlite',
    ZV_DATA_DIR: dataDir,
    ZV_HTTP_ADDR: `127.0.0.1:${orchPort}`,
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
    HOSTNAME: '127.0.0.1',
    NEXT_PUBLIC_FRAGFORGE_MODE: 'local',
    ORCHESTRATOR_URL: orchestratorUrl,
  });

  try {
    // Racing against child exit turns "timed out waiting for healthz" into the
    // real cause when a backend dies at startup instead of coming up slowly.
    await Promise.race([waitForHttp(`${orchestratorUrl}/healthz`, 60000), orch.exited]);
    await Promise.race([waitForHttp(`http://127.0.0.1:${webPort}/`, 60000), web.exited]);
  } catch (err) {
    logLine(`[boot] failed: ${err}\n`);
    showErrorScreen(err);
    return;
  }

  // Boot succeeded, but the children can still die later (the orchestrator
  // loses its capture device, Next.js hits a fatal error, etc.); watch both
  // for the rest of the session so a post-boot crash shows the same error
  // screen instead of leaving the window frozen on stale content.
  const watchPostBoot = (child) =>
    child.exited.catch((err) => {
      if (quitting) return;
      logLine(`[boot] post-boot crash: ${err}\n`);
      showErrorScreen(
        err,
        'FragForge Studio se ha detenido',
        'El backend se detuvo de forma inesperada. Cierra y vuelve a abrir la app.',
      );
    });
  watchPostBoot(orch);
  watchPostBoot(web);

  // Land on the dashboard (the app shell), not a single flow: Studio has both
  // the demo-upload flow and the Twitch stream-clips flow, and the sidebar is
  // the only place that offers both.
  setLoadingStatus('Abriendo la interfaz…');
  loadingScreenShowing = false;
  if (mainWindow) mainWindow.loadURL(`http://127.0.0.1:${webPort}/matches`);
}

// Set in 'before-quit' so post-boot crash watching (above) and
// render-process-gone (in createWindow) don't fight an intentional shutdown
// by throwing up an error screen or reloading a window that's going away.
let quitting = false;

/** Kills one tracked child and, on Windows, its whole descendant tree (the orchestrator spawns zv-recorder -> HLAE -> cs2.exe). */
function killTree(child) {
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

function shutdown() {
  for (const child of children) killTree(child);
}

app.on('second-instance', () => {
  if (mainWindow) {
    if (mainWindow.isMinimized()) mainWindow.restore();
    mainWindow.focus();
  }
});

app.whenReady().then(() => {
  // Local app needs no browser permissions (camera/mic/geolocation/notifications/etc).
  session.defaultSession.setPermissionRequestHandler((_wc, _permission, callback) => callback(false));
  return boot();
});

app.on('window-all-closed', () => app.quit());
app.on('before-quit', () => {
  quitting = true;
  shutdown();
});
process.on('exit', shutdown);
