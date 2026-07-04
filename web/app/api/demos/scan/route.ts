import { NextResponse } from 'next/server';
import { isLocalMode } from '@/lib/mode';
import { localScan } from '../_local';

// Runs server-side so the local orchestrator proxy stays a nodejs route.
export const runtime = 'nodejs';

/**
 * POST /api/demos/scan — accept a .dem upload and queue a scan.
 *
 * Only local mode uses this proxy: it forwards straight to the local
 * orchestrator on this machine (the trust boundary). In hosted mode the browser
 * talks DIRECTLY to the local agent's native /api/jobs route and never hits this
 * proxy, so any non-local request is a misconfiguration and gets a 404. The
 * former Supabase cloud path (session verify + ensureUser + createScanJob) was
 * removed with the accounts move to node:sqlite.
 */
export async function POST(request: Request): Promise<Response> {
  if (isLocalMode()) return localScan(request);
  return NextResponse.json({ error: 'not found' }, { status: 404 });
}
