import { NextResponse } from 'next/server';
import { orchestratorUrl, proxyStream } from '../../../demos/_lib';

export const runtime = 'nodejs';

const SONG_ID_RE = /^[a-z0-9][a-z0-9-]*$/;

/** GET /api/songs/{id}/audio — stream a track for in-browser preview. */
export async function GET(
  request: Request,
  { params }: { params: Promise<{ id: string }> },
): Promise<Response> {
  const { id } = await params;
  if (!SONG_ID_RE.test(id)) {
    return NextResponse.json({ error: 'invalid song id' }, { status: 400 });
  }
  return proxyStream(`${orchestratorUrl()}/api/songs/${id}/audio`, 'audio/mpeg', request);
}
