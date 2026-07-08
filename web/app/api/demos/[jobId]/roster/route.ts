import { localRoster } from '../../_local';

export const runtime = 'nodejs';

/**
 * GET /api/demos/{jobId}/roster — proxy the roster scan result from the local
 * orchestrator. Local-mode route: cloud mode reads the loopback directly.
 */
export async function GET(_request: Request, { params }: { params: Promise<{ jobId: string }> }): Promise<Response> {
  const { jobId } = await params;
  return localRoster(jobId);
}
