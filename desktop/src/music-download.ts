const MUSIC_DOWNLOAD_USER_AGENT = 'FragForge Studio music provisioner';

/** Headers needed by music hosts that protect direct audio URLs from hotlinks. */
export function musicDownloadHeaders(sourceUrl: unknown): Record<string, string> | undefined {
  if (typeof sourceUrl !== 'string' || sourceUrl === '') return undefined;
  return {
    Referer: sourceUrl,
    'User-Agent': MUSIC_DOWNLOAD_USER_AGENT,
  };
}
