import { NextResponse } from 'next/server';
import { jobUrl, forwardError, callOrchestrator, serviceUnavailable } from '../../_lib';

export const runtime = 'nodejs';

/** GET /api/demos/{jobId}/plan — proxy the killplan JSON unchanged. */
export async function GET(_request: Request, { params }: { params: Promise<{ jobId: string }> }): Promise<Response> {
  const { jobId } = await params;
  const url = jobUrl(jobId, '/plan');
  if (!url) return NextResponse.json({ error: 'invalid job id' }, { status: 400 });

  const res = await callOrchestrator(url);
  if (res === null) return serviceUnavailable();
  if (!res.ok) return forwardError(res);

  const plan = (await res.json()) as unknown;
  return NextResponse.json(plan);
}
