import { NextResponse } from 'next/server';
import { jobUrl, proxyStream } from '../../../../../_lib';

export const runtime = 'nodejs';

const VARIANT_RE = /^[A-Za-z0-9][A-Za-z0-9_-]*$/;
const NAME_RE = /^[A-Za-z0-9._-]+$/;

/** GET /api/demos/{jobId}/renders/{variant}/videos/{name} — stream a reel mp4. */
export async function GET(
  request: Request,
  { params }: { params: Promise<{ jobId: string; variant: string; name: string }> },
): Promise<Response> {
  const { jobId, variant, name } = await params;
  if (!VARIANT_RE.test(variant) || !NAME_RE.test(name)) {
    return NextResponse.json({ error: 'invalid path' }, { status: 400 });
  }
  const url = jobUrl(jobId, `/renders/${variant}/videos/${name}`);
  if (!url) return NextResponse.json({ error: 'invalid job id' }, { status: 400 });
  return proxyStream(url, 'video/mp4', request);
}
