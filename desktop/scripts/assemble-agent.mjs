// Assembles the resources the FragForge Agent installer bundles. Unlike
// assemble.mjs (the full Studio app), the Agent does NOT bundle or serve the
// Next web UI - that is hosted on our domain now - so this stages only:
//   build-resources/bin/            -> zv-orchestrator.exe, zv-editor.exe (+ zv.exe, zv-recorder.exe)
//   build-resources/music/          -> catalog.json (+ local-only audio; remote tracks download on first boot)
//   build-resources/agent-mode.flag -> marker that makes main.js boot headless (agentBoot)
//
// The Agent needs no web build, so this is much faster and smaller than the
// Studio assemble. Cross-platform (pure Node fs); the real installer build runs
// on Windows.

import { existsSync, rmSync, mkdirSync, cpSync, writeFileSync, readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const desktop = join(here, '..');
const repo = join(desktop, '..');
const bin = join(repo, 'bin');
const out = join(desktop, 'build-resources');

// zv-orchestrator.exe is the agent backend main.js spawns directly (so quitting
// the agent kills the real server, not a zv intermediary). zv-editor.exe must
// sit in the same bin/ so the orchestrator auto-detects it and enables the
// render worker. zv.exe is staged for CLI use next to the app data.
const zvExe = join(bin, 'zv.exe');
const zvOrchestrator = join(bin, 'zv-orchestrator.exe');
const zvEditor = join(bin, 'zv-editor.exe');
for (const required of [zvExe, zvOrchestrator, zvEditor]) {
  if (!existsSync(required)) {
    console.error(`\nmissing ${required}\nBuild the Go binaries first:  .\\scripts\\build.ps1\n`);
    process.exit(1);
  }
}

console.log('[assemble-agent] staging build-resources/ (headless agent, no web)...');
rmSync(out, { recursive: true, force: true });
mkdirSync(join(out, 'bin'), { recursive: true });

cpSync(zvExe, join(out, 'bin', 'zv.exe'));
cpSync(zvOrchestrator, join(out, 'bin', 'zv-orchestrator.exe'));
cpSync(zvEditor, join(out, 'bin', 'zv-editor.exe'));
const recorder = join(bin, 'zv-recorder.exe');
if (existsSync(recorder)) cpSync(recorder, join(out, 'bin', 'zv-recorder.exe'));

// Music: catalog.json plus any local-only audio (tracks without a downloadUrl).
// Remote CC0/CC-BY tracks are downloaded by main.js on first boot.
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

// The marker main.js checks (agentModeMarkerPresent) to boot headless without
// an env var. Its contents are informational only; presence is what matters.
writeFileSync(
  join(out, 'agent-mode.flag'),
  'FragForge Agent headless mode. Presence of this file makes main.js run agentBoot().\n',
);

console.log('[assemble-agent] done -> ' + out);
