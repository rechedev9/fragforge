import type { SupabaseClient } from '@supabase/supabase-js';
import { supabaseAdmin } from '../supabase/server.ts';

/** Upsert the Steam user and return its internal user id. */
export async function ensureUser(
  steamId: string,
  persona: string,
  avatar: string,
  db: SupabaseClient = supabaseAdmin(),
): Promise<string> {
  const { data, error } = await db
    .from('users')
    .upsert({ steam_id: steamId, persona, avatar }, { onConflict: 'steam_id' })
    .select('id')
    .single();
  if (error) throw new Error(`ensureUser: ${error.message}`);
  return data.id as string;
}
