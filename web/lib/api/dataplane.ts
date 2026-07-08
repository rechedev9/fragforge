/**
 * Client-side data-plane addressing. FragForge processes every media byte on the
 * user's own PC; the browser reaches that pipeline in one of two transports:
 *
 * - local mode: same-origin Next route handlers under /api/demos/* proxy a local
 *   orchestrator on this machine. No token travels to the browser.
 * - cloud mode: the hosted web is control plane only. After pairing, the browser
 *   talks straight to the paired agent's loopback proxy (http://127.0.0.1:<port>)
 *   with a Bearer token, over the orchestrator's native /api/jobs API.
 *
 * Both transports front the same orchestrator, so the two differ only in URL
 * shape, auth header, and a couple of payload field names (the local proxy used
 * to rename these server-side; in cloud mode the client speaks the orchestrator
 * protocol directly). These pure builders are the single source of truth for
 * that mapping, shared by RealApiClient and its tests.
 */

/** A resolved agent's loopback endpoint, handed to the browser by /api/pc/status. */
export type Loopback = { port: number; token: string };

/** The /api/pc/status body: pairing + liveness plus the loopback endpoint when paired. */
export type PcStatus = { paired: boolean; online: boolean; loopback: Loopback | null };

/**
 * The addressing surface for one transport. `headers` is empty in local mode and
 * carries the Bearer token in cloud mode. URL builders return same-origin proxy
 * paths locally and absolute loopback URLs in the cloud. `healthzUrl` is null in
 * local mode (there is no separate loopback liveness probe).
 */
export type DataPlane = {
  headers: Record<string, string>;
  scanUrl: string;
  /** Multipart field name for the .dem upload. */
  scanField: 'file' | 'demo';
  /** Reads the job id out of a scan response (`jobId` local vs `id` cloud). */
  scanJobId(body: unknown): string;
  jobStatusUrl(jobId: string): string;
  rosterUrl(jobId: string): string;
  parseUrl(jobId: string): string;
  /** Parse request body (`steamId` local vs `target_steamid` cloud). */
  parseBody(steamId: string): Record<string, string>;
  planUrl(jobId: string): string;
  recordUrl(jobId: string): string;
  renderUrl(jobId: string, variant: string): string;
  videoUrl(jobId: string, variant: string, name: string): string;
  coverUrl(jobId: string, variant: string, name: string): string;
  capabilitiesUrl: string;
  healthzUrl: string | null;
};

/** Loopback origin for a resolved agent, e.g. `http://127.0.0.1:8090`. */
export function loopbackOrigin(lb: Loopback): string {
  return `http://127.0.0.1:${lb.port}`;
}

function str(body: unknown, key: string): string {
  const v = (body as Record<string, unknown> | null)?.[key];
  return typeof v === 'string' ? v : '';
}

/**
 * Builds the data plane for a transport: pass the resolved loopback for cloud
 * mode, or null for the local same-origin proxy. The local paths match the
 * pre-cloud /api/demos/* proxy routes exactly, so local-mode behavior is
 * unchanged.
 */
export function dataPlane(lb: Loopback | null): DataPlane {
  if (!lb) {
    return {
      headers: {},
      scanUrl: '/api/demos/scan',
      scanField: 'file',
      scanJobId: (body) => str(body, 'jobId'),
      jobStatusUrl: (jobId) => `/api/demos/${jobId}/status`,
      rosterUrl: (jobId) => `/api/demos/${jobId}/roster`,
      parseUrl: (jobId) => `/api/demos/${jobId}/parse`,
      parseBody: (steamId) => ({ steamId }),
      planUrl: (jobId) => `/api/demos/${jobId}/plan`,
      recordUrl: (jobId) => `/api/demos/${jobId}/record`,
      renderUrl: (jobId, variant) => `/api/demos/${jobId}/renders/${variant}`,
      videoUrl: (jobId, variant, name) => `/api/demos/${jobId}/renders/${variant}/videos/${name}`,
      coverUrl: (jobId, variant, name) => `/api/demos/${jobId}/renders/${variant}/covers/${name}`,
      capabilitiesUrl: '/api/capabilities',
      healthzUrl: null,
    };
  }

  const origin = loopbackOrigin(lb);
  const job = (jobId: string) => `${origin}/api/jobs/${jobId}`;
  return {
    headers: { Authorization: `Bearer ${lb.token}` },
    scanUrl: `${origin}/api/jobs`,
    scanField: 'demo',
    scanJobId: (body) => str(body, 'id'),
    jobStatusUrl: (jobId) => job(jobId),
    rosterUrl: (jobId) => `${job(jobId)}/roster`,
    parseUrl: (jobId) => `${job(jobId)}/parse`,
    parseBody: (steamId) => ({ target_steamid: steamId }),
    planUrl: (jobId) => `${job(jobId)}/plan`,
    recordUrl: (jobId) => `${job(jobId)}/record`,
    renderUrl: (jobId, variant) => `${job(jobId)}/renders/${variant}`,
    videoUrl: (jobId, variant, name) => `${job(jobId)}/renders/${variant}/videos/${name}`,
    coverUrl: (jobId, variant, name) => `${job(jobId)}/renders/${variant}/covers/${name}`,
    capabilitiesUrl: `${origin}/api/capabilities`,
    healthzUrl: `${origin}/healthz`,
  };
}

/**
 * Words the offline reason from control-plane liveness. When the loopback probe
 * fails, a recent heartbeat means the PC is up but the agent/loopback is not
 * serving; no recent heartbeat means the PC itself is off. The UI surfaces a
 * single actionable message, but this keeps the distinction explicit.
 */
export function offlineReason(status: PcStatus): 'pc-off' | 'agent-not-running' {
  return status.online ? 'agent-not-running' : 'pc-off';
}
