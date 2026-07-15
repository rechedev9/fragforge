import { createHash } from 'node:crypto';
import {
  existsSync,
  mkdirSync,
  readFileSync,
  renameSync,
  rmSync,
  writeFileSync,
} from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const defaultDesktopDirectory = join(here, '..');

export function readPinnedHLAETool(desktopDirectory = defaultDesktopDirectory) {
  const manifestPath = join(desktopDirectory, 'src', 'hlae-tool.json');
  const manifest = JSON.parse(readFileSync(manifestPath, 'utf8'));
  for (const field of ['version', 'archiveName', 'url', 'sha256', 'exeRel']) {
    if (typeof manifest[field] !== 'string' || manifest[field] === '') {
      throw new Error(`[hlae-bundle] invalid ${field} in ${manifestPath}`);
    }
  }
  if (!/^[a-f0-9]{64}$/.test(manifest.sha256)) {
    throw new Error(`[hlae-bundle] invalid sha256 in ${manifestPath}`);
  }
  if (!/^hlae_[a-zA-Z0-9_]+\.zip$/.test(manifest.archiveName)) {
    throw new Error(`[hlae-bundle] unsafe archiveName in ${manifestPath}`);
  }
  return manifest;
}

export async function stageBundledHLAE({
  desktopDirectory = defaultDesktopDirectory,
  destinationDirectory,
  fetchImpl = fetch,
  spec = readPinnedHLAETool(desktopDirectory),
}) {
  if (!destinationDirectory) throw new Error('[hlae-bundle] destinationDirectory is required');
  mkdirSync(destinationDirectory, { recursive: true });
  const destination = join(destinationDirectory, spec.archiveName);
  const temporary = `${destination}.tmp`;
  rmSync(temporary, { force: true });

  try {
    const response = await fetchImpl(spec.url, {
      headers: { 'User-Agent': 'FragForge-Studio-build' },
      redirect: 'follow',
    });
    if (!response.ok) {
      throw new Error(`[hlae-bundle] download failed with HTTP ${response.status}`);
    }
    writeFileSync(temporary, Buffer.from(await response.arrayBuffer()));
    verifyBundledHLAE(temporary, spec);
    rmSync(destination, { force: true });
    renameSync(temporary, destination);
    return destination;
  } finally {
    rmSync(temporary, { force: true });
  }
}

export function verifyBundledHLAE(archivePath, spec = readPinnedHLAETool()) {
  if (!existsSync(archivePath)) {
    throw new Error(`[hlae-bundle] missing bundled archive ${archivePath}`);
  }
  const digest = createHash('sha256').update(readFileSync(archivePath)).digest('hex');
  if (digest !== spec.sha256) {
    throw new Error(`[hlae-bundle] sha256 mismatch: got ${digest}, want ${spec.sha256}`);
  }
  return archivePath;
}
