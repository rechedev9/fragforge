import * as fs from 'node:fs';
import * as path from 'node:path';
import { downloadFile } from './http-download.ts';

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

    const { id, ext, downloadUrl } = track;
    if (typeof id !== 'string' || !id || typeof ext !== 'string' || !ext) continue;

    const destination = path.join(musicDir, `${id}.${ext}`);
    if (fs.existsSync(destination)) continue;

    if (typeof downloadUrl !== 'string' || !downloadUrl) {
      const bundledAudio = path.join(bundledMusicDir, `${id}.${ext}`);
      if (fs.existsSync(bundledAudio)) {
        fs.copyFileSync(bundledAudio, destination);
        logLine(`[music] copied bundled ${id}.${ext}\n`);
      } else {
        logLine(`[music] skip ${id}: no downloadUrl and no bundled audio\n`);
      }
      continue;
    }

    try {
      await download(downloadUrl, destination, { signal });
      logLine(`[music] downloaded ${id}.${ext}\n`);
    } catch (err) {
      if (signal.aborted) return;
      logLine(`[music] skip ${id}: ${String(err)}\n`);
    }
  }
}
