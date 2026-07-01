import { NextResponse } from 'next/server';
import { supabaseAdmin } from '@/lib/supabase/server';
import { hashToken, newToken } from '@/lib/cloud/agentAuth';

export const runtime = 'nodejs';

/**
 * POST /api/agent/pair — the desktop agent redeems a one-time pairing code
 * (minted by /api/pc/pair) for a durable bearer token. The pending record is
 * looked up by its hashed code and a `pending:<expiresMillis>` name; on
 * success it is rekeyed in place to the agent's real token hash and name.
 */
export async function POST(request: Request): Promise<Response> {
  const body = (await request.json().catch(() => null)) as { code?: string; name?: string } | null;
  const code = body?.code?.trim();
  if (!code) return NextResponse.json({ error: 'missing code' }, { status: 400 });

  const db = supabaseAdmin();
  const { data: pending } = await db
    .from('agents')
    .select('id, name')
    .eq('token_hash', hashToken(`code:${code}`))
    .like('name', 'pending:%')
    .maybeSingle();
  if (!pending) return NextResponse.json({ error: 'invalid code' }, { status: 404 });

  const expires = Number(pending.name.split(':')[1] ?? '0');
  if (Date.now() > expires) {
    await db.from('agents').delete().eq('id', pending.id);
    return NextResponse.json({ error: 'code expired' }, { status: 410 });
  }

  const token = newToken();
  const { error } = await db
    .from('agents')
    .update({ token_hash: hashToken(token), name: body?.name?.slice(0, 64) || 'PC' })
    .eq('id', pending.id);
  if (error) return NextResponse.json({ error: 'pairing failed' }, { status: 500 });
  return NextResponse.json({ token, agentId: pending.id });
}
