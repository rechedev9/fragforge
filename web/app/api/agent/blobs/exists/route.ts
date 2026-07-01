import { NextResponse } from 'next/server';
import { resolveAgent } from '@/lib/cloud/agentAuth';
import { agentOwnsKey } from '@/lib/cloud/blobAuth';
import { supabaseAdmin } from '@/lib/supabase/server';

export const runtime = 'nodejs';

export async function GET(request: Request): Promise<Response> {
  const agent = await resolveAgent(request);
  if (!agent) return NextResponse.json({ error: 'unauthorized' }, { status: 401 });
  const key = new URL(request.url).searchParams.get('key') ?? '';
  if (!(await agentOwnsKey(key, agent.userId))) {
    return NextResponse.json({ error: 'forbidden' }, { status: 403 });
  }
  const isDemo = key.startsWith('demos/');
  const bucket = isDemo ? 'demos' : 'artifacts';
  const path = isDemo ? key.slice('demos/'.length) : key;
  const slash = path.lastIndexOf('/');
  const dir = slash >= 0 ? path.slice(0, slash) : '';
  const name = slash >= 0 ? path.slice(slash + 1) : path;
  const { data, error } = await supabaseAdmin().storage.from(bucket).list(dir, { search: name });
  if (error) return NextResponse.json({ error: error.message }, { status: 500 });
  return NextResponse.json({ exists: !!data?.some((f) => f.name === name) });
}
