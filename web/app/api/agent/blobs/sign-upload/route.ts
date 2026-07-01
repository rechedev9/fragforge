import { NextResponse } from 'next/server';
import { resolveAgent } from '@/lib/cloud/agentAuth';
import { agentOwnsKey, blobLocation } from '@/lib/cloud/blobAuth';
import { supabaseAdmin } from '@/lib/supabase/server';

export const runtime = 'nodejs';

export async function POST(request: Request): Promise<Response> {
  const agent = await resolveAgent(request);
  if (!agent) return NextResponse.json({ error: 'unauthorized' }, { status: 401 });
  const body = (await request.json().catch(() => null)) as { key?: string } | null;
  const key = body?.key;
  if (typeof key !== 'string' || key.length === 0) {
    return NextResponse.json({ error: 'missing key' }, { status: 400 });
  }
  if (!(await agentOwnsKey(key, agent.userId))) {
    return NextResponse.json({ error: 'forbidden' }, { status: 403 });
  }
  const { bucket, path } = blobLocation(key);
  const { data, error } = await supabaseAdmin().storage.from(bucket).createSignedUploadUrl(path);
  if (error) return NextResponse.json({ error: error.message }, { status: 500 });
  return NextResponse.json({ url: data.signedUrl, token: data.token, bucket });
}
