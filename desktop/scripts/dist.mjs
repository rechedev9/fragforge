import { execSync } from 'node:child_process';
import { rmSync, statSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { setTimeout as delay } from 'node:timers/promises';
import { fileURLToPath } from 'node:url';
import { environmentWithoutXAIAPIKey } from './build-environment.mjs';
import { readPinnedHLAETool, verifyBundledHLAE } from './hlae-bundle.mjs';
import { releasePaths, verifyReleaseChecksums, writeReleaseChecksums } from './release-integrity.mjs';

const here = dirname(fileURLToPath(import.meta.url));
const desktop = join(here, '..');
const { artifacts: installerPaths, checksum: checksumPath } = releasePaths(desktop);

// Remove stale release-shaped outputs before building the same version again.
for (const filePath of [...installerPaths, checksumPath]) rmSync(filePath, { force: true });
// electron-builder can otherwise retain files removed from extraFiles between
// releases (notably the retired external assistant launcher).
rmSync(join(desktop, 'dist-installer', 'win-unpacked'), { recursive: true, force: true });

if (process.argv.length > 2) {
  console.error('[dist] unsupported build argument');
  process.exit(1);
}

const sanitizedEnvironment = environmentWithoutXAIAPIKey();
let failed = false;
try {
  execSync('pnpm run build', { cwd: desktop, env: sanitizedEnvironment, stdio: 'inherit' });
  execSync('node scripts/assemble.mjs', {
    cwd: desktop,
    env: sanitizedEnvironment,
    stdio: 'inherit',
  });
  execSync('electron-builder --win nsis', {
    cwd: desktop,
    env: sanitizedEnvironment,
    stdio: 'inherit',
  });
  const hlae = readPinnedHLAETool(desktop);
  verifyBundledHLAE(
    join(desktop, 'dist-installer', 'win-unpacked', 'resources', 'hlae', hlae.archiveName),
    hlae,
  );
  await requireNonEmptyFile(installerPaths[0], 'installer');
  await requireNonEmptyFile(installerPaths[1], 'installer blockmap');
  await writeReleaseChecksums(installerPaths, checksumPath);
  await verifyReleaseChecksums(installerPaths, checksumPath);
} catch (err) {
  failed = true;
  console.error(err instanceof Error && err.message.startsWith('[dist]')
    ? err.message
    : '[dist] build or verification failed');
} finally {
  if (failed) {
    for (const filePath of [...installerPaths, checksumPath]) rmSync(filePath, { force: true });
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
