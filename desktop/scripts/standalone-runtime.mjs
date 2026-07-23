import { existsSync, readdirSync, rmSync } from 'node:fs';
import { join } from 'node:path';

const WINDOWS_X64_SHARP_PACKAGES = new Set(['colour', 'sharp-win32-x64']);

/**
 * Next standalone tracing includes every optional Sharp platform package when
 * pnpm uses a hoisted linker. The desktop target is Windows x64, so retain only
 * the runtime packages that can load on that target.
 */
export function pruneSharpPlatforms(nodeModulesDirectory) {
  const imagePackages = join(nodeModulesDirectory, '@img');
  if (!existsSync(imagePackages)) return;

  for (const entry of readdirSync(imagePackages, { withFileTypes: true })) {
    if (!entry.isDirectory() || WINDOWS_X64_SHARP_PACKAGES.has(entry.name)) continue;
    rmSync(join(imagePackages, entry.name), { force: true, recursive: true });
  }
}
