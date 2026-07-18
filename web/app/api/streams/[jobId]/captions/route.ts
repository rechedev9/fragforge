import { NextResponse } from 'next/server';
import {
  callOrchestrator,
  forwardError,
  mutationHeaders,
  serviceUnavailable,
  streamJobUrl,
} from '../../_lib';

export const runtime = 'nodejs';

/** GET /api/streams/{jobId}/captions — poll durable caption candidates. */
export async function GET(_request: Request, { params }: { params: Promise<{ jobId: string }> }): Promise<Response> {
  const { jobId } = await params;
  const url = streamJobUrl(jobId, '/captions');
  if (!url) return NextResponse.json({ error: 'invalid job id' }, { status: 400 });

  const res = await callOrchestrator(url);
  if (res === null) return serviceUnavailable();
  if (!res.ok) return forwardError(res);
  return NextResponse.json((await res.json()) as unknown, { status: res.status });
}

/** POST /api/streams/{jobId}/captions — start asynchronous candidate generation. */
export async function POST(_request: Request, { params }: { params: Promise<{ jobId: string }> }): Promise<Response> {
  const { jobId } = await params;
  const url = streamJobUrl(jobId, '/captions');
  if (!url) return NextResponse.json({ error: 'invalid job id' }, { status: 400 });

  const res = await callOrchestrator(url, { method: 'POST', headers: mutationHeaders() });
  if (res === null) return serviceUnavailable();
  if (!res.ok) return forwardError(res);
  return NextResponse.json((await res.json()) as unknown, { status: res.status });
}
