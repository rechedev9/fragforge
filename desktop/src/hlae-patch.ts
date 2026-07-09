import { createHash } from 'node:crypto';
import * as fs from 'node:fs';
import * as path from 'node:path';

export interface HLAEPatchSpec {
  version: string;
  sha256: string;
}

export type HLAEPatchStatus = 'not-applicable' | 'current' | 'installed';

export const FRAGFORGE_HLAE_PATCH: HLAEPatchSpec = {
  version: '2.190.2',
  sha256: 'cc688974a3aeda59371fe399db7b91d49b5296e2afe8b15d6f9b7ebba59dc22e',
};

function fileSHA256(file: string): string {
  return createHash('sha256').update(fs.readFileSync(file)).digest('hex');
}

/**
 * Overlays FragForge's verified Source 2 hook on the one HLAE release it was
 * built and capture-tested against. Newer HLAE releases are left untouched.
 */
export function installBundledHLAEPatch(
  hlaeExe: string,
  installedVersion: string,
  bundleDir: string,
  patch: HLAEPatchSpec = FRAGFORGE_HLAE_PATCH,
): HLAEPatchStatus {
  if (installedVersion !== patch.version) return 'not-applicable';

  const bundledHook = path.join(bundleDir, 'AfxHookSource2.dll');
  if (!fs.existsSync(bundledHook)) throw new Error(`bundled hook missing: ${bundledHook}`);
  const bundledDigest = fileSHA256(bundledHook);
  if (bundledDigest !== patch.sha256) {
    throw new Error(`bundled hook sha256 mismatch: got ${bundledDigest}, want ${patch.sha256}`);
  }

  const hook = path.join(path.dirname(hlaeExe), 'x64', 'AfxHookSource2.dll');
  if (!fs.existsSync(hook)) throw new Error(`official HLAE hook missing: ${hook}`);
  if (fileSHA256(hook) === patch.sha256) return 'current';

  const backup = path.join(path.dirname(hook), `AfxHookSource2.official-${patch.version}.dll`);
  if (!fs.existsSync(backup)) fs.copyFileSync(hook, backup);

  const staged = `${hook}.fragforge.tmp`;
  try {
    fs.copyFileSync(bundledHook, staged);
    if (fileSHA256(staged) !== patch.sha256) throw new Error('staged hook failed sha256 verification');
    fs.copyFileSync(staged, hook);
  } finally {
    fs.rmSync(staged, { force: true });
  }

  const installedDigest = fileSHA256(hook);
  if (installedDigest !== patch.sha256) {
    throw new Error(`installed hook sha256 mismatch: got ${installedDigest}, want ${patch.sha256}`);
  }
  return 'installed';
}
