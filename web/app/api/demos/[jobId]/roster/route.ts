import { NextResponse } from 'next/server';
import { supabaseAdmin } from '@/lib/supabase/server';

export const runtime = 'nodejs';

/**
 * GET /api/demos/{jobId}/roster — reads the agent-uploaded roster scan result
 * straight from the `artifacts` bucket (`jobs/{jobId}/roster.json`). Missing
 * object, download error, or a truncated/corrupt payload all resolve to an
 * empty roster rather than a 500: the upload page treats an empty list the
 * same as "still scanning" and keeps polling status.
 */
export async function GET(_request: Request, { params }: { params: Promise<{ jobId: string }> }): Promise<Response> {
  const { jobId } = await params;
  const path = `jobs/${jobId}/roster.json`;

  const { data, error } = await supabaseAdmin().storage.from('artifacts').download(path);
  if (error || !data) return NextResponse.json({ players: [] });

  try {
    const json = JSON.parse(await data.text()) as { players?: unknown[] };
    return NextResponse.json({ players: json.players ?? [] });
  } catch {
    return NextResponse.json({ players: [] });
  }
}
