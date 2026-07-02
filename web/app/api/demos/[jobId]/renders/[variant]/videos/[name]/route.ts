import { NextResponse } from 'next/server';
import { callOrchestrator, forwardError, jobUrl, proxyStream, serviceUnavailable } from '../../../../../_lib';

export const runtime = 'nodejs';

const VARIANT_RE = /^[A-Za-z0-9][A-Za-z0-9_-]*$/;
const NAME_RE = /^[A-Za-z0-9._-]+$/;

function videoUrl(jobId: string, variant: string, name: string): string | null {
  if (!VARIANT_RE.test(variant) || !NAME_RE.test(name)) return null;
  return jobUrl(jobId, `/renders/${variant}/videos/${name}`);
}

/** GET /api/demos/{jobId}/renders/{variant}/videos/{name} — stream a reel mp4. */
export async function GET(
  request: Request,
  { params }: { params: Promise<{ jobId: string; variant: string; name: string }> },
): Promise<Response> {
  const { jobId, variant, name } = await params;
  const url = videoUrl(jobId, variant, name);
  if (!url) return NextResponse.json({ error: 'invalid path' }, { status: 400 });
  return proxyStream(url, 'video/mp4', request);
}

/** DELETE /api/demos/{jobId}/renders/{variant}/videos/{name} — remove a reel's artifacts. */
export async function DELETE(
  _request: Request,
  { params }: { params: Promise<{ jobId: string; variant: string; name: string }> },
): Promise<Response> {
  const { jobId, variant, name } = await params;
  const url = videoUrl(jobId, variant, name);
  if (!url) return NextResponse.json({ error: 'invalid path' }, { status: 400 });
  const res = await callOrchestrator(url, { method: 'DELETE' });
  if (res === null) return serviceUnavailable();
  if (!res.ok) return forwardError(res);
  return new Response(null, { status: 204 });
}
