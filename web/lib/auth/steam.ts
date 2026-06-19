// Server-only Steam helpers: OpenID 2.0 login (identity) + Steam Web API calls
// (persona, CS2 match-sharing codes). OpenID needs no API key and works on
// localhost (the browser does the redirect). Match history needs STEAM_WEB_API_KEY.
// Imported only by route handlers (runtime: nodejs).

const STEAM_OPENID = 'https://steamcommunity.com/openid/login';
const CLAIMED_ID_RE = /^https:\/\/steamcommunity\.com\/openid\/id\/(\d{17})$/;

/** Builds the Steam OpenID 2.0 redirect URL for a checkid_setup login. */
export function buildLoginUrl(returnTo: string, realm: string): string {
  const params = new URLSearchParams({
    'openid.ns': 'http://specs.openid.net/auth/2.0',
    'openid.mode': 'checkid_setup',
    'openid.return_to': returnTo,
    'openid.realm': realm,
    'openid.identity': 'http://specs.openid.net/auth/2.0/identifier_select',
    'openid.claimed_id': 'http://specs.openid.net/auth/2.0/identifier_select',
  });
  return `${STEAM_OPENID}?${params.toString()}`;
}

/**
 * Verifies an OpenID callback by echoing the params back to Steam with
 * mode=check_authentication and confirming is_valid:true, then extracts the
 * 64-bit SteamID from the (server-confirmed) claimed_id. Returns null on any
 * failure so a forged callback cannot mint a session.
 */
export async function verifyCallback(query: URLSearchParams): Promise<string | null> {
  const params = new URLSearchParams();
  for (const [k, v] of query) {
    if (k.startsWith('openid.')) params.set(k, v);
  }
  params.set('openid.mode', 'check_authentication');

  const res = await fetch(STEAM_OPENID, {
    method: 'POST',
    headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
    body: params.toString(),
    cache: 'no-store',
  });
  if (!res.ok) return null;
  const text = await res.text();
  if (!/is_valid\s*:\s*true/i.test(text)) return null;

  const m = CLAIMED_ID_RE.exec(query.get('openid.claimed_id') ?? '');
  return m ? m[1] : null;
}

/** Best-effort persona/avatar lookup; falls back to a placeholder without a key. */
export async function fetchPersona(steamid64: string): Promise<{ persona: string; avatar: string }> {
  const key = process.env.STEAM_WEB_API_KEY;
  const fallback = { persona: `Player ${steamid64.slice(-4)}`, avatar: '' };
  if (!key) return fallback;
  try {
    const res = await fetch(
      `https://api.steampowered.com/ISteamUser/GetPlayerSummaries/v2/?key=${key}&steamids=${steamid64}`,
      { cache: 'no-store' },
    );
    if (!res.ok) return fallback;
    const data = (await res.json()) as { response?: { players?: Array<{ personaname?: string; avatarfull?: string; avatar?: string }> } };
    const p = data.response?.players?.[0];
    if (p) return { persona: p.personaname ?? fallback.persona, avatar: p.avatarfull ?? p.avatar ?? '' };
  } catch {
    // network / API error: degrade to the placeholder
  }
  return fallback;
}

/** Stable, non-secret failure codes the match-history route maps to HTTP status. */
export type SteamErrorCode = 'steam_not_configured' | 'steam_unreachable';

/**
 * A classified, safe-to-surface Steam Web API failure. `code` is stable and the
 * `message` is user-facing and never contains the API key, the request URL, or
 * any underlying Node error text. Wrapped causes stay in `cause` for server logs.
 */
export class SteamApiError extends Error {
  readonly code: SteamErrorCode;
  constructor(code: SteamErrorCode, message: string, options?: { cause?: unknown }) {
    super(message);
    this.name = 'SteamApiError';
    this.code = code;
    if (options?.cause !== undefined) this.cause = options.cause;
  }
}

/** Outcome of a share-code enumeration: the codes plus whether Steam was queried at all. */
export type SharecodeResult = {
  codes: string[];
  /** True once at least one Steam lookup returned a response; lets the caller tell a
   *  clean "no newer matches / bad codes" run apart from a transport failure. */
  queried: boolean;
};

/**
 * Enumerates a player's recent CS2 match-sharing codes via the Steam Web API,
 * starting just after `knownCode`. Each returned code becomes the next query, up
 * to `max` matches. Requires STEAM_WEB_API_KEY plus the player's authentication
 * code (steamidkey).
 *
 * Throws a SteamApiError on a missing key ('steam_not_configured') or a network /
 * transport failure ('steam_unreachable'). It does NOT throw on a non-ok Steam
 * response or an empty result: those return { codes: [...], queried: true } so the
 * caller can distinguish "Steam answered, no newer codes" (likely a bad/expired
 * auth code or an already-latest sharecode) from a true outage. Error messages and
 * the returned value never expose the API key or the request URL.
 */
export async function enumerateSharecodes(
  steamid64: string,
  authCode: string,
  knownCode: string,
  max = 50,
): Promise<SharecodeResult> {
  const key = process.env.STEAM_WEB_API_KEY;
  if (!key) {
    throw new SteamApiError(
      'steam_not_configured',
      "match-history linking isn't set up on this server",
    );
  }

  const codes: string[] = [];
  let queried = false;
  let current = knownCode;
  for (let i = 0; i < max; i++) {
    const url =
      `https://api.steampowered.com/ICSGOPlayers_730/GetNextMatchSharingCode/v1/` +
      `?key=${key}&steamid=${steamid64}&steamidkey=${encodeURIComponent(authCode)}&knowncode=${encodeURIComponent(current)}`;
    let res: Response;
    try {
      res = await fetch(url, { cache: 'no-store' });
    } catch (err) {
      // Transport failure (DNS/TLS/offline). The cause may embed the URL+key, so it
      // stays in `cause` for server logs only and never reaches the message/client.
      throw new SteamApiError('steam_unreachable', 'could not reach the Steam Web API', { cause: err });
    }
    queried = true;
    // A 4xx/5xx from Steam (bad auth code, rate limit, no newer match) is a normal
    // terminal state, not an outage: stop and let the caller read it as "no codes".
    if (!res.ok) break;
    let data: { result?: { nextcode?: string } };
    try {
      data = (await res.json()) as { result?: { nextcode?: string } };
    } catch (err) {
      throw new SteamApiError('steam_unreachable', 'could not read the Steam Web API response', { cause: err });
    }
    const next = data.result?.nextcode;
    if (!next || next === 'n/a') break;
    codes.push(next);
    current = next;
  }
  return { codes, queried };
}
