import { NextResponse } from 'next/server';
import { SERVICE_UNAVAILABLE_CODE } from '@/lib/api/types';

/** Server-side orchestrator base; local-first default. */
export function orchestratorUrl(): string {
  return process.env.ORCHESTRATOR_URL ?? 'http://127.0.0.1:8080';
}

/**
 * Calls the orchestrator, returning null when fetch throws so a route can return
 * a clear 503 instead of a bare 500 that the UI would misread as a bad demo.
 * fetch throws only before an HTTP response exists: the orchestrator being down
 * (connection refused / DNS / socket reset) is the common case, but a malformed
 * ORCHESTRATOR_URL or an aborted request lands here too, and all are reported as
 * "service unavailable". The thrown error is logged server-side first so the
 * real cause (ECONNREFUSED, a URL typo) is not lost, since the client only sees
 * the generic 503. A reachable orchestrator's own non-2xx still comes back as a
 * Response for forwardError to translate. Never logs demo bytes (only url+method).
 */
export async function callOrchestrator(url: string, init?: RequestInit): Promise<Response | null> {
  // Carry the orchestrator token on every call, reads included. An explicitly
  // configured non-loopback orchestrator gates reads behind the token too, not
  // only mutations. mutationHeaders() is empty for the loopback desktop and dev
  // setup, where no token is required.
  const headers = { ...mutationHeaders(), ...((init?.headers as Record<string, string> | undefined) ?? {}) };
  try {
    return await fetch(url, { ...init, headers });
  } catch (err) {
    console.error(`orchestrator unreachable: ${init?.method ?? 'GET'} ${url}`, err);
    return null;
  }
}

/** 503 for when the local analysis service (orchestrator) is unreachable. */
export function serviceUnavailable(): Response {
  return NextResponse.json(
    { error: 'analysis service unavailable', code: SERVICE_UNAVAILABLE_CODE },
    { status: 503 },
  );
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
 * Forwards a non-2xx orchestrator response as a normalized { error: string }
 * JSON object plus its status. For 4xx it extracts the upstream `error` string
 * when present, otherwise wraps the raw text (or a generic message). The proxy
 * never forwards an arbitrary upstream JSON object verbatim, so the upstream's
 * body shape cannot leak through this layer. For 5xx it returns a generic error
 * so upstream internals never leak to the client. Never logs demo bytes.
 */
export async function forwardError(res: Response): Promise<Response> {
  if (res.status >= 500) {
    return NextResponse.json({ error: 'upstream error' }, { status: res.status });
  }
  const text = await res.text().catch(() => '');
  try {
    const body = JSON.parse(text) as unknown;
    if (body && typeof body === 'object' && 'error' in body && typeof (body as { error: unknown }).error === 'string') {
      return NextResponse.json({ error: (body as { error: string }).error }, { status: res.status });
    }
  } catch {
    // not JSON; fall through to a wrapped text error
  }
  return NextResponse.json({ error: text || `orchestrator error (${res.status})` }, { status: res.status });
}

/**
 * Streams a binary artifact (reel mp4 / cover jpg / song audio) from the
 * orchestrator, preserving content-type and length when present. Forwards the
 * client's Range header and mirrors the upstream status (200/206) plus range
 * headers, because the browser <video>/<audio> element needs range support to
 * start playback and seek. Non-2xx is forwarded as a JSON error so the client
 * can surface it. Never logs bytes.
 */
export async function proxyStream(
  url: string,
  fallbackContentType: string,
  request?: Request,
): Promise<Response> {
  const range = request?.headers.get('range');
  const res = await callOrchestrator(url, range ? { headers: { range } } : undefined);
  if (res === null) return serviceUnavailable();
  if (!res.ok) return forwardError(res);
  const headers: Record<string, string> = {
    'content-type': res.headers.get('content-type') ?? fallbackContentType,
    'cache-control': 'no-store',
  };
  for (const name of ['content-length', 'content-range', 'accept-ranges'] as const) {
    const value = res.headers.get(name);
    if (value) headers[name] = value;
  }
  return new Response(res.body, { status: res.status, headers });
}
