import { NextResponse } from 'next/server';
import { cookies } from 'next/headers';
import { enumerateSharecodes } from '@/lib/auth/steam';
import { verifySession, signSession, SESSION_COOKIE, SESSION_MAX_AGE } from '@/lib/auth/session';

export const runtime = 'nodejs';

/**
 * POST /api/auth/match-history — link the signed-in user's CS2 match history via
 * the Steam Web API match-sharing codes. Body: { authCode, knownCode }. Requires
 * a Steam session cookie and STEAM_WEB_API_KEY on the server. On success it marks
 * the session matchHistoryLinked and returns the discovered share codes.
 */
export async function POST(request: Request): Promise<Response> {
  const jar = await cookies();
  const session = verifySession(jar.get(SESSION_COOKIE)?.value);
  if (!session) {
    return NextResponse.json({ error: 'not signed in' }, { status: 401 });
  }

  let body: { authCode?: string; knownCode?: string };
  try {
    body = (await request.json()) as { authCode?: string; knownCode?: string };
  } catch {
    return NextResponse.json({ error: 'invalid request body' }, { status: 400 });
  }
  const authCode = (body.authCode ?? '').trim();
  const knownCode = (body.knownCode ?? '').trim();
  if (!authCode || !knownCode) {
    return NextResponse.json({ error: 'authCode and knownCode are required' }, { status: 400 });
  }

  let codes: string[];
  try {
    codes = await enumerateSharecodes(session.steamid64, authCode, knownCode);
  } catch (err) {
    const message = err instanceof Error ? err.message : 'failed to query Steam';
    return NextResponse.json({ error: message }, { status: 502 });
  }

  // Persist the linked state so the onboarding gate (matchHistoryLinked) sticks.
  const token = signSession({ ...session, matchHistoryLinked: true });
  jar.set(SESSION_COOKIE, token, { httpOnly: true, sameSite: 'lax', path: '/', maxAge: SESSION_MAX_AGE });

  return NextResponse.json({ ok: true, matchesFound: codes.length, codes });
}
