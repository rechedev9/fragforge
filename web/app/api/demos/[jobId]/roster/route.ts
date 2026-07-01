import { NextResponse } from 'next/server';
import { supabaseAdmin } from '@/lib/supabase/server';
import { serviceUnavailable } from '../../_lib';

export const runtime = 'nodejs';

/**
 * GET /api/demos/{jobId}/roster — reads the agent-uploaded roster scan result
 * straight from the `artifacts` bucket (`jobs/{jobId}/roster.json`). Missing
 * object, download error, or a truncated/corrupt payload all resolve to an
 * empty roster rather than a 500: the upload page treats an empty list the
 * same as "still scanning" and keeps polling status. A Supabase outage/misconfig
 * (the client itself throwing) instead surfaces the {code: service_unavailable}
 * 503 shape, per the /api/demos/* contract.
 */
export async function GET(_request: Request, { params }: { params: Promise<{ jobId: string }> }): Promise<Response> {
  const { jobId } = await params;
  const path = `jobs/${jobId}/roster.json`;

  try {
    const { data, error } = await supabaseAdmin().storage.from('artifacts').download(path);
    if (error || !data) return NextResponse.json({ players: [] });

    try {
      const json = JSON.parse(await data.text()) as { players?: unknown[] };
      return NextResponse.json({ players: json.players ?? [] });
    } catch {
      return NextResponse.json({ players: [] });
    }
  } catch (err) {
    console.error('roster lookup failed', err);
    return serviceUnavailable();
  }
}
