import { NextResponse } from 'next/server';
import { cookies } from 'next/headers';
import { verifySession, SESSION_COOKIE } from '@/lib/auth/session';
import { ensureUser } from '@/lib/cloud/users';
import { supabaseAdmin } from '@/lib/supabase/server';

export const runtime = 'nodejs';

export async function GET(): Promise<Response> {
  const jar = await cookies();
  const s = verifySession(jar.get(SESSION_COOKIE)?.value);
  if (!s) return NextResponse.json({ error: 'unauthorized' }, { status: 401 });

  const userId = await ensureUser(s.steamid64, s.persona, s.avatar);
  const { data } = await supabaseAdmin()
    .from('agents')
    .select('last_heartbeat_at')
    .eq('user_id', userId)
    .not('name', 'like', 'pending:%')
    .order('last_heartbeat_at', { ascending: false, nullsFirst: false })
    .limit(1);

  const agent = data?.[0];
  const online = !!agent?.last_heartbeat_at && Date.now() - new Date(agent.last_heartbeat_at).getTime() < 60_000;
  return NextResponse.json({ paired: !!agent, online });
}
