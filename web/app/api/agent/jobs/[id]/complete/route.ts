import { NextResponse } from 'next/server';
import { resolveAgent } from '@/lib/cloud/agentAuth';
import { supabaseAdmin } from '@/lib/supabase/server';

export const runtime = 'nodejs';

export async function POST(request: Request, { params }: { params: Promise<{ id: string }> }): Promise<Response> {
  const agent = await resolveAgent(request);
  if (!agent) return NextResponse.json({ error: 'unauthorized' }, { status: 401 });
  const { id } = await params;
  const db = supabaseAdmin();
  const { data: job } = await db
    .from('jobs')
    .select('id, type')
    .eq('demo_id', id)
    .eq('agent_id', agent.agentId)
    .not('state', 'in', '(done,failed)')
    .maybeSingle();
  if (!job) return NextResponse.json({ error: 'not found' }, { status: 404 });
  const finalState = job.type === 'scan' ? 'scanned' : job.type === 'parse' ? 'parsed' : 'done';
  await db.from('jobs').update({ state: finalState, updated_at: new Date().toISOString() }).eq('id', job.id);
  return new NextResponse(null, { status: 204 });
}
