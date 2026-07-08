import { NextResponse } from 'next/server';
import { cookies } from 'next/headers';
import { enumerateSharecodes, SteamApiError } from '@/lib/auth/steam';
import { verifySession, signSession, isGuest, SESSION_COOKIE, SESSION_MAX_AGE } from '@/lib/auth/session';

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
  // Match history queries the Steam Web API with the user's real SteamID64; a
  // guest id would fail. Require a Steam login for this route specifically.
  if (isGuest(session)) {
    return NextResponse.json(
      { error: 'Sign in with Steam to link your match history.', code: 'steam_login_required' },
      { status: 401 },
    );
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
  let queried: boolean;
  try {
    ({ codes, queried } = await enumerateSharecodes(session.steamid64, authCode, knownCode));
  } catch (err) {
    // Log only the classified, key-free signal server-side — never the raw error
    // (its `cause` can embed the request URL + API key on some Node versions).
    let signal: string;
    if (err instanceof SteamApiError) {
      signal = err.code;
    } else if (err instanceof Error) {
      signal = err.name;
    } else {
      signal = 'unknown';
    }
    console.error('match-history: enumerateSharecodes failed:', signal);
    if (err instanceof SteamApiError && err.code === 'steam_not_configured') {
      return NextResponse.json(
        {
          error:
            "Match-history linking isn't set up on this server (the operator hasn't configured a Steam Web API key). You can skip pairing and link your matches later.",
          code: 'steam_not_configured',
        },
        { status: 503 },
      );
    }
    return NextResponse.json(
      {
        error:
          "We couldn't reach Steam to check your matches. This is usually temporary — try again in a moment.",
        code: 'steam_unreachable',
      },
      { status: 502 },
    );
  }

  // Steam answered but found no newer share codes. The likeliest cause is a
  // mistyped/expired auth code or a sharecode that is already your latest match,
  // not a server fault — say so instead of a generic failure.
  if (queried && codes.length === 0) {
    return NextResponse.json(
      {
        error:
          'Steam returned no matches for those codes. Double-check your authentication code and that the sharecode is your most recent match, then try again.',
        code: 'no_matches',
      },
      { status: 422 },
    );
  }

  // Persist the linked state so the onboarding gate (matchHistoryLinked) sticks.
  const token = signSession({ ...session, matchHistoryLinked: true });
  jar.set(SESSION_COOKIE, token, { httpOnly: true, sameSite: 'lax', path: '/', maxAge: SESSION_MAX_AGE });

  return NextResponse.json({ ok: true, matchesFound: codes.length, codes });
}
