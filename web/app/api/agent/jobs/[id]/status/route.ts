import { NextResponse } from 'next/server';
import { resolveAgent } from '@/lib/cloud/agentAuth';
import { supabaseAdmin } from '@/lib/supabase/server';
import { JOB_STATE, STATE_FROM_GO, TERMINAL_STATES_FILTER } from '@/lib/cloud/jobDto';

export const runtime = 'nodejs';

export async function POST(request: Request, { params }: { params: Promise<{ id: string }> }): Promise<Response> {
  const agent = await resolveAgent(request);
  if (!agent) return NextResponse.json({ error: 'unauthorized' }, { status: 401 });
  const { id } = await params;
  const body = (await request.json().catch(() => ({}))) as { status?: number; failure_reason?: string };
  const state = STATE_FROM_GO[body.status ?? -1] ?? JOB_STATE.running;
  await supabaseAdmin()
    .from('jobs')
    .update({ state, error: body.failure_reason ?? '', lease_expires_at: new Date(Date.now() + 900_000).toISOString(), updated_at: new Date().toISOString() })
    .eq('demo_id', id)
    .eq('agent_id', agent.agentId)
    .not('state', 'in', TERMINAL_STATES_FILTER);
  return new NextResponse(null, { status: 204 });
}
