import * as fs from 'node:fs';
import * as path from 'node:path';
import { createHash, timingSafeEqual } from 'node:crypto';
import { downloadFile } from './http-download.ts';

const SHA256_RE = /^[a-f0-9]{64}$/i;

export interface MusicLibraryOptions {
  bundledMusicDir: string;
  musicDir: string;
  signal: AbortSignal;
  logLine: (line: string) => void;
  download?: typeof downloadFile;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null;
}

/**
 * Copies the bundled catalog and fills the user music library one track at a
 * time. Provisioning is deliberately best-effort: an unavailable track never
 * prevents Studio from booting, and the orchestrator discovers files as they
 * arrive.
 */
export async function provisionMusicLibrary({
  bundledMusicDir,
  musicDir,
  signal,
  logLine,
  download = downloadFile,
}: MusicLibraryOptions): Promise<void> {
  const bundledCatalog = path.join(bundledMusicDir, 'catalog.json');
  if (!fs.existsSync(bundledCatalog)) return;

  fs.mkdirSync(musicDir, { recursive: true });
  fs.copyFileSync(bundledCatalog, path.join(musicDir, 'catalog.json'));

  let tracks: unknown[];
  try {
    const parsed: unknown = JSON.parse(fs.readFileSync(bundledCatalog, 'utf8'));
    tracks = isRecord(parsed) && Array.isArray(parsed.tracks) ? parsed.tracks : [];
  } catch (err) {
    logLine(`[music] bad catalog.json: ${String(err)}\n`);
    return;
  }

  // Preserve the original sequential behavior: tracks become visible as each
  // one lands without hammering several release hosts during application boot.
  for (const track of tracks) {
    if (signal.aborted) return;
    if (!isRecord(track)) continue;

    const { id, ext, downloadUrl, sha256 } = track;
    if (typeof id !== 'string' || !id || typeof ext !== 'string' || !ext) continue;

    const destination = path.join(musicDir, `${id}.${ext}`);
    if (typeof downloadUrl !== 'string' || !downloadUrl) {
      if (fs.existsSync(destination)) continue;
      const bundledAudio = path.join(bundledMusicDir, `${id}.${ext}`);
      if (fs.existsSync(bundledAudio)) {
        fs.copyFileSync(bundledAudio, destination);
        logLine(`[music] copied bundled ${id}.${ext}\n`);
      } else {
        logLine(`[music] skip ${id}: no downloadUrl and no bundled audio\n`);
      }
      continue;
    }

    if (typeof sha256 !== 'string' || !SHA256_RE.test(sha256)) {
      fs.rmSync(destination, { force: true });
      logLine(`[music] skip ${id}: remote track has no valid sha256\n`);
      continue;
    }

    if (fs.existsSync(destination)) {
      try {
        const cachedDigest = await fileSHA256(destination, signal);
        if (sha256Matches(cachedDigest, sha256)) continue;
        fs.rmSync(destination, { force: true });
        logLine(`[music] removed ${id}.${ext}: sha256 mismatch\n`);
      } catch (err) {
        if (signal.aborted) return;
        fs.rmSync(destination, { force: true });
        logLine(`[music] removed ${id}.${ext}: could not verify sha256: ${String(err)}\n`);
      }
    }

    try {
      const downloadedDigest = await download(downloadUrl, destination, { signal });
      if (!sha256Matches(downloadedDigest, sha256)) {
        // downloadFile publishes the destination before returning its digest.
        // A mismatched asset must not survive that rename boundary.
        fs.rmSync(destination, { force: true });
        throw new Error('sha256 mismatch');
      }
      logLine(`[music] downloaded ${id}.${ext}\n`);
    } catch (err) {
      if (signal.aborted) return;
      logLine(`[music] skip ${id}: ${String(err)}\n`);
    }
  }
}

async function fileSHA256(filePath: string, signal: AbortSignal): Promise<string> {
  const hash = createHash('sha256');
  for await (const chunk of fs.createReadStream(filePath, { signal })) hash.update(chunk);
  return hash.digest('hex');
}

function sha256Matches(got: string, want: string): boolean {
  if (!SHA256_RE.test(got) || !SHA256_RE.test(want)) return false;
  return timingSafeEqual(Buffer.from(got, 'hex'), Buffer.from(want, 'hex'));
}
