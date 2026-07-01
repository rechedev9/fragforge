import { NextResponse } from 'next/server';
import { cookies } from 'next/headers';
import { verifyCallback, fetchPersona } from '@/lib/auth/steam';
import { signSession, SESSION_COOKIE, SESSION_MAX_AGE } from '@/lib/auth/session';
import { ensureUser } from '@/lib/cloud/users';

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
  await ensureUser(steamid, persona, avatar);
  const token = signSession({ steamid64: steamid, persona, avatar, matchHistoryLinked: false });

  const jar = await cookies();
  jar.set(SESSION_COOKIE, token, {
    httpOnly: true,
    sameSite: 'lax',
    path: '/',
    maxAge: SESSION_MAX_AGE,
  });

  return NextResponse.redirect(`${url.origin}/connect`);
}
