/** Formats the build-provided desktop version for the visible Studio chrome. */
export function formatAppVersion(version: string | undefined): string | null {
  const normalized = version?.trim();
  if (!normalized) return null;
  return `v${normalized.replace(/^v/i, '')}`;
}
