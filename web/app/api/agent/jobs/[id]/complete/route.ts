import { NextResponse } from 'next/server';
import { resolveAgent } from '@/lib/cloud/agentAuth';
import { supabaseAdmin } from '@/lib/supabase/server';
import { JOB_STATE, JOB_TYPE, TERMINAL_STATES_FILTER, type JobState } from '@/lib/cloud/jobDto';

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
    .not('state', 'in', TERMINAL_STATES_FILTER)
    .maybeSingle();
  if (!job) return NextResponse.json({ error: 'not found' }, { status: 404 });
  let finalState: JobState;
  if (job.type === JOB_TYPE.scan) {
    finalState = JOB_STATE.scanned;
  } else if (job.type === JOB_TYPE.parse) {
    finalState = JOB_STATE.parsed;
  } else {
    finalState = JOB_STATE.done;
  }
  await db.from('jobs').update({ state: finalState, updated_at: new Date().toISOString() }).eq('id', job.id);
  return new NextResponse(null, { status: 204 });
}
