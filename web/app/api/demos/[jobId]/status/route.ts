import { localStatus } from '../../_local';

export const runtime = 'nodejs';

/**
 * GET /api/demos/{jobId}/status — proxy the job's current status from the local
 * orchestrator. Local-mode route: cloud mode polls the loopback directly.
 */
export async function GET(_request: Request, { params }: { params: Promise<{ jobId: string }> }): Promise<Response> {
  const { jobId } = await params;
  return localStatus(jobId);
}
