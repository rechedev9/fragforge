import { localScan } from '../_local';

export const runtime = 'nodejs';

/**
 * POST /api/demos/scan — accept a .dem upload and start a roster scan by
 * proxying the local orchestrator. This is a local-mode route: in cloud mode the
 * browser talks straight to the paired agent's loopback, never through here, so
 * the whole /api/demos/* surface is now a same-origin proxy to a local
 * orchestrator (no Supabase, no media bytes on the control plane).
 */
export async function POST(request: Request): Promise<Response> {
  return localScan(request);
}
