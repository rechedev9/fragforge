import { NextResponse } from 'next/server';
import { orchestratorUrl, callOrchestrator, forwardError, serviceUnavailable } from '../demos/_lib';

export const runtime = 'nodejs';

/**
 * GET /api/presets — proxy the orchestrator's render-preset registry so the UI
 * lists the user-selectable reel presets (Kill Feed / Clean POV / Full HUD)
 * instead of hardcoding them. The preset name doubles as the render variant.
 */
export async function GET(): Promise<Response> {
  const res = await callOrchestrator(`${orchestratorUrl()}/api/presets`, { cache: 'no-store' });
  if (res === null) return serviceUnavailable();
  if (!res.ok) return forwardError(res);
  return NextResponse.json(await res.json());
}
