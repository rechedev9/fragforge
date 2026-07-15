const ALLOWED_FETCH_SITES = new Set(['none', 'same-origin']);

const INVALID_HOST_ERROR = 'local API host rejected';
const CROSS_SITE_ERROR = 'cross-site request blocked';

/**
 * Validates that an API request targets the loopback Studio origin and was not
 * initiated by another browser origin. The Host check also blocks DNS
 * rebinding: an attacker-controlled hostname is rejected even if DNS resolves
 * it to 127.0.0.1.
 *
 * Requests from non-browser local clients normally omit both Origin and
 * Sec-Fetch-Site. They remain supported, but only with an explicit loopback
 * Host and port.
 */
export function localAPIRequestError(headers: Headers): string | undefined {
  const host = headers.get('host')?.trim() ?? '';
  if (!isLoopbackHostWithPort(host)) return INVALID_HOST_ERROR;

  const fetchSite = headers.get('sec-fetch-site');
  if (fetchSite !== null && !ALLOWED_FETCH_SITES.has(fetchSite.toLowerCase())) {
    return CROSS_SITE_ERROR;
  }

  const origin = headers.get('origin');
  if (origin === null) return undefined;
  try {
    const parsed = new URL(origin);
    if (
      parsed.username !== ''
      || parsed.password !== ''
      || parsed.pathname !== '/'
      || parsed.search !== ''
      || parsed.hash !== ''
      || parsed.host.toLowerCase() !== host.toLowerCase()
    ) {
      return CROSS_SITE_ERROR;
    }
  } catch {
    return CROSS_SITE_ERROR;
  }
  return undefined;
}

function isLoopbackHostWithPort(host: string): boolean {
  const match = /^(localhost|\[::1\]|(\d{1,3}(?:\.\d{1,3}){3})):(\d{1,5})$/i.exec(host);
  if (match === null) return false;

  const port = Number(match[3]);
  if (!Number.isInteger(port) || port < 1 || port > 65_535) return false;

  const hostname = match[1]?.toLowerCase();
  if (hostname === 'localhost' || hostname === '[::1]') return true;

  const octets = match[2]?.split('.').map(Number);
  return octets !== undefined
    && octets.length === 4
    && octets[0] === 127
    && octets.every((octet) => Number.isInteger(octet) && octet >= 0 && octet <= 255);
}
