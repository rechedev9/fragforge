import { localSeries } from '../../_local';

export const runtime = 'nodejs';

/**
 * GET /api/demos/series/{seriesId} — list the demos uploaded under one bulk
 * series (bo3/bo5) from the local desktop orchestrator through the same-origin
 * server boundary, so the upstream address and optional token stay server-side.
 */
export async function GET(_request: Request, { params }: { params: Promise<{ seriesId: string }> }): Promise<Response> {
  const { seriesId } = await params;
  return localSeries(seriesId);
}
