import { localScan } from '../_local';

export const runtime = 'nodejs';

/**
 * POST /api/demos/scan — accept a .dem upload and start a roster scan through
 * the desktop-bundled local orchestrator. The browser uses this same-origin
 * route, so the upstream address and optional token remain server-side while
 * the local server forwards the upload.
 */
export async function POST(request: Request): Promise<Response> {
  return localScan(request);
}
