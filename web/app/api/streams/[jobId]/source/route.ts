import { NextResponse } from 'next/server';
import { streamJobUrl, proxyStream } from '../../_lib';

export const runtime = 'nodejs';

/**
 * GET /api/streams/{jobId}/source — stream the job's source MP4 (the
 * acquired/uploaded video, before rendering) so the facecam picker can paint
 * a frame from it in a <video> element. Range-aware via proxyStream, which
 * mirrors the demos renders/videos proxy.
 */
export async function GET(request: Request, { params }: { params: Promise<{ jobId: string }> }): Promise<Response> {
  const { jobId } = await params;
  const url = streamJobUrl(jobId, '/source');
  if (!url) return NextResponse.json({ error: 'invalid job id' }, { status: 400 });
  return proxyStream(url, 'video/mp4', request);
}
