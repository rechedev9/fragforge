import { NextResponse } from 'next/server';
import { resolveAgent } from '@/lib/cloud/agentAuth';
import { supabaseAdmin } from '@/lib/supabase/server';
import { parseJobRow, toJobDto, TERMINAL_STATES_FILTER } from '@/lib/cloud/jobDto';

export const runtime = 'nodejs';

export async function GET(request: Request, { params }: { params: Promise<{ id: string }> }): Promise<Response> {
  const agent = await resolveAgent(request);
  if (!agent) return NextResponse.json({ error: 'unauthorized' }, { status: 401 });
  const { id } = await params;
  const { data: row } = await supabaseAdmin()
    .from('jobs')
    .select('demo_id, target_steamid, rules, type, demos(storage_key, sha256)')
    .eq('demo_id', id)
    .eq('agent_id', agent.agentId)
    .not('state', 'in', TERMINAL_STATES_FILTER)
    .maybeSingle();
  const job = row ? parseJobRow(row) : null;
  if (!job) return NextResponse.json({ error: 'not found' }, { status: 404 });
  return NextResponse.json(toJobDto(job));
}
