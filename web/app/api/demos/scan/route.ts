import { NextResponse } from 'next/server';
import { cookies } from 'next/headers';
import { verifySession, SESSION_COOKIE } from '@/lib/auth/session';
import { ensureUser } from '@/lib/cloud/users';
import { createScanJob } from '@/lib/cloud/demos';
import { serviceUnavailable } from '../_lib';
import { isLocalMode } from '@/lib/mode';
import { localScan } from '../_local';

// Runs server-side so the Supabase service-role key stays off the client and
// the .dem bytes are uploaded straight to Storage without CORS.
export const runtime = 'nodejs';

// Caps how much of a .dem we hold in memory and store. Chosen to match the
// previous orchestrator-proxy cap so behavior does not regress.
const MAX_DEMO_BYTES = 500 * 1024 * 1024;

// CS2 (Source 2) demos start with "PBDEMS2"; legacy GOTV demos with "HL2DEMO".
// Mirrors the orchestrator's isDemoHeader so non-demo uploads are rejected at
// submit time instead of failing deep in the agent's parser minutes later.
function isDemoHeader(head: Uint8Array): boolean {
  const startsWith = (magic: string) =>
    head.length >= magic.length && [...magic].every((ch, i) => head[i] === ch.charCodeAt(0));
  return startsWith('PBDEMS2') || startsWith('HL2DEMO');
}

/**
 * POST /api/demos/scan — accept a .dem upload, store it in Supabase Storage,
 * and queue a scan job for the paired agent to claim.
 */
export async function POST(request: Request): Promise<Response> {
  // Local studio: proxy straight to the local orchestrator, no Supabase, no
  // session (the orchestrator on this machine is the trust boundary).
  if (isLocalMode()) return localScan(request);

  const jar = await cookies();
  const s = verifySession(jar.get(SESSION_COOKIE)?.value);
  if (!s) return NextResponse.json({ error: 'unauthorized' }, { status: 401 });

  // Fast-path reject only when a PRESENT, valid Content-Length already exceeds
  // the cap. A missing or non-numeric header is "unknown", not zero, so we do
  // not pre-reject on it; the real check happens on the parsed file size below.
  const cl = Number(request.headers.get('content-length'));
  if (Number.isFinite(cl) && cl > MAX_DEMO_BYTES) {
    return NextResponse.json({ error: 'file too large' }, { status: 413 });
  }

  const form = await request.formData();
  const file = form.get('file');
  if (!(file instanceof File)) return NextResponse.json({ error: 'missing file' }, { status: 400 });
  if (file.size > MAX_DEMO_BYTES) {
    return NextResponse.json({ error: 'file too large' }, { status: 413 });
  }

  try {
    const userId = await ensureUser(s.steamid64, s.persona, s.avatar);
    const bytes = await file.arrayBuffer();
    if (!isDemoHeader(new Uint8Array(bytes, 0, Math.min(8, bytes.byteLength)))) {
      return NextResponse.json({ error: 'uploaded file is not a CS2 demo' }, { status: 400 });
    }
    const { demoId } = await createScanJob(userId, { name: file.name, size: file.size, bytes });
    return NextResponse.json({ jobId: demoId }, { status: 201 });
  } catch (err) {
    console.error('scan create failed', err);
    return serviceUnavailable();
  }
}
