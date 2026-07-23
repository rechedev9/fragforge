import { NextResponse } from 'next/server';
import { localAPIRequestError } from '@/lib/api/local-request-guard';
import { prepareLocalUploadBody, readBoundedText } from '@/lib/api/bounded-request-body';
import {
  orchestratorUrl,
  callOrchestrator,
  mutationHeaders,
  forwardError,
  serviceUnavailable,
  callOrchestratorStreamingUpload,
  UPLOAD_BODY_LIMIT_EXCEEDED,
} from './_lib';

export const runtime = 'nodejs';

// Generous cap for an uploaded MP4 (facecam + gameplay VOD clip), well above a
// typical Twitch clip/short VOD export.
const MAX_UPLOAD_BYTES = 2 * 1024 * 1024 * 1024;

/**
 * POST /api/streams — create a Stream Clips job, either from a Twitch clip/VOD
 * URL (`application/json` `{ source_url, title? }`) or an uploaded MP4
 * (`multipart/form-data`, field `video`, optional `config` JSON). The browser
 * uses the upstream field names so this route can stream the multipart body
 * unchanged instead of buffering and rebuilding it. Mirrors the
 * /api/demos/* contract: a 503 `{code: service_unavailable}` when the
 * orchestrator is unreachable, and the orchestrator's own 409 (for example, yt-dlp
 * unconfigured) passes through with its message so the UI can surface it.
 */
export async function POST(request: Request): Promise<Response> {
  const localError = await localAPIRequestError(request.headers, request.method);
  if (localError !== undefined) return NextResponse.json({ error: localError }, { status: 403 });

  const contentType = request.headers.get('content-type') ?? '';
  const url = `${orchestratorUrl()}/api/stream-jobs`;

  let init: RequestInit;
  if (contentType.includes('multipart/form-data')) {
    const upload = await prepareLocalUploadBody(request, MAX_UPLOAD_BYTES);
    if (!upload.ok) return NextResponse.json({ error: upload.error }, { status: upload.status });

    const headers: Record<string, string> = { 'Content-Type': contentType };
    if (upload.contentLength !== undefined) headers['Content-Length'] = upload.contentLength;
    const res = await callOrchestratorStreamingUpload(url, {
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
    return NextResponse.json((await res.json()) as unknown, { status: res.status });
  } else {
    const incoming = await readBoundedText(request);
    if (!incoming.ok) return NextResponse.json({ error: incoming.error }, { status: incoming.status });
    let body: { source_url?: unknown; title?: unknown };
    try {
      body = JSON.parse(incoming.text || '{}') as { source_url?: unknown; title?: unknown };
    } catch {
      return NextResponse.json({ error: 'invalid json body' }, { status: 400 });
    }
    if (typeof body.source_url !== 'string' || !body.source_url) {
      return NextResponse.json({ error: 'source_url is required' }, { status: 400 });
    }
    const payload: { source_url: string; title?: string } = { source_url: body.source_url };
    if (typeof body.title === 'string' && body.title) payload.title = body.title;
    init = {
      method: 'POST',
      headers: { ...mutationHeaders(), 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    };
  }

  const res = await callOrchestrator(url, init);
  if (res === null) return serviceUnavailable();
  if (!res.ok) return forwardError(res);

  return NextResponse.json((await res.json()) as unknown, { status: res.status });
}

/** GET /api/streams — list stream-clip jobs. */
export async function GET(request: Request): Promise<Response> {
  const localError = await localAPIRequestError(request.headers, request.method);
  if (localError !== undefined) return NextResponse.json({ error: localError }, { status: 403 });

  const res = await callOrchestrator(`${orchestratorUrl()}/api/stream-jobs`);
  if (res === null) return serviceUnavailable();
  if (!res.ok) return forwardError(res);

  return NextResponse.json((await res.json()) as unknown);
}
