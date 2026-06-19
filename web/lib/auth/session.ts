// Server-only signed-cookie session for real Steam logins. The cookie holds the
// authenticated SteamID64 plus cached persona/avatar, signed with HMAC-SHA256 so
// the client cannot forge or tamper with it. Imported only by route handlers
// (runtime: nodejs); never by client code.
import crypto from 'node:crypto';

export const SESSION_COOKIE = 'ff_session';
export const SESSION_MAX_AGE = 60 * 60 * 24 * 7; // 7 days

export type SessionPayload = {
  steamid64: string;
  persona: string;
  avatar: string;
  matchHistoryLinked: boolean;
};

function secret(): string {
  const fromEnv = process.env.ZV_SESSION_SECRET;
  if (fromEnv && fromEnv.length >= 16) return fromEnv;
  // Fail closed in production: a weak/absent secret would let anyone forge a
  // session cookie. Local/dev keeps a default so the BYO one-box run just works.
  if (process.env.NODE_ENV === 'production') {
    throw new Error('ZV_SESSION_SECRET must be set to a strong value (>=16 chars) in production');
  }
  return 'fragforge-dev-session-secret-change-me';
}

function hmac(body: string): string {
  return crypto.createHmac('sha256', secret()).update(body).digest('base64url');
}

export function signSession(payload: SessionPayload): string {
  const body = Buffer.from(JSON.stringify(payload)).toString('base64url');
  return `${body}.${hmac(body)}`;
}

export function verifySession(token: string | undefined): SessionPayload | null {
  if (!token) return null;
  const dot = token.lastIndexOf('.');
  if (dot <= 0) return null;
  const body = token.slice(0, dot);
  const mac = token.slice(dot + 1);
  const expected = hmac(body);
  const macBuf = Buffer.from(mac);
  const expBuf = Buffer.from(expected);
  if (macBuf.length !== expBuf.length || !crypto.timingSafeEqual(macBuf, expBuf)) return null;
  try {
    const parsed = JSON.parse(Buffer.from(body, 'base64url').toString('utf8')) as Partial<SessionPayload>;
    if (parsed && typeof parsed.steamid64 === 'string' && /^\d{17}$/.test(parsed.steamid64)) {
      return {
        steamid64: parsed.steamid64,
        persona: typeof parsed.persona === 'string' ? parsed.persona : '',
        avatar: typeof parsed.avatar === 'string' ? parsed.avatar : '',
        matchHistoryLinked: parsed.matchHistoryLinked === true,
      };
    }
  } catch {
    // tampered / malformed payload
  }
  return null;
}
