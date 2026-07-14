import { execSync } from 'node:child_process';
import { readFileSync, rmSync, statSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { setTimeout as delay } from 'node:timers/promises';
import { fileURLToPath } from 'node:url';
import {
  assembleUsesTeamXAIKey,
  environmentWithoutXAIAPIKey,
  stageTeamXAIKey,
} from './team-xai-key.mjs';

const here = dirname(fileURLToPath(import.meta.url));
const desktop = join(here, '..');
const teamDirectory = join(desktop, 'build-resources', 'team');
const outputDirectory = join(desktop, 'dist-installer');
const packageMetadata = JSON.parse(readFileSync(join(desktop, 'package.json'), 'utf8'));
const installerName = `FragForge Studio Setup ${packageMetadata.version}.exe`;
const installerPaths = [
  join(outputDirectory, installerName),
  join(outputDirectory, `${installerName}.blockmap`),
];
const unpackedKeyPath = join(outputDirectory, 'win-unpacked', 'resources', 'team', 'xai-api-key');

// Remove stale release-shaped outputs and staged credentials before validating
// input. A failed team build can never leave an older same-version installer
// looking publishable.
for (const filePath of [...installerPaths, unpackedKeyPath]) rmSync(filePath, { force: true });
stageTeamXAIKey(teamDirectory, '');

let teamBuild = false;
try {
  teamBuild = assembleUsesTeamXAIKey(process.argv.slice(2));
} catch {
  console.error('[dist] unsupported build argument');
  process.exit(1);
}

const sanitizedEnvironment = environmentWithoutXAIAPIKey();
let failed = false;
try {
  execSync('pnpm run build', { cwd: desktop, env: sanitizedEnvironment, stdio: 'inherit' });
  execSync(teamBuild
    ? 'node scripts/assemble.mjs --team-xai-key'
    : 'node scripts/assemble.mjs', {
    cwd: desktop,
    env: teamBuild ? process.env : sanitizedEnvironment,
    stdio: 'inherit',
  });
  execSync('electron-builder --win nsis', {
    cwd: desktop,
    env: sanitizedEnvironment,
    stdio: 'inherit',
  });
  execSync('pnpm run test:mcp:packaged', {
    cwd: desktop,
    env: sanitizedEnvironment,
    stdio: 'inherit',
  });
  await requireNonEmptyFile(installerPaths[0], 'installer');
  await requireNonEmptyFile(installerPaths[1], 'installer blockmap');
  const packagedKeyBytes = (await waitForFile(unpackedKeyPath, 'packaged xAI key resource')).size;
  if (teamBuild ? packagedKeyBytes === 0 : packagedKeyBytes !== 0) {
    throw new Error(teamBuild
      ? '[dist] internal installer is missing its xAI team fallback'
      : '[dist] standard installer contains a non-empty xAI team credential');
  }
} catch (err) {
  failed = true;
  console.error(err instanceof Error && err.message.startsWith('[dist]')
    ? err.message
    : '[dist] build or verification failed');
} finally {
  // electron-builder has already copied the resource. Keep raw key material
  // out of build-resources after both successful and failed dist runs.
  stageTeamXAIKey(teamDirectory, '');
  if (failed) {
    for (const filePath of [...installerPaths, unpackedKeyPath]) rmSync(filePath, { force: true });
  }
}

if (failed) process.exit(1);

async function requireNonEmptyFile(filePath, label) {
  const info = await waitForFile(filePath, label);
  if (info.size === 0) throw new Error(`[dist] ${label} was not produced`);
}

async function waitForFile(filePath, label) {
  const deadline = Date.now() + 15_000;
  while (true) {
    try {
      const info = statSync(filePath);
      if (info.isFile()) return info;
    } catch {
      // Windows security scanning can briefly hide a newly signed NSIS output.
    }
    if (Date.now() >= deadline) throw new Error(`[dist] ${label} was not produced`);
    await delay(200);
  }
}
