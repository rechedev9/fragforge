import { NextResponse } from 'next/server';
import { resolveAgent } from '@/lib/cloud/agentAuth';
import { supabaseAdmin } from '@/lib/supabase/server';
import { toJobDto, type JobRow } from '@/lib/cloud/jobDto';

export const runtime = 'nodejs';

export async function POST(request: Request): Promise<Response> {
  const agent = await resolveAgent(request);
  if (!agent) return NextResponse.json({ error: 'unauthorized' }, { status: 401 });

  const db = supabaseAdmin();
  const { data: claimed, error } = await db.rpc('claim_next_job', { p_agent: agent.agentId, p_lease_seconds: 900 });
  if (error) return NextResponse.json({ error: 'claim failed' }, { status: 500 });
  if (!claimed) return new NextResponse(null, { status: 204 });

  const { data: row, error: rowError } = await db
    .from('jobs')
    .select('demo_id, target_steamid, rules, type, demos(storage_key, sha256)')
    .eq('id', claimed.id)
    .single();
  if (rowError || !row) return NextResponse.json({ error: 'claim failed' }, { status: 500 });
  return NextResponse.json({ job: toJobDto(row as unknown as JobRow), jobType: row.type });
}
