import { NextResponse } from 'next/server';
import { resolveAgent } from '@/lib/cloud/agentAuth';
import { supabaseAdmin } from '@/lib/supabase/server';

export const runtime = 'nodejs';

export async function POST(request: Request, { params }: { params: Promise<{ id: string }> }): Promise<Response> {
  const agent = await resolveAgent(request);
  if (!agent) return NextResponse.json({ error: 'unauthorized' }, { status: 401 });
  const { id } = await params;
  const body = (await request.json().catch(() => ({}))) as { kill_plan?: unknown };
  await supabaseAdmin()
    .from('jobs')
    .update({ kill_plan: body.kill_plan ?? null, updated_at: new Date().toISOString() })
    .eq('demo_id', id)
    .eq('agent_id', agent.agentId)
    .not('state', 'in', '(done,failed)');
  return new NextResponse(null, { status: 204 });
}
