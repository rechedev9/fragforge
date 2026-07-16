import { NextResponse } from 'next/server';
import { localAPIRequestError } from '@/lib/api/local-request-guard';
import { prepareLocalUploadBody } from '@/lib/api/bounded-request-body';
import {
  orchestratorUrl,
  forwardError,
  callOrchestrator,
  callOrchestratorStreamingUpload,
  serviceUnavailable,
  jobUrl,
  seriesJobsUrl,
  UPLOAD_BODY_LIMIT_EXCEEDED,
} from './_lib';

/**
 * Server-side `/api/demos/*` proxy handlers for the desktop-bundled local
 * orchestrator. The orchestrator owns the whole pipeline (scan, parse, HLAE/CS2
 * capture, render), while its URL and optional token stay out of the browser.
 */

// Matches the orchestrator's 500 MiB demo cap plus its 1 MiB allowance for
// multipart boundaries and headers.
const MAX_DEMO_REQUEST_BYTES = 501 * 1024 * 1024;

/**
 * POST /api/demos/scan (local) - accept a .dem upload and start a roster scan.
 * The orchestrator treats a job created with no target as a scan, so we forward
 * only the file under field name `demo`.
 */
export async function localScan(request: Request): Promise<Response> {
  const localError = localAPIRequestError(request.headers);
  if (localError !== undefined) return NextResponse.json({ error: localError }, { status: 403 });

  const contentType = request.headers.get('content-type') ?? '';
  if (!contentType.toLowerCase().startsWith('multipart/form-data;')) {
    return NextResponse.json({ error: 'multipart/form-data required' }, { status: 400 });
  }

  const upload = prepareLocalUploadBody(request, MAX_DEMO_REQUEST_BYTES);
  if (!upload.ok) return NextResponse.json({ error: upload.error }, { status: upload.status });

  const headers: Record<string, string> = { 'Content-Type': contentType };
  if (upload.contentLength !== undefined) headers['Content-Length'] = upload.contentLength;
  const res = await callOrchestratorStreamingUpload(`${orchestratorUrl()}/api/jobs`, {
    method: 'POST',
    headers,
    body: upload.body,
    duplex: 'half',
  }, upload.exceeded);
  if (res === UPLOAD_BODY_LIMIT_EXCEEDED) {
    return NextResponse.json({ error: 'file too large' }, { status: 413 });
  }
  if (res === null) return serviceUnavailable();
  if (!res.ok) return forwardError(res);

  const { id } = (await res.json()) as { id: string };
  return NextResponse.json({ jobId: id }, { status: 201 });
}

/** GET /api/demos/{jobId}/status (local) - proxy the job's current status. */
export async function localStatus(jobId: string): Promise<Response> {
  const url = jobUrl(jobId, '?view=status');
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

  // Forward only the known top-level keys, never the raw upstream object. The
  // real client's toRosterMatch reads match.{map,score_ct,score_t,rounds}, so
  // match must survive the proxy; it is omitted when the scan produced none.
  const body = (await res.json()) as { players: unknown[]; match?: unknown };
  const out: { players: unknown[]; match?: unknown } = { players: body.players };
  if (body.match !== undefined) out.match = body.match;
  return NextResponse.json(out);
}

/**
 * GET /api/demos/series/{seriesId} (local) - list the demos uploaded under one
 * bulk series (bo3/bo5), in upload order. Forwards only a whitelisted per-demo
 * shape, never the raw upstream job objects: failure_reason and demo_file_name
 * are present only when the orchestrator sends them.
 */
export async function localSeries(seriesId: string): Promise<Response> {
  const url = seriesJobsUrl(seriesId);
  if (!url) return NextResponse.json({ error: 'invalid series id' }, { status: 400 });

  const res = await callOrchestrator(url);
  if (res === null) return serviceUnavailable();
  if (!res.ok) return forwardError(res);

  type UpstreamJob = {
    id: string;
    status: string;
    failure_reason?: string;
    demo_file_name?: string;
  };
  const body = (await res.json()) as { jobs: UpstreamJob[] };
  const demos = body.jobs.map((job) => {
    const demo: { jobId: string; status: string; failureReason?: string; fileName?: string } = {
      jobId: job.id,
      status: job.status,
    };
    if (job.failure_reason) demo.failureReason = job.failure_reason;
    if (job.demo_file_name) demo.fileName = job.demo_file_name;
    return demo;
  });
  return NextResponse.json({ demos });
}
