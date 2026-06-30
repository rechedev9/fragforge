import { NextResponse } from 'next/server';
import { jobUrl, forwardError, callOrchestrator, serviceUnavailable } from '../../_lib';

export const runtime = 'nodejs';

/** GET /api/demos/{jobId}/status — proxy the job's current status. */
export async function GET(_request: Request, { params }: { params: Promise<{ jobId: string }> }): Promise<Response> {
  const { jobId } = await params;
  const url = jobUrl(jobId);
  if (!url) return NextResponse.json({ error: 'invalid job id' }, { status: 400 });

  const res = await callOrchestrator(url);
  if (res === null) return serviceUnavailable();
  if (!res.ok) return forwardError(res);

  // Forward only the two known fields, never the raw upstream object (see _lib).
  // failure_reason is omitted by the orchestrator unless the job actually failed.
  const data = (await res.json()) as { status: string; failure_reason?: string };
  const body: { status: string; failure_reason?: string } = { status: data.status };
  if (data.failure_reason) body.failure_reason = data.failure_reason;
  return NextResponse.json(body);
}
