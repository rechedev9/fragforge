import { NextResponse } from 'next/server';
import { orchestratorUrl, mutationHeaders, forwardError } from '../_lib';

// Runs server-side so the orchestrator URL + token stay off the client and the
// .dem bytes are proxied without CORS. Local-first: same machine as the parser.
export const runtime = 'nodejs';

/**
 * POST /api/demos/scan — accept a .dem upload and start a roster scan. The
 * orchestrator treats a job created with no `config` (no target) as a scan, so
 * we forward only the file under field name `demo`.
 */
export async function POST(request: Request): Promise<Response> {
  const cl = Number(request.headers.get('content-length') ?? 0);
  if (cl > 530 * 1024 * 1024) {
    return NextResponse.json({ error: 'file too large' }, { status: 413 });
  }

  const incoming = await request.formData();
  const file = incoming.get('file');
  if (!(file instanceof File)) {
    return NextResponse.json({ error: 'no demo file' }, { status: 400 });
  }

  const form = new FormData();
  form.append('demo', file, file.name);

  const res = await fetch(`${orchestratorUrl()}/api/jobs`, {
    method: 'POST',
    headers: mutationHeaders(),
    body: form,
  });
  if (!res.ok) return forwardError(res);

  const { id } = (await res.json()) as { id: string };
  return NextResponse.json({ jobId: id }, { status: 201 });
}
