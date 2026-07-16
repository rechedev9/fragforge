import { localJobs } from '../_local';

export const runtime = 'nodejs';

/**
 * GET /api/demos/jobs — list the most recent demo jobs from the local desktop
 * orchestrator through the same-origin server boundary, so the upstream address
 * and optional token stay server-side. Partidas uses it to rediscover uploads
 * and series after the app restarts.
 */
export async function GET(): Promise<Response> {
  return localJobs();
}
