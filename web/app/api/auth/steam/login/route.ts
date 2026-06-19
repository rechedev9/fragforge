import { NextResponse } from 'next/server';
import { buildLoginUrl } from '@/lib/auth/steam';

export const runtime = 'nodejs';

/**
 * GET /api/auth/steam/login — kick off Steam OpenID. Redirects the browser to
 * Steam; on success Steam redirects back to /api/auth/steam/callback. Works on
 * localhost because the redirect happens in the user's browser.
 */
export async function GET(request: Request): Promise<Response> {
  const origin = new URL(request.url).origin;
  return NextResponse.redirect(buildLoginUrl(`${origin}/api/auth/steam/callback`, `${origin}/`));
}
