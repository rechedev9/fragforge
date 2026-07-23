import { NextResponse } from 'next/server';
import { prepareLocalUploadBody } from '@/lib/api/bounded-request-body';
import { localAPIRequestError } from '@/lib/api/local-request-guard';
import {
  orchestratorUrl,
  callOrchestrator,
  forwardError,
  serviceUnavailable,
  callOrchestratorStreamingUpload,
  UPLOAD_BODY_LIMIT_EXCEEDED,
} from '../../demos/_lib';

export const runtime = 'nodejs';

const VOICE_PROFILE_ID = 'raizerinhocs2';
const MAX_VOICE_UPLOAD_BYTES = 26 * 1024 * 1024;
const FRONTEND_AUDIO_URL = '/api/news/voice-profile/audio';

function upstreamURL(): string {
  return `${orchestratorUrl()}/api/voice-profiles/${VOICE_PROFILE_ID}`;
}

async function frontendProfileResponse(res: Response): Promise<Response> {
  const body = (await res.json()) as unknown;
  if (body !== null && typeof body === 'object') {
    return NextResponse.json({ ...body, audio_url: FRONTEND_AUDIO_URL });
  }
  return NextResponse.json(body);
}

export async function GET(request: Request): Promise<Response> {
  const localError = await localAPIRequestError(request.headers, request.method);
  if (localError !== undefined) return NextResponse.json({ error: localError }, { status: 403 });

  const res = await callOrchestrator(upstreamURL());
  if (res === null) return serviceUnavailable();
  if (!res.ok) return forwardError(res);
  return frontendProfileResponse(res);
}

export async function PUT(request: Request): Promise<Response> {
  const localError = await localAPIRequestError(request.headers, request.method);
  if (localError !== undefined) return NextResponse.json({ error: localError }, { status: 403 });

  const contentType = request.headers.get('content-type') ?? '';
  if (!contentType.includes('multipart/form-data')) {
    return NextResponse.json({ error: 'multipart voice upload required' }, { status: 400 });
  }

  const upload = await prepareLocalUploadBody(request, MAX_VOICE_UPLOAD_BYTES);
  if (!upload.ok) return NextResponse.json({ error: upload.error }, { status: upload.status });

  const headers: Record<string, string> = { 'Content-Type': contentType };
  if (upload.contentLength !== undefined) headers['Content-Length'] = upload.contentLength;
  const res = await callOrchestratorStreamingUpload(upstreamURL(), {
    method: 'PUT',
    headers,
    body: upload.body,
    duplex: 'half',
  }, upload.exceeded);
  if (res === UPLOAD_BODY_LIMIT_EXCEEDED) {
    return NextResponse.json({ error: 'voice reference too large' }, { status: 413 });
  }
  if (res === null) return serviceUnavailable();
  if (!res.ok) return forwardError(res);
  return frontendProfileResponse(res);
}

export async function DELETE(request: Request): Promise<Response> {
  const localError = await localAPIRequestError(request.headers, request.method);
  if (localError !== undefined) return NextResponse.json({ error: localError }, { status: 403 });

  const res = await callOrchestrator(upstreamURL(), { method: 'DELETE' });
  if (res === null) return serviceUnavailable();
  if (!res.ok) return forwardError(res);
  return new Response(null, { status: 204 });
}
