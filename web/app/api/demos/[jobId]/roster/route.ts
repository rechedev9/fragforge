import { NextResponse } from 'next/server';
import { isLocalMode } from '@/lib/mode';
import { localRoster } from '../../_local';

export const runtime = 'nodejs';

/**
 * GET /api/demos/{jobId}/roster — roster scan result proxy.
 *
 * Only local mode uses this: it proxies the roster from the local orchestrator.
 * In hosted mode the browser reads the roster from the local agent directly, so
 * any non-local request is a misconfiguration and gets a 404. The former
 * Supabase artifacts-download path was removed with the accounts move to
 * node:sqlite (job artifacts no longer live on our server).
 */
export async function GET(_request: Request, { params }: { params: Promise<{ jobId: string }> }): Promise<Response> {
  const { jobId } = await params;
  if (isLocalMode()) return localRoster(jobId);
  return NextResponse.json({ players: [] }, { status: 404 });
}
