import { NextResponse } from 'next/server';
import { cookies } from 'next/headers';
import { verifySession, SESSION_COOKIE } from '@/lib/auth/session';
import { ensureUser } from '@/lib/cloud/users';
import { supabaseAdmin } from '@/lib/supabase/server';
import { hashToken } from '@/lib/cloud/agentAuth';

export const runtime = 'nodejs';

// A short, human-typeable one-time pairing code (no ambiguous chars).
function pairingCode(): string {
  const alphabet = 'ABCDEFGHJKMNPQRSTUVWXYZ23456789';
  const bytes = crypto.getRandomValues(new Uint8Array(8));
  return Array.from(bytes, (b) => alphabet[b % alphabet.length]).join('');
}

/**
 * POST /api/pc/pair — the authenticated browser mints a one-time pairing code
 * for the desktop agent to redeem at /api/agent/pair. Fails closed (500) if
 * Supabase is unreachable: without the DB there is nothing to pair against.
 */
export async function POST(): Promise<Response> {
  const jar = await cookies();
  const s = verifySession(jar.get(SESSION_COOKIE)?.value);
  if (!s) return NextResponse.json({ error: 'unauthorized' }, { status: 401 });

  const userId = await ensureUser(s.steamid64, s.persona, s.avatar);
  const code = pairingCode();
  // Store the code hashed with a 10-minute TTL encoded in the name field.
  const expires = Date.now() + 10 * 60 * 1000;
  const { error } = await supabaseAdmin().from('agents').insert({
    user_id: userId,
    name: `pending:${expires}`,
    token_hash: hashToken(`code:${code}`),
  });
  if (error) return NextResponse.json({ error: 'pairing failed' }, { status: 500 });
  return NextResponse.json({ pairingCode: code });
}
