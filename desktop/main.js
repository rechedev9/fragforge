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
const http = require('http');
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

const zvExe = resourcePath('bin', process.platform === 'win32' ? 'zv.exe' : 'zv');
const nextServer = resourcePath('web', 'server.js');
const dataDir = path.join(app.getPath('userData'), 'data');

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

  const orchPort = await freePort();
  const webPort = await freePort();
  const orchestratorUrl = `http://127.0.0.1:${orchPort}`;

  // The orchestrator: memory mode (in-memory jobs + inline queue), capture
  // auto-detected (HLAE/CS2/recorder). Loopback bind needs no mutation token.
  launch('orchestrator', zvExe, ['serve'], {
    ZV_DATABASE_URL: 'memory',
    ZV_DATA_DIR: dataDir,
    ZV_HTTP_ADDR: `127.0.0.1:${orchPort}`,
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

  if (mainWindow) mainWindow.loadURL(`http://127.0.0.1:${webPort}/upload`);
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
