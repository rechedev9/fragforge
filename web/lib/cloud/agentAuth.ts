import { createHash, randomBytes } from 'crypto';

export function hashToken(raw: string): string {
  return createHash('sha256').update(raw).digest('hex');
}

export function newToken(): string {
  return randomBytes(32).toString('hex');
}

/** Resolve the agent from a Bearer token, or null if missing/unknown. */
export async function resolveAgent(req: Request): Promise<{ agentId: string; userId: string } | null> {
  const auth = req.headers.get('authorization') ?? '';
  const raw = auth.startsWith('Bearer ') ? auth.slice(7) : '';
  if (!raw) return null;
  const { supabaseAdmin } = await import('@/lib/supabase/server');
  const { data, error } = await supabaseAdmin()
    .from('agents')
    .select('id, user_id')
    .eq('token_hash', hashToken(raw))
    .maybeSingle();
  if (error || !data) return null;
  return { agentId: data.id as string, userId: data.user_id as string };
}
