import { NextResponse } from 'next/server';
import { streamJobUrl, mutationHeaders, forwardError, callOrchestrator, serviceUnavailable } from '../../_lib';

export const runtime = 'nodejs';

/** GET /api/streams/{jobId}/edit-plan — proxy the current edit plan JSON. */
export async function GET(_request: Request, { params }: { params: Promise<{ jobId: string }> }): Promise<Response> {
  const { jobId } = await params;
  const url = streamJobUrl(jobId, '/edit-plan');
  if (!url) return NextResponse.json({ error: 'invalid job id' }, { status: 400 });

  const res = await callOrchestrator(url);
  if (res === null) return serviceUnavailable();
  if (!res.ok) return forwardError(res);

  return NextResponse.json((await res.json()) as unknown);
}

/**
 * PUT /api/streams/{jobId}/edit-plan — save the facecam/gameplay crops, clip
 * ranges, and caption settings the user picked in the editor.
 */
export async function PUT(request: Request, { params }: { params: Promise<{ jobId: string }> }): Promise<Response> {
  const { jobId } = await params;
  const url = streamJobUrl(jobId, '/edit-plan');
  if (!url) return NextResponse.json({ error: 'invalid job id' }, { status: 400 });

  const bodyText = await request.text();
  const res = await callOrchestrator(url, {
    method: 'PUT',
    headers: { ...mutationHeaders(), 'Content-Type': 'application/json' },
    body: bodyText,
  });
  if (res === null) return serviceUnavailable();
  if (!res.ok) return forwardError(res);

  return NextResponse.json((await res.json()) as unknown);
}
