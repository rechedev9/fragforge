import type { DatabaseSync } from 'node:sqlite';

// SQLite-backed accounts store, replacing the former Supabase users table
// (lib/cloud/users.ts). Only the Steam login upsert survives the move to
// Topology A: jobs/demos/blobs now live on the LOCAL agent, not our server.
//
// Note: this module does NOT statically import lib/db/sqlite.ts (which is
// `server-only`). The real handle is pulled in lazily so unit tests can inject
// an in-memory DatabaseSync without tripping the server-only guard. The
// `node:sqlite` import above is type-only and never runs at runtime.

function nowIso(): string {
  return new Date().toISOString();
}

/**
 * Upsert the Steam user and return its internal user id as a string.
 *
 * First login inserts a row (created_at + last_login_at set to now). A repeat
 * login on the same steam_id updates display_name, avatar_url and
 * last_login_at, leaving created_at untouched. RETURNING id gives us the row id
 * whether we inserted or updated.
 *
 * @param db Optional injected handle (unit tests pass an in-memory DatabaseSync).
 *           When omitted, the process-wide server-only store is used.
 */
export async function ensureUser(
  steamId: string,
  displayName: string,
  avatarUrl: string,
  db?: DatabaseSync,
): Promise<string> {
  const handle = db ?? (await import('@/lib/db/sqlite')).getDb();
  const now = nowIso();
  const row = handle
    .prepare(
      `INSERT INTO users (steam_id, display_name, avatar_url, created_at, last_login_at)
       VALUES (?, ?, ?, ?, ?)
       ON CONFLICT(steam_id) DO UPDATE SET
         display_name = excluded.display_name,
         avatar_url = excluded.avatar_url,
         last_login_at = excluded.last_login_at
       RETURNING id`,
    )
    .get(steamId, displayName, avatarUrl, now, now) as { id: number | bigint } | undefined;
  if (!row) throw new Error('ensureUser: upsert returned no id');
  return String(row.id);
}
