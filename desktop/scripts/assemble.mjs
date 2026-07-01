// Assembles the resources electron-builder bundles into the installer:
//   build-resources/bin/  -> zv.exe (+ zv-recorder.exe)
//   build-resources/web/  -> the Next.js standalone server, ready to run
//
// The Next standalone output does NOT include .next/static or public, so we copy
// them next to server.js the same way the web Dockerfile does. Cross-platform
// (pure Node fs), because the real build runs on Windows.

import { execSync } from 'node:child_process';
import { existsSync, rmSync, mkdirSync, cpSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const desktop = join(here, '..');
const repo = join(desktop, '..');
const web = join(repo, 'web');
const bin = join(repo, 'bin');
const out = join(desktop, 'build-resources');

const zvExe = join(bin, 'zv.exe');
if (!existsSync(zvExe)) {
  console.error(`\nmissing ${zvExe}\nBuild the Go binaries first:  .\\scripts\\build.ps1\n`);
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
const recorder = join(bin, 'zv-recorder.exe');
if (existsSync(recorder)) cpSync(recorder, join(out, 'bin', 'zv-recorder.exe'));

console.log('[assemble] done -> ' + out);
