import { NextResponse } from 'next/server';
import { orchestratorUrl, mutationHeaders, forwardError, callOrchestrator, serviceUnavailable, jobUrl } from './_lib';
import type { JobSummary } from '@/lib/api/types';

/**
 * Local-mode `/api/demos/*` handlers. In local mode the web is a thin proxy to a
 * local orchestrator (`zv serve`) on the same machine, which owns the whole
 * pipeline (scan, parse, record with HLAE/CS2, render). Cloud mode never reaches
 * these routes: the browser talks straight to the paired agent's loopback, so
 * the whole /api/demos/* surface is now a pure same-origin proxy. Everything runs
 * server-side so the orchestrator URL and token never reach the browser.
 */

// Matches the orchestrator's 500 MiB MaxBytesReader cap. Enforced here too so a
// chunked upload with a bogus/absent Content-Length cannot slip a huge body past
// us before the orchestrator rejects it.
const MAX_DEMO_BYTES = 500 * 1024 * 1024;

/**
 * POST /api/demos/scan (local) - accept a .dem upload and start a roster scan.
 * The orchestrator treats a job created with no target as a scan, so we forward
 * only the file under field name `demo`.
 */
export async function localScan(request: Request): Promise<Response> {
  // Fast-path reject only when a PRESENT, valid Content-Length already exceeds
  // the cap. A missing/non-numeric header is "unknown", not zero, so we do not
  // pre-reject on it; the real check is the parsed file size below.
  const cl = Number(request.headers.get('content-length'));
  if (Number.isFinite(cl) && cl > MAX_DEMO_BYTES) {
    return NextResponse.json({ error: 'file too large' }, { status: 413 });
  }

  const incoming = await request.formData();
  const file = incoming.get('file');
  if (!(file instanceof File)) return NextResponse.json({ error: 'missing file' }, { status: 400 });
  if (file.size > MAX_DEMO_BYTES) return NextResponse.json({ error: 'file too large' }, { status: 413 });

  const form = new FormData();
  form.append('demo', file, file.name);

  const res = await callOrchestrator(`${orchestratorUrl()}/api/jobs`, {
    method: 'POST',
    headers: mutationHeaders(),
    body: form,
  });
  if (res === null) return serviceUnavailable();
  if (!res.ok) return forwardError(res);

  const { id } = (await res.json()) as { id: string };
  return NextResponse.json({ jobId: id }, { status: 201 });
}

/** Default and bounds for the job-list page size, mirroring the orchestrator. */
const DEFAULT_LIST_LIMIT = 50;
const MIN_LIST_LIMIT = 1;
const MAX_LIST_LIMIT = 100;

/** The raw upstream job shape, of which we forward only the whitelisted scalars. */
type UpstreamJob = {
  id: string;
  status: string;
  failure_reason?: string;
  created_at: string;
  kill_plan?: {
    demo?: { map?: string };
    stats?: { total_kills_target?: number };
    segments?: unknown[];
  };
};

/**
 * Projects one raw orchestrator job onto the flat, whitelisted JobSummary. The
 * plan-derived fields (map / target_kills / segment_count) exist only once the
 * job has a kill plan, so they are set only when present; the raw kill_plan,
 * rules, demo_path, demo_sha256, and target_steamid are never forwarded.
 */
function toJobSummary(job: UpstreamJob): JobSummary {
  const summary: JobSummary = { id: job.id, status: job.status, created_at: job.created_at };
  if (job.failure_reason) summary.failure_reason = job.failure_reason;
  const plan = job.kill_plan;
  if (plan) {
    if (typeof plan.demo?.map === 'string') summary.map = plan.demo.map;
    if (typeof plan.stats?.total_kills_target === 'number') summary.target_kills = plan.stats.total_kills_target;
    if (Array.isArray(plan.segments)) summary.segment_count = plan.segments.length;
  }
  return summary;
}

/**
 * GET /api/demos (local) - list the orchestrator's jobs so the desktop UI can
 * enumerate server-side work (matches page poll, reel-library synthesis). Only a
 * whitelisted per-job projection is forwarded (see toJobSummary), never the raw
 * job objects, so the polled payload stays small. `limit` is clamped to the
 * orchestrator's 1-100 range so an out-of-range query never 400s here.
 */
export async function localListJobs(request: Request): Promise<Response> {
  const raw = Number(new URL(request.url).searchParams.get('limit'));
  const limit = Number.isFinite(raw) && raw > 0
    ? Math.min(MAX_LIST_LIMIT, Math.max(MIN_LIST_LIMIT, Math.trunc(raw)))
    : DEFAULT_LIST_LIMIT;

  const res = await callOrchestrator(`${orchestratorUrl()}/api/jobs?limit=${limit}`);
  if (res === null) return serviceUnavailable();
  if (!res.ok) return forwardError(res);

  const body = (await res.json()) as { jobs?: UpstreamJob[] };
  const jobs = (body.jobs ?? []).map(toJobSummary);
  return NextResponse.json({ jobs });
}

/** GET /api/demos/{jobId}/status (local) - proxy the job's current status. */
export async function localStatus(jobId: string): Promise<Response> {
  const url = jobUrl(jobId);
  if (!url) return NextResponse.json({ error: 'invalid job id' }, { status: 400 });

  const res = await callOrchestrator(url);
  if (res === null) return serviceUnavailable();
  if (!res.ok) return forwardError(res);

  // Forward only the known fields, never the raw upstream object. failure_reason
  // is omitted by the orchestrator unless the job failed; progress is present
  // only while capturing (segments done/total) so the library card can show a
  // real percent. Both are forwarded only when the orchestrator sends them.
  type CaptureProgress = { done: number; total: number };
  const data = (await res.json()) as { status: string; failure_reason?: string; progress?: CaptureProgress };
  const body: { status: string; failure_reason?: string; progress?: CaptureProgress } = { status: data.status };
  if (data.failure_reason) body.failure_reason = data.failure_reason;
  const p = data.progress;
  if (p && typeof p.done === 'number' && typeof p.total === 'number' && p.total > 0) {
    body.progress = { done: p.done, total: p.total };
  }
  return NextResponse.json(body);
}

/**
 * GET /api/demos/{jobId}/roster (local) - proxy the roster scan result. The
 * orchestrator already wraps it as { players: [...] } with steamid64 keys; the
 * client maps steamid64 → steamId, so this is a pass-through.
 */
export async function localRoster(jobId: string): Promise<Response> {
  const url = jobUrl(jobId, '/roster');
  if (!url) return NextResponse.json({ error: 'invalid job id' }, { status: 400 });

  const res = await callOrchestrator(url);
  if (res === null) return serviceUnavailable();
  if (!res.ok) return forwardError(res);

  const body = (await res.json()) as { players: unknown[] };
  return NextResponse.json({ players: body.players });
}
