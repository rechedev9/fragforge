import { NextResponse } from 'next/server';

/** Server-side orchestrator base; local-first default. */
export function orchestratorUrl(): string {
  return process.env.ORCHESTRATOR_URL ?? 'http://127.0.0.1:8080';
}

const UUID_RE = /^[0-9a-f]{8}(-[0-9a-f]{4}){3}-[0-9a-f]{12}$/i;

/**
 * Builds the upstream job URL for a validated UUID jobId, returning null when
 * the id is not a UUID. Defence in depth against path injection into upstream.
 */
export function jobUrl(jobId: string, suffix = ''): string | null {
  return UUID_RE.test(jobId) ? `${orchestratorUrl()}/api/jobs/${jobId}${suffix}` : null;
}

/** Mutation headers: the optional orchestrator token, server-side only. */
export function mutationHeaders(): Record<string, string> {
  const token = process.env.ORCHESTRATOR_TOKEN;
  return token ? { 'X-FragForge-Token': token } : {};
}

/**
 * Forwards a non-2xx orchestrator response as a JSON error + its status. For 4xx
 * it reuses the upstream JSON error body when present, otherwise wraps a short
 * text message. For 5xx it returns a generic error so upstream internals never
 * leak to the client. Never logs demo bytes.
 */
export async function forwardError(res: Response): Promise<Response> {
  if (res.status >= 500) {
    return NextResponse.json({ error: 'upstream error' }, { status: res.status });
  }
  const text = await res.text().catch(() => '');
  try {
    const body = JSON.parse(text) as unknown;
    if (body && typeof body === 'object') {
      return NextResponse.json(body, { status: res.status });
    }
  } catch {
    // not JSON; fall through to a wrapped text error
  }
  return NextResponse.json({ error: text || `orchestrator error (${res.status})` }, { status: res.status });
}

/**
 * Streams a binary artifact (reel mp4 / cover jpg) from the orchestrator,
 * preserving content-type and length when present. Non-2xx is forwarded as a
 * JSON error so the client can surface it. Never logs bytes.
 */
export async function proxyStream(url: string, fallbackContentType: string): Promise<Response> {
  const res = await fetch(url);
  if (!res.ok) return forwardError(res);
  const headers: Record<string, string> = {
    'content-type': res.headers.get('content-type') ?? fallbackContentType,
    'cache-control': 'no-store',
  };
  const len = res.headers.get('content-length');
  if (len) headers['content-length'] = len;
  return new Response(res.body, { status: 200, headers });
}
