import { NextResponse } from 'next/server';
import { proxyStream, streamJobUrl } from '../../../../../_lib';

export const runtime = 'nodejs';
const SAFE = /^[A-Za-z0-9][A-Za-z0-9._-]*$/;

export async function GET(
  request: Request,
  { params }: { params: Promise<{ jobId: string; variant: string; name: string }> },
): Promise<Response> {
  const { jobId, variant, name } = await params;
  if (!SAFE.test(variant) || !SAFE.test(name)) return NextResponse.json({ error: 'invalid path' }, { status: 400 });
  const url = streamJobUrl(jobId, `/renders/${variant}/delivery/${encodeURIComponent(name)}`);
  if (!url) return NextResponse.json({ error: 'invalid job id' }, { status: 400 });
  return proxyStream(url, '', request);
}
