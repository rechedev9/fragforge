/**
 * Client-side data-plane addressing. FragForge processes every media byte on
 * the user's own PC: the browser reaches the bundled orchestrator through
 * same-origin Next route handlers under /api/demos/*, which proxy to the
 * orchestrator running on this machine. No token travels to the browser.
 * This pure builder is the single source of truth for that URL/field-name
 * mapping, shared by RealApiClient and its tests.
 */

/**
 * The addressing surface for the local same-origin proxy transport. `headers`
 * is always empty (no auth token crosses the proxy boundary).
 */
export type DataPlane = {
  headers: Record<string, string>;
  scanUrl: string;
  /** Multipart field name for the .dem upload. */
  scanField: 'demo';
  /** Multipart field name for the optional bulk-series id carried on a scan. */
  scanSeriesField: 'series_id';
  /** Reads the job id out of a scan response. */
  scanJobId(body: unknown): string;
  jobStatusUrl(jobId: string): string;
  rosterUrl(jobId: string): string;
  seriesUrl(seriesId: string): string;
  parseUrl(jobId: string): string;
  /** Parse request body. */
  parseBody(steamId: string): Record<string, string>;
  planUrl(jobId: string): string;
  recordUrl(jobId: string): string;
  renderUrl(jobId: string, variant: string): string;
  videoUrl(jobId: string, variant: string, name: string): string;
  publishAssistantUrl(jobId: string, variant: string, name: string, days?: number): string;
  coverUrl(jobId: string, variant: string, name: string): string;
  capabilitiesUrl: string;
};

function str(body: unknown, key: string): string {
  const v = (body as Record<string, unknown> | null)?.[key];
  return typeof v === 'string' ? v : '';
}

/**
 * Builds the local same-origin proxy data plane. The paths match the
 * /api/demos/* proxy routes exactly.
 */
export function dataPlane(): DataPlane {
  return {
    headers: {},
    scanUrl: '/api/demos/scan',
    scanField: 'demo',
    scanSeriesField: 'series_id',
    scanJobId: (body) => str(body, 'jobId'),
    jobStatusUrl: (jobId) => `/api/demos/${jobId}/status`,
    rosterUrl: (jobId) => `/api/demos/${jobId}/roster`,
    seriesUrl: (seriesId) => `/api/demos/series/${seriesId}`,
    parseUrl: (jobId) => `/api/demos/${jobId}/parse`,
    parseBody: (steamId) => ({ steamId }),
    planUrl: (jobId) => `/api/demos/${jobId}/plan`,
    recordUrl: (jobId) => `/api/demos/${jobId}/record`,
    renderUrl: (jobId, variant) => `/api/demos/${jobId}/renders/${variant}`,
    videoUrl: (jobId, variant, name) => `/api/demos/${jobId}/renders/${variant}/videos/${name}`,
    publishAssistantUrl: (jobId, variant, name, days = 7) =>
      `/api/demos/${jobId}/renders/${variant}/videos/${name}/publish-assistant?days=${days}`,
    coverUrl: (jobId, variant, name) => `/api/demos/${jobId}/renders/${variant}/covers/${name}`,
    capabilitiesUrl: '/api/capabilities',
  };
}
