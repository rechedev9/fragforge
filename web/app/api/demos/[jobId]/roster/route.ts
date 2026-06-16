import { NextResponse } from 'next/server';
import { jobUrl, forwardError } from '../../_lib';

export const runtime = 'nodejs';

/**
 * GET /api/demos/{jobId}/roster — proxy the roster scan result. The orchestrator
 * already wraps it as { players: [...] } with steamid64 keys; the client maps
 * steamid64 → steamId, so this is a pass-through.
 */
export async function GET(_request: Request, { params }: { params: Promise<{ jobId: string }> }): Promise<Response> {
  const { jobId } = await params;
  const url = jobUrl(jobId, '/roster');
  if (!url) return NextResponse.json({ error: 'invalid job id' }, { status: 400 });

  const res = await fetch(url);
  if (!res.ok) return forwardError(res);

  const body = (await res.json()) as { players: unknown[] };
  return NextResponse.json({ players: body.players });
}
