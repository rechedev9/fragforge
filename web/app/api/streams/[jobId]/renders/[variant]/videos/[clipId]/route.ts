import { NextResponse } from 'next/server';
import { streamJobUrl, proxyStream } from '../../../../../_lib';

export const runtime = 'nodejs';

const VARIANT_RE = /^[A-Za-z0-9][A-Za-z0-9_-]*$/;
const CLIP_ID_RE = /^[A-Za-z0-9._-]+$/;

/** GET /api/streams/{jobId}/renders/{variant}/videos/{clipId} — stream a rendered Short mp4. */
export async function GET(
  request: Request,
  { params }: { params: Promise<{ jobId: string; variant: string; clipId: string }> },
): Promise<Response> {
  const { jobId, variant, clipId } = await params;
  if (!VARIANT_RE.test(variant) || !CLIP_ID_RE.test(clipId)) {
    return NextResponse.json({ error: 'invalid path' }, { status: 400 });
  }
  const url = streamJobUrl(jobId, `/renders/${variant}/videos/${clipId}`);
  if (!url) return NextResponse.json({ error: 'invalid job id' }, { status: 400 });
  return proxyStream(url, 'video/mp4', request);
}
