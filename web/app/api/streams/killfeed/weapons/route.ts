import { NextResponse } from 'next/server';
import { orchestratorUrl, callOrchestrator, forwardError, serviceUnavailable } from '../../_lib';

export const runtime = 'nodejs';

/**
 * GET /api/streams/killfeed/weapons — proxy the kill-notice weapon catalog so
 * the editor's weapon <select> offers exactly the keys the renderer validates
 * against. Not job-scoped: the catalog is global.
 */
export async function GET(): Promise<Response> {
  const res = await callOrchestrator(`${orchestratorUrl()}/api/stream-killfeed/weapons`);
  if (res === null) return serviceUnavailable();
  if (!res.ok) return forwardError(res);

  return NextResponse.json((await res.json()) as unknown);
}
