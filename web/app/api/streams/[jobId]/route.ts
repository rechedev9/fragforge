import { NextResponse } from 'next/server';
import { streamJobUrl, callOrchestrator, forwardError, serviceUnavailable } from '../_lib';

export const runtime = 'nodejs';

/** GET /api/streams/{jobId} — proxy a single stream-clip job. */
export async function GET(_request: Request, { params }: { params: Promise<{ jobId: string }> }): Promise<Response> {
  const { jobId } = await params;
  const url = streamJobUrl(jobId);
  if (!url) return NextResponse.json({ error: 'invalid job id' }, { status: 400 });

  const res = await callOrchestrator(url);
  if (res === null) return serviceUnavailable();
  if (!res.ok) return forwardError(res);

  return NextResponse.json((await res.json()) as unknown);
}
