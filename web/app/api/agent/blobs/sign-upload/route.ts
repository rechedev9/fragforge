import { NextResponse } from 'next/server';
import { resolveAgent } from '@/lib/cloud/agentAuth';
import { supabaseAdmin } from '@/lib/supabase/server';

export const runtime = 'nodejs';

function bucketFor(key: string): { bucket: string; path: string } {
  return key.startsWith('demos/')
    ? { bucket: 'demos', path: key.slice('demos/'.length) }
    : { bucket: 'artifacts', path: key };
}

export async function POST(request: Request): Promise<Response> {
  const agent = await resolveAgent(request);
  if (!agent) return NextResponse.json({ error: 'unauthorized' }, { status: 401 });
  const { key } = (await request.json()) as { key: string };
  const { bucket, path } = bucketFor(key);
  const { data, error } = await supabaseAdmin().storage.from(bucket).createSignedUploadUrl(path);
  if (error) return NextResponse.json({ error: error.message }, { status: 500 });
  return NextResponse.json({ url: data.signedUrl, token: data.token, bucket });
}
