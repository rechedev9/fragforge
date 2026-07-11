import { localListJobs } from './_local';

export const runtime = 'nodejs';

/**
 * GET /api/demos - list the local orchestrator's jobs (forwards to its
 * GET /api/jobs). Local-mode route: the desktop UI polls this to enumerate
 * server-side jobs so externally-created work shows up on /matches and /videos.
 */
export function GET(request: Request): Promise<Response> {
  return localListJobs(request);
}
