import { NextResponse } from 'next/server';
import { streamJobUrl, mutationHeaders, callOrchestrator, serviceUnavailable } from '../../_lib';

export const runtime = 'nodejs';

/**
 * Forwards a non-2xx killfeed-read response, preserving the upstream `code`
 * alongside `error`. Unlike the shared forwardError, this keeps the code so the
 * editor can tell a missing xAI key (409 xai_key_missing) or an xAI upstream
 * failure (502 xai_request_failed, whose message is already bounded and
 * key-free) apart from other errors. Only the two known fields are forwarded,
 * never an arbitrary upstream object; other 5xx stay generic so orchestrator
 * internals never leak.
 */
async function forwardKillfeedError(res: Response): Promise<Response> {
  if (res.status >= 500 && res.status !== 502) {
    return NextResponse.json({ error: 'upstream error' }, { status: res.status });
  }
  const text = await res.text().catch(() => '');
  try {
    const body = JSON.parse(text) as { error?: unknown; code?: unknown };
    if (body && typeof body === 'object') {
      const payload: { error: string; code?: string } = {
        error: typeof body.error === 'string' ? body.error : `orchestrator error (${res.status})`,
      };
      if (typeof body.code === 'string') payload.code = body.code;
      return NextResponse.json(payload, { status: res.status });
    }
  } catch {
    // not JSON; fall through to a wrapped text error
  }
  return NextResponse.json({ error: text || `orchestrator error (${res.status})` }, { status: res.status });
}

/**
 * POST /api/streams/{jobId}/killfeed-read — read the confirmed kills visible at
 * a cue with the xAI vision reader. Forwards {clip_id, cue_seconds} upstream and
 * relays the {kills} JSON. The orchestrator's own 409 (xai_key_missing / ffmpeg
 * unconfigured) passes through with its code and message so the editor can tell
 * a missing xAI key apart from other failures.
 */
export async function POST(request: Request, { params }: { params: Promise<{ jobId: string }> }): Promise<Response> {
  const { jobId } = await params;
  const url = streamJobUrl(jobId, '/killfeed-read');
  if (!url) return NextResponse.json({ error: 'invalid job id' }, { status: 400 });

  const bodyText = await request.text();
  const res = await callOrchestrator(url, {
    method: 'POST',
    headers: { ...mutationHeaders(), 'Content-Type': 'application/json' },
    body: bodyText,
  });
  if (res === null) return serviceUnavailable();
  if (!res.ok) return forwardKillfeedError(res);

  return NextResponse.json((await res.json()) as unknown);
}
