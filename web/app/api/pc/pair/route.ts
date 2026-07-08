import { NextResponse } from 'next/server';
import { cookies } from 'next/headers';
import { verifySession, SESSION_COOKIE, SESSION_MAX_AGE } from '@/lib/auth/session';
import { issuePairingCode } from '@/lib/cloud/pcPairing';
import { supabaseAdmin } from '@/lib/supabase/server';

export const runtime = 'nodejs';

/**
 * POST /api/pc/pair — mints a one-time pairing code for the desktop agent to
 * redeem at /api/agent/pair. No Steam login is required: without a session a
 * guest session is minted and its cookie set, so manually uploading a demo never
 * depends on linking a Steam account. Fails closed (500) if Supabase is
 * unreachable: without the DB there is nothing to pair against.
 */
export async function POST(): Promise<Response> {
  const jar = await cookies();
  const session = verifySession(jar.get(SESSION_COOKIE)?.value);

  const outcome = await issuePairingCode(session, supabaseAdmin());
  if (!outcome) return NextResponse.json({ error: 'pairing failed' }, { status: 500 });

  if (outcome.sessionCookie) {
    jar.set(SESSION_COOKIE, outcome.sessionCookie.token, {
      httpOnly: true,
      sameSite: 'lax',
      path: '/',
      maxAge: SESSION_MAX_AGE,
    });
  }
  return NextResponse.json({ pairingCode: outcome.pairingCode });
}
