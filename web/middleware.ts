import { NextResponse, type NextRequest } from 'next/server';
import { localAPIRequestError } from '@/lib/api/local-request-guard';

/** Rejects cross-origin and DNS-rebound access to every local API endpoint. */
export function middleware(request: NextRequest): NextResponse {
  const error = localAPIRequestError(request.headers);
  if (error === undefined) return NextResponse.next();
  return NextResponse.json({ error }, { status: 403 });
}

export const config = {
  // Large uploads validate the same guard inside their route handler before
  // reading the body. Keeping them out of middleware avoids Next cloning and
  // buffering a multi-gigabyte request before the streaming proxy can cap it.
  matcher: '/api/((?!demos/scan/?$|streams/?$).*)',
};
