import { NextResponse } from 'next/server';
import { jobUrl, mutationHeaders, forwardError, callOrchestrator, serviceUnavailable } from '../../_lib';

export const runtime = 'nodejs';

/**
 * POST /api/demos/{jobId}/generate — one-click capture + render. The
 * orchestrator persists the render intent (preset, music, edit) and enqueues
 * the recording; the record worker chains the render on success, so the
 * pipeline advances server-side even if the browser tab closes mid-capture.
 */
export async function POST(request: Request, { params }: { params: Promise<{ jobId: string }> }): Promise<Response> {
  const { jobId } = await params;
  const url = jobUrl(jobId, '/generate');
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
