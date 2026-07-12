import { execSync } from 'node:child_process';
import { readFileSync, rmSync } from 'node:fs';
import { dirname, join } from 'node:path';
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
  execSync('npm run build', { cwd: desktop, env: sanitizedEnvironment, stdio: 'inherit' });
  execSync(teamBuild
    ? 'node scripts/assemble.mjs --team-xai-key'
    : 'node scripts/assemble.mjs', {
    cwd: desktop,
    env: process.env,
    stdio: 'inherit',
  });
  execSync('electron-builder --win nsis', {
    cwd: desktop,
    env: sanitizedEnvironment,
    stdio: 'inherit',
  });
} catch {
  failed = true;
} finally {
  // electron-builder has already copied the resource. Keep raw key material
  // out of build-resources after both successful and failed dist runs.
  stageTeamXAIKey(teamDirectory, '');
  if (failed) {
    for (const filePath of [...installerPaths, unpackedKeyPath]) rmSync(filePath, { force: true });
  }
}

if (failed) process.exit(1);
