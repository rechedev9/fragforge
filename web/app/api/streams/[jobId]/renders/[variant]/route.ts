import { NextResponse } from 'next/server';
import { streamJobUrl, mutationHeaders, forwardError, callOrchestrator, serviceUnavailable } from '../../../_lib';

export const runtime = 'nodejs';

const VARIANT_RE = /^[A-Za-z0-9][A-Za-z0-9_-]*$/;

/** POST /api/streams/{jobId}/renders/{variant} — start a Stream Clips render. */
export async function POST(request: Request, { params }: { params: Promise<{ jobId: string; variant: string }> }): Promise<Response> {
  const { jobId, variant } = await params;
  if (!VARIANT_RE.test(variant)) return NextResponse.json({ error: 'invalid variant' }, { status: 400 });
  const url = streamJobUrl(jobId, `/renders/${variant}`);
  if (!url) return NextResponse.json({ error: 'invalid job id' }, { status: 400 });

  const bodyText = await request.text();
  const init: RequestInit = { method: 'POST', headers: { ...mutationHeaders() } };
  if (bodyText) {
    init.headers = { ...init.headers, 'Content-Type': 'application/json' };
    init.body = bodyText;
  }
  const res = await callOrchestrator(url, init);
  if (res === null) return serviceUnavailable();
  if (!res.ok) return forwardError(res);

  return NextResponse.json((await res.json()) as unknown);
}

/** GET /api/streams/{jobId}/renders/{variant} — render-variant state (status, videos). */
export async function GET(_request: Request, { params }: { params: Promise<{ jobId: string; variant: string }> }): Promise<Response> {
  const { jobId, variant } = await params;
  if (!VARIANT_RE.test(variant)) return NextResponse.json({ error: 'invalid variant' }, { status: 400 });
  const url = streamJobUrl(jobId, `/renders/${variant}`);
  if (!url) return NextResponse.json({ error: 'invalid job id' }, { status: 400 });

  const res = await callOrchestrator(url);
  if (res === null) return serviceUnavailable();
  if (!res.ok) return forwardError(res);

  return NextResponse.json((await res.json()) as unknown);
}
