/**
 * Client-only settings + probe for the LOCAL agent in hosted mode (Topology A).
 *
 * In hosted mode the browser runs on the end user's PC and calls the local Go
 * agent DIRECTLY at a configurable base URL, authenticating with a pairing token
 * carried in the X-FragForge-Token header on EVERY request. Both the URL and the
 * token are user settings persisted to localStorage (the token never reaches our
 * server). This module is the single source of truth for reading/writing those
 * settings and for probing whether the agent is reachable.
 *
 * NOTE: this module is client-only but is intentionally NOT marked 'server-only',
 * because the api/streams client factories import it during module evaluation on
 * the server too. Every localStorage access is therefore SSR-guarded (typeof
 * window) so importing it on the server is inert (returns defaults / no token).
 */

export const FF_AGENT_URL_KEY = 'ff_agent_url';
export const FF_AGENT_TOKEN_KEY = 'ff_agent_token';

/** Default local agent bind for hosted mode (127.0.0.1:8787, loopback only). */
export const DEFAULT_AGENT_URL = 'http://127.0.0.1:8787';

/** True only in a browser, where localStorage exists. */
function hasWindow(): boolean {
  return typeof window !== 'undefined';
}

/** Trims a trailing slash so callers can safely append `/api/...`. */
function trimTrailingSlash(url: string): string {
  return url.replace(/\/+$/, '');
}

/**
 * The configured agent base URL (localStorage) or the loopback default. Always
 * trailing-slash trimmed. SSR-safe: returns the default on the server.
 */
export function agentBaseUrl(): string {
  if (!hasWindow()) return DEFAULT_AGENT_URL;
  try {
    const raw = window.localStorage.getItem(FF_AGENT_URL_KEY);
    const value = raw && raw.trim() ? raw.trim() : DEFAULT_AGENT_URL;
    return trimTrailingSlash(value);
  } catch {
    return DEFAULT_AGENT_URL;
  }
}

/** The pasted pairing token, or '' when unset. SSR-safe (returns ''). */
export function agentToken(): string {
  if (!hasWindow()) return '';
  try {
    return window.localStorage.getItem(FF_AGENT_TOKEN_KEY)?.trim() ?? '';
  } catch {
    return '';
  }
}

export function setAgentUrl(url: string): void {
  if (!hasWindow()) return;
  try {
    const trimmed = url.trim();
    if (trimmed) window.localStorage.setItem(FF_AGENT_URL_KEY, trimTrailingSlash(trimmed));
    else window.localStorage.removeItem(FF_AGENT_URL_KEY);
  } catch {
    // Private-mode / disabled storage: setting is best-effort.
  }
}

export function setAgentToken(token: string): void {
  if (!hasWindow()) return;
  try {
    const trimmed = token.trim();
    if (trimmed) window.localStorage.setItem(FF_AGENT_TOKEN_KEY, trimmed);
    else window.localStorage.removeItem(FF_AGENT_TOKEN_KEY);
  } catch {
    // Private-mode / disabled storage: setting is best-effort.
  }
}

/**
 * The auth header sent on every agent request: the pairing token when set, else
 * empty (so a mis-configured / unpaired setup fails closed at the agent, which
 * requires the token for any cross-site Origin). Returned as a plain object so it
 * merges cleanly into a fetch init's headers.
 */
export function agentHeaders(): Record<string, string> {
  const token = agentToken();
  return token ? { 'X-FragForge-Token': token } : {};
}

/** Milliseconds before a probe aborts; the agent is loopback, so this is short. */
const PROBE_TIMEOUT_MS = 4000;

export type AgentProbeResult = { connected: boolean; reason?: string };

/**
 * Probes the local agent for reachability. Tries GET `${base}/healthz` first and
 * falls back to `${base}/api/capabilities` (older agents may lack /healthz),
 * both with the token header and a short abort timeout. Any network/CORS error,
 * timeout, or non-2xx maps to disconnected with a human reason - the browser
 * silently blocks a cross-origin fetch the agent did not CORS-allow, which
 * surfaces here as a generic network failure rather than a status.
 */
export async function probeAgent(base: string = agentBaseUrl()): Promise<AgentProbeResult> {
  const url = trimTrailingSlash(base);
  const viaHealthz = await probeOnce(`${url}/healthz`);
  if (viaHealthz.connected) return viaHealthz;
  const viaCapabilities = await probeOnce(`${url}/api/capabilities`);
  if (viaCapabilities.connected) return viaCapabilities;
  // Prefer the capabilities reason (the endpoint every agent serves) when both
  // failed, so the user sees the most representative error.
  return viaCapabilities;
}

async function probeOnce(url: string): Promise<AgentProbeResult> {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), PROBE_TIMEOUT_MS);
  try {
    const res = await fetch(url, {
      method: 'GET',
      headers: agentHeaders(),
      cache: 'no-store',
      signal: controller.signal,
    });
    if (res.ok) return { connected: true };
    if (res.status === 401 || res.status === 403) {
      return { connected: false, reason: 'token rejected by the agent' };
    }
    return { connected: false, reason: `agent responded ${res.status}` };
  } catch {
    // Network error, CORS block, DNS, or the abort timeout: all indistinguishable
    // to fetch, so report a single actionable reason.
    return { connected: false, reason: 'agent unreachable (is it running?)' };
  } finally {
    clearTimeout(timer);
  }
}
