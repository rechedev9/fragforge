import { NextResponse } from 'next/server';
import { resolveAgent } from '@/lib/cloud/agentAuth';
import { agentOwnsKey, blobLocation } from '@/lib/cloud/blobAuth';
import { supabaseAdmin } from '@/lib/supabase/server';

export const runtime = 'nodejs';

export async function GET(request: Request): Promise<Response> {
  const agent = await resolveAgent(request);
  if (!agent) return NextResponse.json({ error: 'unauthorized' }, { status: 401 });
  const key = new URL(request.url).searchParams.get('key') ?? '';
  if (!(await agentOwnsKey(key, agent.userId))) {
    return NextResponse.json({ error: 'forbidden' }, { status: 403 });
  }
  const { bucket, path } = blobLocation(key);
  const { data, error } = await supabaseAdmin().storage.from(bucket).createSignedUrl(path, 900);
  if (error) return NextResponse.json({ error: error.message }, { status: 500 });
  return NextResponse.json({ url: data.signedUrl });
}
