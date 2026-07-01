import { NextResponse } from 'next/server';
import { resolveAgent } from '@/lib/cloud/agentAuth';
import { supabaseAdmin } from '@/lib/supabase/server';
import { STATE_FROM_GO } from '@/lib/cloud/jobDto';

export const runtime = 'nodejs';

export async function POST(request: Request, { params }: { params: Promise<{ id: string }> }): Promise<Response> {
  const agent = await resolveAgent(request);
  if (!agent) return NextResponse.json({ error: 'unauthorized' }, { status: 401 });
  const { id } = await params;
  const body = (await request.json().catch(() => ({}))) as { status?: number; failure_reason?: string };
  const state = STATE_FROM_GO[body.status ?? -1] ?? 'running';
  await supabaseAdmin()
    .from('jobs')
    .update({ state, error: body.failure_reason ?? '', lease_expires_at: new Date(Date.now() + 900_000).toISOString(), updated_at: new Date().toISOString() })
    .eq('demo_id', id)
    .eq('agent_id', agent.agentId)
    .not('state', 'in', '(done,failed)');
  return new NextResponse(null, { status: 204 });
}
