import { NextResponse } from 'next/server';
import { orchestratorUrl, mutationHeaders, forwardError, callOrchestrator, serviceUnavailable } from '../_lib';

// Runs server-side so the orchestrator URL + token stay off the client and the
// .dem bytes are proxied without CORS. Local-first: same machine as the parser.
export const runtime = 'nodejs';

// Matches the orchestrator's 500 MiB MaxBytesReader cap. The orchestrator stays
// the ultimate authority; this layer enforces the real size too so a chunked
// (Transfer-Encoding) upload with a bogus or absent Content-Length cannot slip a
// huge body past us before the orchestrator rejects it.
const MAX_DEMO_BYTES = 500 * 1024 * 1024;

/**
 * POST /api/demos/scan — accept a .dem upload and start a roster scan. The
 * orchestrator treats a job created with no `config` (no target) as a scan, so
 * we forward only the file under field name `demo`.
 */
export async function POST(request: Request): Promise<Response> {
  // Fast-path reject only when a PRESENT, valid Content-Length already exceeds
  // the cap. A missing or non-numeric header is "unknown", not zero, so we do
  // not pre-reject on it; the real check happens on the parsed file size below.
  const cl = Number(request.headers.get('content-length'));
  if (Number.isFinite(cl) && cl > MAX_DEMO_BYTES) {
    return NextResponse.json({ error: 'file too large' }, { status: 413 });
  }

  const incoming = await request.formData();
  const file = incoming.get('file');
  if (!(file instanceof File)) {
    return NextResponse.json({ error: 'no demo file' }, { status: 400 });
  }
  if (file.size > MAX_DEMO_BYTES) {
    return NextResponse.json({ error: 'file too large' }, { status: 413 });
  }

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
