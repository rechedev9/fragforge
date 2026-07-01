import type { SupabaseClient } from '@supabase/supabase-js';

/**
 * Authorize a blob key against the caller's user id.
 *
 * `demos/` keys are scoped by a pure prefix check (`demos/{userId}/...`), so a
 * missing `db` never triggers the lazy Supabase import for that branch.
 * Every other key is treated as a `jobs/{demoId}/...` artifact key and is
 * authorized by looking up the owning demo's `user_id`.
 */
export async function agentOwnsKey(key: string, userId: string, db?: SupabaseClient): Promise<boolean> {
  if (!key || key.trim().length === 0) return false;

  if (key.startsWith('demos/')) {
    return key.startsWith(`demos/${userId}/`);
  }

  const parts = key.split('/');
  if (parts.length < 2 || parts[0] !== 'jobs' || parts[1].length === 0) return false;
  const demoId = parts[1];

  const client = db ?? (await import('@/lib/supabase/server')).supabaseAdmin();
  const { data, error } = await client.from('demos').select('user_id').eq('id', demoId).maybeSingle();
  if (error || !data) return false;
  return data.user_id === userId;
}
