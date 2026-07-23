import { localAPIRequestError } from '@/lib/api/local-request-guard';
import { orchestratorUrl, proxyStream } from '../../../demos/_lib';

const VOICE_PROFILE_ID = 'raizerinhocs2';

export async function GET(request: Request): Promise<Response> {
  const localError = await localAPIRequestError(request.headers, request.method);
  if (localError !== undefined) return Response.json({ error: localError }, { status: 403 });
  return proxyStream(
    `${orchestratorUrl()}/api/voice-profiles/${VOICE_PROFILE_ID}/audio`,
    'application/octet-stream',
    request,
  );
}
