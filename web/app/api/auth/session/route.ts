import { NextResponse } from 'next/server';
import { cookies } from 'next/headers';
import { verifySession, SESSION_COOKIE } from '@/lib/auth/session';

export const runtime = 'nodejs';

/** GET /api/auth/session — the current Steam user from the signed cookie, or null. */
export async function GET(): Promise<Response> {
  const jar = await cookies();
  const payload = verifySession(jar.get(SESSION_COOKIE)?.value);
  if (!payload) {
    return NextResponse.json({ user: null, matchHistoryLinked: false });
  }
  return NextResponse.json({
    user: { id: payload.steamid64, personaName: payload.persona, avatarUrl: payload.avatar },
    matchHistoryLinked: payload.matchHistoryLinked,
  });
}
