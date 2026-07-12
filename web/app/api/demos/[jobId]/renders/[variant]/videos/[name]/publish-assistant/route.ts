import { NextResponse } from 'next/server';
import {
  callOrchestrator,
  forwardError,
  jobUrl,
  serviceUnavailable,
} from '../../../../../../_lib';
import { isCrossSitePublishAssistantRequest } from '@/lib/api/publish-assistant';

export const runtime = 'nodejs';

const DEFAULT_DAYS = 7;
const MAX_DAYS = 14;
const VARIANT_RE = /^[A-Za-z0-9][A-Za-z0-9_-]*$/;
const NAME_RE = /^[A-Za-z0-9._-]+$/;

type RouteContext = {
  params: Promise<{ jobId: string; variant: string; name: string }>;
};

function requestedDays(request: Request): number | null {
  const raw = new URL(request.url).searchParams.get('days');
  if (raw === null) return DEFAULT_DAYS;
  const days = Number(raw);
  return Number.isInteger(days) && days >= 1 && days <= MAX_DAYS ? days : null;
}

/** GET the metadata and deterministic schedule for manually publishing one rendered MP4. */
export async function GET(request: Request, { params }: RouteContext): Promise<Response> {
  if (isCrossSitePublishAssistantRequest(request)) {
    return NextResponse.json({ error: 'cross-site request blocked' }, { status: 403 });
  }
  const days = requestedDays(request);
  if (days === null) {
    return NextResponse.json({ error: `days must be between 1 and ${MAX_DAYS}` }, { status: 400 });
  }
  const { jobId, variant, name } = await params;
  if (!VARIANT_RE.test(variant) || !NAME_RE.test(name)) {
    return NextResponse.json({ error: 'invalid path' }, { status: 400 });
  }
  const upstream = jobUrl(jobId, `/renders/${variant}/videos/${name}/publish-assistant?days=${days}`);
  if (!upstream) return NextResponse.json({ error: 'invalid job id' }, { status: 400 });
  const response = await callOrchestrator(upstream, { cache: 'no-store' });
  if (response === null) return serviceUnavailable();
  if (!response.ok) return forwardError(response);
  return NextResponse.json((await response.json()) as unknown, {
    status: response.status,
    headers: { 'cache-control': 'no-store' },
  });
}
