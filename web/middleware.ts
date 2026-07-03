import { NextResponse, type NextRequest } from 'next/server';

/**
 * Optional password gate for hosted deployments (e.g. the VPS docker stack).
 * When FRAGFORGE_WEB_PASSWORD is set, every route — pages, /api proxies, and
 * static assets — requires HTTP Basic Auth with that password (any username).
 * When it is unset (desktop app, local dev, Playwright), the gate is off and
 * behavior is unchanged. Basic Auth is used because the browser caches the
 * credentials per-origin and attaches them to every subsequent request,
 * including <video> streams and downloads, with no session plumbing.
 */
export function middleware(request: NextRequest): NextResponse {
  const password = process.env.FRAGFORGE_WEB_PASSWORD;
  if (!password) return NextResponse.next();
  if (isAuthorized(request.headers.get('authorization'), password)) {
    return NextResponse.next();
  }
  return new NextResponse('Autenticación requerida', {
    status: 401,
    headers: { 'WWW-Authenticate': 'Basic realm="FragForge", charset="UTF-8"' },
  });
}

/** True when the Authorization header is Basic and its password matches. */
function isAuthorized(header: string | null, password: string): boolean {
  if (!header?.startsWith('Basic ')) return false;
  let decoded: string;
  try {
    decoded = atob(header.slice('Basic '.length).trim());
  } catch {
    return false;
  }
  const colon = decoded.indexOf(':');
  if (colon < 0) return false;
  return timingSafeEqual(decoded.slice(colon + 1), password);
}

/**
 * Constant-time string comparison (the middleware runs on the Edge runtime,
 * which has no node:crypto timingSafeEqual).
 */
function timingSafeEqual(a: string, b: string): boolean {
  const enc = new TextEncoder();
  const ab = enc.encode(a);
  const bb = enc.encode(b);
  let diff = ab.length ^ bb.length;
  const len = Math.max(ab.length, bb.length);
  for (let i = 0; i < len; i++) {
    diff |= (ab[i % (ab.length || 1)] ?? 0) ^ (bb[i % (bb.length || 1)] ?? 0);
  }
  return diff === 0;
}
