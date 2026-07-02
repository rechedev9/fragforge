import { NextResponse } from 'next/server';
import { orchestratorUrl, callOrchestrator, mutationHeaders, forwardError, serviceUnavailable } from './_lib';

export const runtime = 'nodejs';

// Generous cap for an uploaded MP4 (facecam + gameplay VOD clip), well above a
// typical Twitch clip/short VOD export.
const MAX_UPLOAD_BYTES = 2 * 1024 * 1024 * 1024;

/**
 * POST /api/streams — create a Stream Clips job, either from a Twitch clip/VOD
 * URL (`application/json` `{ source_url, title? }`) or an uploaded MP4
 * (`multipart/form-data`, field `file`, optional field `title`). The
 * browser-facing field is `file`, but the request forwarded upstream to the
 * orchestrator uses field `video`, matching POST /api/stream-jobs's
 * r.FormFile("video") (internal/httpapi/stream_handlers.go) — the two field
 * names are an intentional proxy-boundary rename, not a mismatch. Mirrors the
 * /api/demos/* contract: a 503 `{code: service_unavailable}` when the
 * orchestrator is unreachable, and the orchestrator's own 409 (yt-dlp/whisper
 * unconfigured) passes through with its message so the UI can surface it.
 */
export async function POST(request: Request): Promise<Response> {
  const contentType = request.headers.get('content-type') ?? '';
  const url = `${orchestratorUrl()}/api/stream-jobs`;

  let init: RequestInit;
  if (contentType.includes('multipart/form-data')) {
    // Fast-path reject only when a PRESENT, valid Content-Length already
    // exceeds the cap; a missing/non-numeric header is "unknown", not zero.
    const cl = Number(request.headers.get('content-length'));
    if (Number.isFinite(cl) && cl > MAX_UPLOAD_BYTES) {
      return NextResponse.json({ error: 'file too large' }, { status: 413 });
    }

    const incoming = await request.formData();
    const file = incoming.get('file');
    if (!(file instanceof File)) return NextResponse.json({ error: 'missing file' }, { status: 400 });
    if (file.size > MAX_UPLOAD_BYTES) return NextResponse.json({ error: 'file too large' }, { status: 413 });

    // Proxy-facing field is "file" (our own contract with the browser client,
    // see lib/api/streams.ts createFromFile); upstream the orchestrator's
    // POST /api/stream-jobs handler reads r.FormFile("video")
    // (internal/httpapi/stream_handlers.go), so the field must be renamed here.
    const form = new FormData();
    form.append('video', file, file.name);
    const title = incoming.get('title');
    if (typeof title === 'string' && title) form.append('title', title);

    init = { method: 'POST', headers: mutationHeaders(), body: form };
  } else {
    const bodyText = await request.text();
    let body: { source_url?: unknown; title?: unknown };
    try {
      body = JSON.parse(bodyText || '{}') as { source_url?: unknown; title?: unknown };
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
export async function GET(): Promise<Response> {
  const res = await callOrchestrator(`${orchestratorUrl()}/api/stream-jobs`);
  if (res === null) return serviceUnavailable();
  if (!res.ok) return forwardError(res);

  return NextResponse.json((await res.json()) as unknown);
}
