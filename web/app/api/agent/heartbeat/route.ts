import { NextResponse } from 'next/server';
import { resolveAgent } from '@/lib/cloud/agentAuth';
import { supabaseAdmin } from '@/lib/supabase/server';

export const runtime = 'nodejs';

export async function POST(request: Request): Promise<Response> {
  const agent = await resolveAgent(request);
  if (!agent) return NextResponse.json({ error: 'unauthorized' }, { status: 401 });
  const body = (await request.json().catch(() => ({}))) as {
    capabilities?: unknown;
    loopbackToken?: string;
    loopbackPort?: number;
  };
  // Refresh the loopback endpoint on every heartbeat so a re-paired or
  // re-configured agent (new token/port) propagates without re-pairing. Only
  // overwrite when the agent actually reports one, so a heartbeat that omits
  // these fields never clears a good endpoint.
  const update: Record<string, unknown> = {
    last_heartbeat_at: new Date().toISOString(),
    capabilities: body.capabilities ?? {},
  };
  if (typeof body.loopbackToken === 'string') update.loopback_token = body.loopbackToken;
  if (Number.isInteger(body.loopbackPort)) update.loopback_port = body.loopbackPort;
  await supabaseAdmin().from('agents').update(update).eq('id', agent.agentId);
  return new NextResponse(null, { status: 204 });
}
