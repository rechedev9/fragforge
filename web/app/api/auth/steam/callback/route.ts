import { NextResponse } from 'next/server';
import { cookies } from 'next/headers';
import { verifyCallback, fetchPersona } from '@/lib/auth/steam';
import { signSession, verifySession, isGuest, SESSION_COOKIE, SESSION_MAX_AGE } from '@/lib/auth/session';
import { ensureUser } from '@/lib/cloud/users';
import { migrateGuestAgents } from '@/lib/cloud/pcPairing';
import { supabaseAdmin } from '@/lib/supabase/server';

export const runtime = 'nodejs';

/**
 * GET /api/auth/steam/callback — Steam's OpenID return. Verifies the assertion
 * with Steam, mints a signed session cookie for the confirmed SteamID, and sends
 * the user into onboarding. A forged/invalid callback bounces back to the login
 * page with ?auth=failed.
 */
export async function GET(request: Request): Promise<Response> {
  const url = new URL(request.url);
  const steamid = await verifyCallback(url.searchParams);
  if (!steamid) {
    return NextResponse.redirect(`${url.origin}/?auth=failed`);
  }

  const { persona, avatar } = await fetchPersona(steamid);
  const jar = await cookies();
  const prior = verifySession(jar.get(SESSION_COOKIE)?.value);
  try {
    const steamUserId = await ensureUser(steamid, persona, avatar);
    // If the browser paired a PC as a guest before logging in, move those agents
    // to the Steam user so the PC stays paired rather than silently unpairing.
    if (prior && isGuest(prior)) {
      const guestUserId = await ensureUser(prior.steamid64, prior.persona, prior.avatar);
      if (guestUserId !== steamUserId) {
        await migrateGuestAgents(guestUserId, steamUserId, supabaseAdmin());
      }
    }
  } catch (err) {
    console.error('ensureUser/guest-migration on login callback failed (continuing, will retry on pair/status)', err);
  }
  const token = signSession({ steamid64: steamid, persona, avatar, matchHistoryLinked: false });

  jar.set(SESSION_COOKIE, token, {
    httpOnly: true,
    sameSite: 'lax',
    path: '/',
    maxAge: SESSION_MAX_AGE,
  });

  return NextResponse.redirect(`${url.origin}/connect`);
}
