import 'server-only';
import { DatabaseSync } from 'node:sqlite';
import { mkdirSync } from 'node:fs';
import { dirname } from 'node:path';

// Server-only SQLite accounts store, backed by Node's built-in node:sqlite
// (DatabaseSync). No native module (better-sqlite3) is used, so this needs no
// gcc/make on the host. This file must never be bundled into a client component:
// the browser has no node:sqlite and Next would fail to resolve it.

const DEFAULT_DB_PATH = '/data/fragforge.db';

let db: DatabaseSync | null = null;

/** Absolute path of the accounts DB file, from FRAGFORGE_SQLITE_PATH. */
export function dbPath(): string {
  return process.env.FRAGFORGE_SQLITE_PATH || DEFAULT_DB_PATH;
}

/**
 * Create the schema on a freshly opened handle. Idempotent: every statement is
 * IF NOT EXISTS, so calling it on an existing DB is a no-op.
 *
 * - users: one row per Steam login. steam_id is the natural key.
 * - agent_tokens: a future server-side pairing registry. Unused in v1 (Topology
 *   A pairs the browser to the LOCAL agent with a token the agent itself mints),
 *   created now so the schema is stable.
 */
export function initSchema(handle: DatabaseSync): void {
  handle.exec(`
    CREATE TABLE IF NOT EXISTS users (
      id INTEGER PRIMARY KEY,
      steam_id TEXT UNIQUE NOT NULL,
      display_name TEXT,
      avatar_url TEXT,
      created_at TEXT,
      last_login_at TEXT
    );
    CREATE TABLE IF NOT EXISTS agent_tokens (
      id INTEGER PRIMARY KEY,
      user_id INTEGER,
      token_hash TEXT,
      label TEXT,
      created_at TEXT,
      last_seen_at TEXT
    );
  `);
}

/**
 * Process-wide singleton DatabaseSync handle. Opens FRAGFORGE_SQLITE_PATH
 * (default /data/fragforge.db), creating the parent dir if missing, enables WAL
 * for concurrent reads during writes, and creates the tables on first use.
 */
export function getDb(): DatabaseSync {
  if (db) return db;
  const path = dbPath();
  mkdirSync(dirname(path), { recursive: true });
  const handle = new DatabaseSync(path);
  // WAL survives across opens once set, but setting it again is cheap and makes
  // a fresh DB file behave the same as an existing one.
  handle.exec('PRAGMA journal_mode = WAL;');
  initSchema(handle);
  db = handle;
  return db;
}
