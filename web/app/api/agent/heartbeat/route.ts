import { NextResponse } from 'next/server';
import { resolveAgent } from '@/lib/cloud/agentAuth';
import { supabaseAdmin } from '@/lib/supabase/server';

export const runtime = 'nodejs';

export async function POST(request: Request): Promise<Response> {
  const agent = await resolveAgent(request);
  if (!agent) return NextResponse.json({ error: 'unauthorized' }, { status: 401 });
  const body = (await request.json().catch(() => ({}))) as { capabilities?: unknown };
  await supabaseAdmin()
    .from('agents')
    .update({ last_heartbeat_at: new Date().toISOString(), capabilities: body.capabilities ?? {} })
    .eq('id', agent.agentId);
  return new NextResponse(null, { status: 204 });
}
