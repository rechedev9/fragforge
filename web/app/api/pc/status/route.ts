import { NextResponse } from 'next/server';
import { cookies } from 'next/headers';
import { verifySession, SESSION_COOKIE } from '@/lib/auth/session';
import { ensureUser } from '@/lib/cloud/users';
import { supabaseAdmin } from '@/lib/supabase/server';
import { pcStatus } from '@/lib/cloud/pcStatus';

export const runtime = 'nodejs';

/**
 * GET /api/pc/status — session-gated pairing + liveness for the signed-in user's
 * agent, plus the loopback endpoint the browser dials in cloud mode. Returns
 * `{ paired, online, loopback: { port, token } | null }`; `loopback` is non-null
 * only when a paired agent has reported one. The token here is the browser's
 * credential for the same-machine loopback data plane, so this route is the only
 * place it crosses the control plane.
 */
export async function GET(): Promise<Response> {
  const jar = await cookies();
  const s = verifySession(jar.get(SESSION_COOKIE)?.value);
  if (!s) return NextResponse.json({ error: 'unauthorized' }, { status: 401 });

  const userId = await ensureUser(s.steamid64, s.persona, s.avatar);
  const { data } = await supabaseAdmin()
    .from('agents')
    .select('last_heartbeat_at, loopback_token, loopback_port')
    .eq('user_id', userId)
    .not('name', 'like', 'pending:%')
    .order('last_heartbeat_at', { ascending: false, nullsFirst: false })
    .limit(1);

  return NextResponse.json(pcStatus(data?.[0]), { headers: { 'cache-control': 'no-store' } });
}
