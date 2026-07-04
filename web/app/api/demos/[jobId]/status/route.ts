import { NextResponse } from 'next/server';
import { isLocalMode } from '@/lib/mode';
import { localStatus } from '../../_local';

export const runtime = 'nodejs';

/**
 * GET /api/demos/{jobId}/status — job status proxy.
 *
 * Only local mode uses this: it proxies the job status straight from the local
 * orchestrator. In hosted mode the browser polls the local agent's native
 * /api/jobs/{id} route directly, so any non-local request is a misconfiguration
 * and gets a 404. The former Supabase cloud path was removed with the accounts
 * move to node:sqlite (jobs no longer live on our server).
 */
export async function GET(_request: Request, { params }: { params: Promise<{ jobId: string }> }): Promise<Response> {
  const { jobId } = await params;
  if (isLocalMode()) return localStatus(jobId);
  return NextResponse.json({ status: 'unknown' }, { status: 404 });
}
