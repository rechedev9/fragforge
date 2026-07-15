import { NextResponse } from 'next/server';
import { readBoundedText } from '@/lib/api/bounded-request-body';
import { orchestratorUrl, callOrchestrator, mutationHeaders, forwardError, serviceUnavailable } from '../../_lib';

export const runtime = 'nodejs';

/**
 * POST /api/streams/killfeed/notice-preview — render one kill notice to the
 * exact synthetic PNG the render uses. The upstream returns image/png bytes, so
 * this route passes the body through as an ArrayBuffer with content-type
 * image/png instead of parsing JSON, while keeping the shared 503-on-unreachable
 * contract. Not job-scoped: the notice is drawn purely from the request body.
 */
export async function POST(request: Request): Promise<Response> {
  const incoming = await readBoundedText(request);
  if (!incoming.ok) return NextResponse.json({ error: incoming.error }, { status: incoming.status });
  const res = await callOrchestrator(`${orchestratorUrl()}/api/stream-killfeed/notice-preview`, {
    method: 'POST',
    headers: { ...mutationHeaders(), 'Content-Type': 'application/json' },
    body: incoming.text,
  });
  if (res === null) return serviceUnavailable();
  if (!res.ok) return forwardError(res);

  const png = await res.arrayBuffer();
  return new Response(png, {
    status: 200,
    headers: {
      'content-type': res.headers.get('content-type') ?? 'image/png',
      'cache-control': 'no-store',
    },
  });
}
