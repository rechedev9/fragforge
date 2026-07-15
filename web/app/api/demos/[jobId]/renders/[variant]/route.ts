import { NextResponse } from 'next/server';
import { readBoundedText } from '@/lib/api/bounded-request-body';
import { jobUrl, mutationHeaders, forwardError, callOrchestrator, serviceUnavailable } from '../../../_lib';

export const runtime = 'nodejs';

const VARIANT_RE = /^[A-Za-z0-9][A-Za-z0-9_-]*$/;

/**
 * POST /api/demos/{jobId}/renders/{variant} — start a vertical-reel render.
 * Forwards an optional JSON body (e.g. { music: "<track-key>" }) to the
 * orchestrator so Music Edit can mix a track.
 */
export async function POST(request: Request, { params }: { params: Promise<{ jobId: string; variant: string }> }): Promise<Response> {
  const { jobId, variant } = await params;
  if (!VARIANT_RE.test(variant)) return NextResponse.json({ error: 'invalid variant' }, { status: 400 });
  const url = jobUrl(jobId, `/renders/${variant}`);
  if (!url) return NextResponse.json({ error: 'invalid job id' }, { status: 400 });

  const incoming = await readBoundedText(request);
  if (!incoming.ok) return NextResponse.json({ error: incoming.error }, { status: incoming.status });
  const init: RequestInit = { method: 'POST', headers: { ...mutationHeaders() } };
  if (incoming.text) {
    init.headers = { ...init.headers, 'Content-Type': 'application/json' };
    init.body = incoming.text;
  }
  const res = await callOrchestrator(url, init);
  if (res === null) return serviceUnavailable();
  if (!res.ok) return forwardError(res);

  return NextResponse.json((await res.json()) as unknown);
}

/** GET /api/demos/{jobId}/renders/{variant} — render-variant state (status, shorts). */
export async function GET(_request: Request, { params }: { params: Promise<{ jobId: string; variant: string }> }): Promise<Response> {
  const { jobId, variant } = await params;
  if (!VARIANT_RE.test(variant)) return NextResponse.json({ error: 'invalid variant' }, { status: 400 });
  const url = jobUrl(jobId, `/renders/${variant}`);
  if (!url) return NextResponse.json({ error: 'invalid job id' }, { status: 400 });

  const res = await callOrchestrator(url);
  if (res === null) return serviceUnavailable();
  if (!res.ok) return forwardError(res);

  return NextResponse.json((await res.json()) as unknown);
}
