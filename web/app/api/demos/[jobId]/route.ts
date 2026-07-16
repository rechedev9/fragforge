import { localDeleteJob } from '../_local';

export const runtime = 'nodejs';

/**
 * DELETE /api/demos/{jobId} — delete a demo job (match) and its server-side
 * artifacts through the same-origin boundary. 204 on success; 409 while the job
 * is still processing; 404 for an unknown id; 503 when the orchestrator is down.
 */
export async function DELETE(_request: Request, { params }: { params: Promise<{ jobId: string }> }): Promise<Response> {
  const { jobId } = await params;
  return localDeleteJob(jobId);
}
