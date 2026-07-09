// Assembles the resources electron-builder bundles into the installer:
//   build-resources/bin/   -> zv.exe, zv-orchestrator.exe, zv-editor.exe (+ zv-recorder.exe)
//   build-resources/web/   -> the Next.js standalone server, ready to run
//   build-resources/music/ -> catalog.json (track metadata; audio is downloaded on first boot)
//   build-resources/hlae-patch/ -> capture-tested Source 2 hook + corresponding source
//
// The Next standalone output does NOT include .next/static or public, so we copy
// them next to server.js the same way the web Dockerfile does. Cross-platform
// (pure Node fs), because the real build runs on Windows.

import { execSync } from 'node:child_process';
import { existsSync, rmSync, mkdirSync, cpSync, readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const desktop = join(here, '..');
const repo = join(desktop, '..');
const web = join(repo, 'web');
const bin = join(repo, 'bin');
const out = join(desktop, 'build-resources');
const hlaePatch = join(desktop, 'resources', 'hlae-patch');

// zv-orchestrator.exe is the backend main.js spawns (directly, not via `zv
// serve`, so quitting the app kills the real server). zv-editor.exe must sit
// in the same bin/ so the orchestrator auto-detects it and enables the render
// worker; without it every created reel fails after capture with an
// unconfigured render:variant queue. zv.exe is staged for CLI use next to the
// app's data.
const zvExe = join(bin, 'zv.exe');
const zvOrchestrator = join(bin, 'zv-orchestrator.exe');
const zvEditor = join(bin, 'zv-editor.exe');
for (const required of [zvExe, zvOrchestrator, zvEditor]) {
  if (!existsSync(required)) {
    console.error(`\nmissing ${required}\nBuild the Go binaries first:  .\\scripts\\build.ps1\n`);
    process.exit(1);
  }
}
for (const required of ['AfxHookSource2.dll', 'SOURCE.patch', 'LICENSE', 'README.md', 'THIRDPARTY.yml']) {
  const file = join(hlaePatch, required);
  if (!existsSync(file)) {
    console.error(`\nmissing ${file}\nThe verified HLAE patch bundle is incomplete.\n`);
    process.exit(1);
  }
}

// electron-builder picks up build/icon.ico automatically (see desktop/README.md);
// it does not fail loudly if it's missing, it just ships an installer with the
// default Electron icon. Fail here instead, before the (slow) Next.js build
// below, so a missing icon is caught in seconds rather than discovered by
// eyeballing the finished installer.
const iconFile = join(desktop, 'build', 'icon.ico');
if (!existsSync(iconFile)) {
  console.error(`\nmissing ${iconFile}\nelectron-builder needs this for the app/installer icon.\n`);
  process.exit(1);
}

// 1. Build the web in local mode. NEXT_PUBLIC_FRAGFORGE_MODE is inlined into the
//    client bundle at build time, so the desktop distributable is local-only.
console.log('[assemble] building web (NEXT_PUBLIC_FRAGFORGE_MODE=local)...');
execSync('npm run build', {
  cwd: web,
  stdio: 'inherit',
  env: { ...process.env, NEXT_PUBLIC_FRAGFORGE_MODE: 'local' },
});

const standalone = join(web, '.next', 'standalone');
if (!existsSync(join(standalone, 'server.js'))) {
  console.error(`\nexpected ${join(standalone, 'server.js')} - is output:'standalone' set in web/next.config?\n`);
  process.exit(1);
}

// 2. Assemble build-resources/ from scratch.
console.log('[assemble] staging build-resources/...');
rmSync(out, { recursive: true, force: true });
mkdirSync(join(out, 'bin'), { recursive: true });

cpSync(standalone, join(out, 'web'), { recursive: true });
cpSync(join(web, '.next', 'static'), join(out, 'web', '.next', 'static'), { recursive: true });
const publicDir = join(web, 'public');
if (existsSync(publicDir)) cpSync(publicDir, join(out, 'web', 'public'), { recursive: true });

cpSync(zvExe, join(out, 'bin', 'zv.exe'));
cpSync(zvOrchestrator, join(out, 'bin', 'zv-orchestrator.exe'));
cpSync(zvEditor, join(out, 'bin', 'zv-editor.exe'));
const recorder = join(bin, 'zv-recorder.exe');
if (existsSync(recorder)) cpSync(recorder, join(out, 'bin', 'zv-recorder.exe'));
cpSync(hlaePatch, join(out, 'hlae-patch'), { recursive: true });

// Music: catalog.json plus any local-only audio (tracks without a downloadUrl,
// e.g. the AI-generated ones). Remote CC0/CC-BY tracks are still downloaded by
// main.js on first boot, keeping the installer small.
mkdirSync(join(out, 'music'), { recursive: true });
const musicSrc = join(repo, 'data', 'music');
cpSync(join(musicSrc, 'catalog.json'), join(out, 'music', 'catalog.json'));
const musicCatalog = JSON.parse(readFileSync(join(musicSrc, 'catalog.json'), 'utf8'));
for (const t of musicCatalog.tracks ?? []) {
  if (t.downloadUrl || !t.id || !t.ext) continue;
  const audio = join(musicSrc, `${t.id}.${t.ext}`);
  if (!existsSync(audio)) {
    console.error(`\nmissing local-only track audio ${audio} (catalog id ${t.id})\n`);
    process.exit(1);
  }
  cpSync(audio, join(out, 'music', `${t.id}.${t.ext}`));
}

console.log('[assemble] done -> ' + out);
