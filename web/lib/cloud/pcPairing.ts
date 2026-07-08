// Pairing logic behind POST /api/pc/pair, kept out of the route so it is unit
// testable against a fake Supabase client (like pcStatus.ts). Relative .ts
// imports (not the `@/` alias) so Node's native TS loader resolves them under
// `node --test`.
import crypto from 'node:crypto';
import type { SupabaseClient } from '@supabase/supabase-js';
import { guestSession, signSession, type SessionPayload } from '../auth/session.ts';
import { ensureUser } from './users.ts';
import { hashToken } from './agentAuth.ts';

// Unambiguous alphabet (no 0/O/1/I) for a short, human-typeable pairing code.
const PAIR_ALPHABET = 'ABCDEFGHJKMNPQRSTUVWXYZ23456789';
const PAIR_TTL_MS = 10 * 60 * 1000;

/** A short, human-typeable one-time pairing code (no ambiguous chars). */
export function pairingCode(): string {
  const bytes = crypto.randomBytes(8);
  return Array.from(bytes, (b) => PAIR_ALPHABET[b % PAIR_ALPHABET.length]).join('');
}

export type PairOutcome = {
  pairingCode: string;
  // When set, the caller must write this signed session cookie: a guest session
  // was just minted because the browser had none. Null when an existing session
  // (guest or Steam) drove the pairing.
  sessionCookie: { token: string } | null;
};

/**
 * Mints a one-time pairing code for the desktop agent to redeem. When there is
 * no session, a guest session is minted so uploading/pairing never requires a
 * Steam login; the caller sets the returned cookie. Returns null if the DB
 * insert fails (nothing to pair against).
 */
export async function issuePairingCode(
  session: SessionPayload | null,
  db: SupabaseClient,
): Promise<PairOutcome | null> {
  let active = session;
  let sessionCookie: { token: string } | null = null;
  if (!active) {
    active = guestSession();
    sessionCookie = { token: signSession(active) };
  }

  const userId = await ensureUser(active.steamid64, active.persona, active.avatar, db);
  const code = pairingCode();
  // Store the code hashed with a TTL encoded in the name field.
  const expires = Date.now() + PAIR_TTL_MS;
  const { error } = await db.from('agents').insert({
    user_id: userId,
    name: `pending:${expires}`,
    token_hash: hashToken(`code:${code}`),
  });
  if (error) return null;
  return { pairingCode: code, sessionCookie };
}

/**
 * Reassigns a guest user's agents to a Steam user after login, so a PC paired
 * as a guest stays paired once the same browser signs in with Steam. The leftover
 * guest users row is harmless and left in place.
 */
export async function migrateGuestAgents(
  fromUserId: string,
  toUserId: string,
  db: SupabaseClient,
): Promise<void> {
  const { error } = await db.from('agents').update({ user_id: toUserId }).eq('user_id', fromUserId);
  if (error) throw new Error(`migrateGuestAgents: ${error.message}`);
}
