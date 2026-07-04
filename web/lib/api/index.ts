import { MockApiClient } from './mock';
import { RealApiClient } from './real';
import type { ApiClient } from './client';
import { isLocalMode, isHostedMode } from '@/lib/mode';
import { agentBaseUrl, agentHeaders } from '@/lib/agent/connection';

/**
 * The RealApiClient (same ApiClient interface) drives the real upload->parse->
 * record->render path.
 *
 * - hosted: the browser talks DIRECTLY to the local agent. The client is native
 *   (proxy paths remapped to the agent's `/api/jobs/*` routes), prefixed with the
 *   agent base URL, and sends X-FragForge-Token on every request. baseUrl/headers
 *   read localStorage; agentHeaders is passed as a THUNK so the token is re-read
 *   per request (a pasted token takes effect without a reload), and agentBaseUrl()
 *   is SSR-guarded so importing this on the server is inert (returns the default).
 * - local / NEXT_PUBLIC_API_BASE (cloud): the existing same-origin client whose
 *   `/api/demos/*` routes proxy the orchestrator.
 * - otherwise: the in-memory mock (the design/preview default).
 */
function selectApi(): ApiClient {
  if (isHostedMode()) {
    return new RealApiClient({ baseUrl: agentBaseUrl(), headers: agentHeaders, native: true });
  }
  if (process.env.NEXT_PUBLIC_API_BASE || isLocalMode()) return new RealApiClient();
  return new MockApiClient();
}

export const api: ApiClient = selectApi();
