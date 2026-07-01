import { NextResponse } from 'next/server';
import { supabaseAdmin } from '@/lib/supabase/server';
import { serviceUnavailable } from '../../_lib';
import { isLocalMode } from '@/lib/mode';
import { localStatus } from '../../_local';

export const runtime = 'nodejs';

/**
 * GET /api/demos/{jobId}/status — Supabase-backed job status for the cloud
 * scan flow. Intentionally unauthenticated: the jobId is an unguessable demo
 * UUID, which is the Phase 1 trust boundary (see task-13 brief). Reads the
 * newest job row for this demo, then reports whether the owning user has a
 * paired agent that heartbeat within the last minute, so the UI can tell a
 * slow scan apart from a PC that is simply offline.
 */
export async function GET(_request: Request, { params }: { params: Promise<{ jobId: string }> }): Promise<Response> {
  const { jobId } = await params;

  // Local studio: proxy the job status straight from the local orchestrator.
  if (isLocalMode()) return localStatus(jobId);

  // Keep the /api/demos/* contract: a Supabase outage or misconfig must surface
  // as the {code: service_unavailable} 503 shape, not a bare code-less 500 the
  // UI would misread as a bad demo.
  try {
    const db = supabaseAdmin();

    const { data: job } = await db
      .from('jobs')
      .select('state, error, user_id')
      .eq('demo_id', jobId)
      .order('created_at', { ascending: false })
      .limit(1)
      .maybeSingle();
    if (!job) return NextResponse.json({ status: 'unknown' }, { status: 404 });

    const { data: agents } = await db
      .from('agents')
      .select('last_heartbeat_at')
      .eq('user_id', job.user_id)
      .not('name', 'like', 'pending:%');
    const online = (agents ?? []).some(
      (a) => a.last_heartbeat_at && Date.now() - new Date(a.last_heartbeat_at).getTime() < 60_000,
    );

    const body: { status: string; failure_reason?: string; online: boolean } = { status: job.state, online };
    if (job.error) body.failure_reason = job.error;
    return NextResponse.json(body);
  } catch (err) {
    console.error('status lookup failed', err);
    return serviceUnavailable();
  }
}
