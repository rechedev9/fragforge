import type { SupabaseClient } from '@supabase/supabase-js';

/** Upsert the Steam user and return its internal user id. */
export async function ensureUser(
  steamId: string,
  persona: string,
  avatar: string,
  db?: SupabaseClient,
): Promise<string> {
  const client = db ?? (await import('@/lib/supabase/server')).supabaseAdmin();
  const { data, error } = await client
    .from('users')
    .upsert({ steam_id: steamId, persona, avatar }, { onConflict: 'steam_id' })
    .select('id')
    .single();
  if (error) throw new Error(`ensureUser: ${error.message}`);
  return data.id as string;
}
