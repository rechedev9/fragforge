import { NextResponse } from 'next/server';
import { readBoundedText } from '@/lib/api/bounded-request-body';
import {
  callOrchestrator,
  forwardError,
  mutationHeaders,
  serviceUnavailable,
  streamJobUrl,
} from '../../../_lib';

export const runtime = 'nodejs';

/** POST /api/streams/{jobId}/killfeed/apply — apply one current generation. */
export async function POST(request: Request, { params }: { params: Promise<{ jobId: string }> }): Promise<Response> {
  const { jobId } = await params;
  const url = streamJobUrl(jobId, '/killfeed/apply');
  if (!url) return NextResponse.json({ error: 'invalid job id' }, { status: 400 });

  const incoming = await readBoundedText(request);
  if (!incoming.ok) return NextResponse.json({ error: incoming.error }, { status: incoming.status });
  const res = await callOrchestrator(url, {
    method: 'POST',
    headers: { ...mutationHeaders(), 'Content-Type': 'application/json' },
    body: incoming.text,
  });
  if (res === null) return serviceUnavailable();
  if (!res.ok) return forwardError(res);
  return NextResponse.json((await res.json()) as unknown, { status: res.status });
}
