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

const { app, BrowserWindow } = require('electron');
const { spawn } = require('child_process');
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
    process.stdout.write(`[music] bad catalog.json: ${err}\n`);
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
        process.stdout.write(`[music] copied bundled ${t.id}.${t.ext}\n`);
      } else {
        process.stdout.write(`[music] skip ${t.id}: no downloadUrl and no bundled audio\n`);
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
      process.stdout.write(`[music] downloaded ${t.id}.${t.ext}\n`);
    } catch (err) {
      fs.rmSync(tmp, { force: true });
      process.stdout.write(`[music] skip ${t.id}: ${err}\n`);
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
    process.stdout.write(`[ports] could not persist ${key} port: ${err}\n`);
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

/** Spawns a child, tracks it for shutdown, and prefixes its logs. */
function launch(label, exe, args, env) {
  const child = spawn(exe, args, { env: { ...process.env, ...env }, windowsHide: true });
  children.push(child);
  const tag = (buf) => process.stdout.write(`[${label}] ${buf}`);
  child.stdout.on('data', tag);
  child.stderr.on('data', tag);
  child.on('exit', (code) => process.stdout.write(`[${label}] exited (${code})\n`));
  return child;
}

let mainWindow = null;

function createWindow(loadingOnly) {
  mainWindow = new BrowserWindow({
    width: 1280,
    height: 900,
    backgroundColor: '#0a0a0a',
    title: 'FragForge Studio',
    webPreferences: { contextIsolation: true, nodeIntegration: false },
  });
  mainWindow.removeMenu();
  if (loadingOnly) mainWindow.loadFile(path.join(__dirname, 'loading.html'));
  return mainWindow;
}

async function boot() {
  createWindow(true);

  const orchPort = await stablePort('orchestrator');
  const webPort = await stablePort('web');
  const orchestratorUrl = `http://127.0.0.1:${orchPort}`;

  // Fire-and-forget: tracks land while the app boots (and even mid-session);
  // /api/songs rescans the dir per request.
  provisionMusic().catch((err) => process.stdout.write(`[music] provision failed: ${err}\n`));

  // The orchestrator: SQLite job repo on disk (so reels survive an app restart)
  // + inline queue, capture and render tools auto-detected (HLAE/CS2/recorder/
  // editor/ffmpeg). "sqlite" with no path stores <dataDir>/jobs.db. Loopback
  // bind needs no mutation token.
  launch('orchestrator', orchestratorExe, [], {
    ZV_DATABASE_URL: 'sqlite',
    ZV_DATA_DIR: dataDir,
    ZV_HTTP_ADDR: `127.0.0.1:${orchPort}`,
    ZV_MUSIC_DIR: musicDir,
  });

  // The Next.js standalone server, run by Electron's own Node (no separate Node
  // runtime shipped). NEXT_PUBLIC_FRAGFORGE_MODE is baked into the client bundle
  // at build time; it is set here too for the server route handlers.
  launch('web', process.execPath, [nextServer], {
    ELECTRON_RUN_AS_NODE: '1',
    NODE_ENV: 'production',
    PORT: String(webPort),
    HOSTNAME: '127.0.0.1',
    NEXT_PUBLIC_FRAGFORGE_MODE: 'local',
    ORCHESTRATOR_URL: orchestratorUrl,
  });

  try {
    await waitForHttp(`${orchestratorUrl}/healthz`, 30000);
    await waitForHttp(`http://127.0.0.1:${webPort}/`, 30000);
  } catch (err) {
    if (mainWindow) {
      mainWindow.loadURL(
        'data:text/html,' +
          encodeURIComponent(`<body style="font:16px system-ui;background:#0a0a0a;color:#eee;padding:2rem">
            <h2>FragForge Studio no pudo arrancar</h2><pre>${String(err)}</pre></body>`),
      );
    }
    return;
  }

  // Land on the dashboard (the app shell), not a single flow: Studio has both
  // the demo-upload flow and the Twitch stream-clips flow, and the sidebar is
  // the only place that offers both.
  if (mainWindow) mainWindow.loadURL(`http://127.0.0.1:${webPort}/matches`);
}

function shutdown() {
  for (const child of children) {
    if (!child.killed) child.kill();
  }
}

app.on('second-instance', () => {
  if (mainWindow) {
    if (mainWindow.isMinimized()) mainWindow.restore();
    mainWindow.focus();
  }
});

app.whenReady().then(boot);

app.on('window-all-closed', () => app.quit());
app.on('before-quit', shutdown);
process.on('exit', shutdown);
