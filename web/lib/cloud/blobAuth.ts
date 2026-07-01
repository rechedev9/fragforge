import type { SupabaseClient } from '@supabase/supabase-js';

/**
 * Map an already-authorized blob key to its private bucket and object path.
 * `demos/` keys live in the demos bucket (prefix stripped); every other key is
 * an artifacts key used verbatim. Callers MUST authorize with agentOwnsKey
 * first — this does no validation, it only routes.
 */
export function blobLocation(key: string): { bucket: string; path: string } {
  return key.startsWith('demos/')
    ? { bucket: 'demos', path: key.slice('demos/'.length) }
    : { bucket: 'artifacts', path: key };
}

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

  // The key is used verbatim as a Storage object path, so reject empty, '.' and
  // '..' segments before any ownership check: `jobs/{ownedDemo}/../otherDemo/x`
  // would otherwise pass the demoId check yet resolve outside the owned prefix.
  const segments = key.split('/');
  if (segments.some((s) => s === '' || s === '.' || s === '..')) return false;

  if (key.startsWith('demos/')) {
    return key.startsWith(`demos/${userId}/`);
  }

  if (segments[0] !== 'jobs' || segments.length < 3) return false;
  const demoId = segments[1];

  const client = db ?? (await import('@/lib/supabase/server')).supabaseAdmin();
  const { data, error } = await client.from('demos').select('user_id').eq('id', demoId).maybeSingle();
  if (error || !data) return false;
  return data.user_id === userId;
}
