import { NextResponse } from 'next/server';
import { readBoundedText } from '@/lib/api/bounded-request-body';
import { jobUrl, mutationHeaders, forwardError, callOrchestrator, serviceUnavailable } from '../../_lib';

export const runtime = 'nodejs';

/**
 * POST /api/demos/{jobId}/parse — start a parse for the chosen player. Maps the
 * client's { steamId } to the orchestrator's { target_steamid } and forwards the
 * mutation token.
 */
export async function POST(request: Request, { params }: { params: Promise<{ jobId: string }> }): Promise<Response> {
  const { jobId } = await params;
  const url = jobUrl(jobId, '/parse');
  if (!url) return NextResponse.json({ error: 'invalid job id' }, { status: 400 });

  const incoming = await readBoundedText(request);
  if (!incoming.ok) return NextResponse.json({ error: incoming.error }, { status: incoming.status });
  let requestBody: { steamId?: unknown };
  try {
    requestBody = JSON.parse(incoming.text || '{}') as { steamId?: unknown };
  } catch {
    return NextResponse.json({ error: 'invalid json body' }, { status: 400 });
  }
  const { steamId } = requestBody;
  if (typeof steamId !== 'string' || !steamId) {
    return NextResponse.json({ error: 'steamId required' }, { status: 400 });
  }

  const res = await callOrchestrator(url, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...mutationHeaders() },
    body: JSON.stringify({ target_steamid: steamId }),
  });
  if (res === null) return serviceUnavailable();
  if (!res.ok) return forwardError(res);

  const body = (await res.json()) as { id: string; status: string };
  return NextResponse.json(body);
}
