import { NextResponse } from 'next/server';
import { orchestratorUrl, callOrchestrator, serviceUnavailable, forwardError } from '../demos/_lib';

export const runtime = 'nodejs';

/**
 * GET /api/capabilities — proxy the local orchestrator's capture-readiness
 * snapshot (which media workers are enabled and each tool's configured/accessible
 * state). Stateless and machine-level, so it follows the same callOrchestrator +
 * serviceUnavailable pattern as the job routes; only known fields are forwarded.
 */
export async function GET(): Promise<Response> {
  const res = await callOrchestrator(`${orchestratorUrl()}/api/capabilities`);
  if (res === null) return serviceUnavailable();
  if (!res.ok) return forwardError(res);

  const data = (await res.json()) as {
    record?: { enabled?: boolean; tools?: unknown[] };
    render?: { enabled?: boolean; tools?: unknown[] };
    compose?: { enabled?: boolean };
  };
  return NextResponse.json(
    {
      record: { enabled: Boolean(data.record?.enabled), tools: data.record?.tools ?? [] },
      render: { enabled: Boolean(data.render?.enabled), tools: data.render?.tools ?? [] },
      compose: { enabled: Boolean(data.compose?.enabled) },
    },
    { headers: { 'cache-control': 'no-store' } },
  );
}
