import {
  orchestratorUrl,
  callOrchestrator,
  mutationHeaders,
  forwardError,
  serviceUnavailable,
  proxyStream,
} from '../demos/_lib';

/**
 * Stream-clip proxy helpers. Reuses the demos proxy's orchestrator plumbing
 * (base URL, token headers, error forwarding, 503-on-unreachable, range-aware
 * binary streaming) so /api/streams/* follows exactly the same contract as
 * /api/demos/*: every route goes through callOrchestrator and a bare fetch
 * failure never turns into a code-less 500.
 */
export { orchestratorUrl, callOrchestrator, mutationHeaders, forwardError, serviceUnavailable, proxyStream };

const UUID_RE = /^[0-9a-f]{8}(-[0-9a-f]{4}){3}-[0-9a-f]{12}$/i;

/**
 * Builds the upstream stream-job URL for a validated UUID jobId, returning
 * null when the id is not a UUID. Defence in depth against path injection.
 */
export function streamJobUrl(jobId: string, suffix = ''): string | null {
  return UUID_RE.test(jobId) ? `${orchestratorUrl()}/api/stream-jobs/${jobId}${suffix}` : null;
}
