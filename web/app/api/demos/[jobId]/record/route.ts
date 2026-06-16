import { NextResponse } from 'next/server';
import { jobUrl, mutationHeaders, forwardError } from '../../_lib';

export const runtime = 'nodejs';

/** POST /api/demos/{jobId}/record — start the HLAE/CS2 recording for a parsed job. */
export async function POST(_request: Request, { params }: { params: Promise<{ jobId: string }> }): Promise<Response> {
  const { jobId } = await params;
  const url = jobUrl(jobId, '/record');
  if (!url) return NextResponse.json({ error: 'invalid job id' }, { status: 400 });

  const res = await fetch(url, { method: 'POST', headers: { ...mutationHeaders() } });
  if (!res.ok) return forwardError(res);

  return NextResponse.json((await res.json()) as unknown);
}
