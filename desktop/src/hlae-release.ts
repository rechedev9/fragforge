export interface HLAEReleaseSpec {
  version: string;
  url: string;
  sha256: string;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null;
}

const VERSION_PATTERN = /^\d+\.\d+\.\d+$/;
const SHA256_PATTERN = /^[a-f0-9]{64}$/;

/**
 * Validates GitHub's latest-release response for advancedfx and extracts the
 * Windows portable HLAE archive. The URL is constrained to the official repo
 * and the asset digest is required, so the provisioner never trusts arbitrary
 * JSON fields or downloads an unverified executable.
 */
export function parseLatestHLAERelease(value: unknown): HLAEReleaseSpec | null {
  if (!isRecord(value) || typeof value.tag_name !== 'string' || !Array.isArray(value.assets)) return null;
  if (!value.tag_name.startsWith('v')) return null;

  const version = value.tag_name.slice(1);
  if (!VERSION_PATTERN.test(version)) return null;

  const assetName = `hlae_${version.replaceAll('.', '_')}.zip`;
  const expectedURL = `https://github.com/advancedfx/advancedfx/releases/download/${value.tag_name}/${assetName}`;
  for (const asset of value.assets) {
    if (!isRecord(asset) || asset.name !== assetName) continue;
    if (asset.browser_download_url !== expectedURL || typeof asset.digest !== 'string') return null;
    const [algorithm, digest, extra] = asset.digest.split(':');
    if (algorithm !== 'sha256' || extra !== undefined || !SHA256_PATTERN.test(digest)) return null;
    return { version, url: expectedURL, sha256: digest };
  }
  return null;
}

export function compareHLAEVersions(left: string, right: string): number {
  const leftParts = left.split('.').map(Number);
  const rightParts = right.split('.').map(Number);
  for (let index = 0; index < 3; index += 1) {
    const difference = leftParts[index] - rightParts[index];
    if (difference !== 0) return difference;
  }
  return 0;
}
