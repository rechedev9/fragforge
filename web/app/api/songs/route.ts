import { NextResponse } from 'next/server';
import { orchestratorUrl, forwardError } from '../demos/_lib';

export const runtime = 'nodejs';

/**
 * GET /api/songs — proxy the orchestrator's music catalog (the curated
 * open-source tracks under ZV_MUSIC_DIR) so the song picker can list and preview
 * real soundtracks.
 */
export async function GET(): Promise<Response> {
  const res = await fetch(`${orchestratorUrl()}/api/songs`, { cache: 'no-store' });
  if (!res.ok) return forwardError(res);
  return NextResponse.json(await res.json());
}
