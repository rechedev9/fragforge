import { NextResponse } from 'next/server';
import { jobUrl, forwardError } from '../../_lib';

export const runtime = 'nodejs';

/** GET /api/demos/{jobId}/status — proxy the job's current status. */
export async function GET(_request: Request, { params }: { params: Promise<{ jobId: string }> }): Promise<Response> {
  const { jobId } = await params;
  const url = jobUrl(jobId);
  if (!url) return NextResponse.json({ error: 'invalid job id' }, { status: 400 });

  const res = await fetch(url);
  if (!res.ok) return forwardError(res);

  const { status } = (await res.json()) as { status: string };
  return NextResponse.json({ status });
}
