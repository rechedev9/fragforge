// Unit tests for the SQLite-backed ensureUser upsert.
// Run: node --test lib/accounts/users.test.ts
//
// Uses an in-memory DatabaseSync(':memory:') injected as the `db` argument, so
// the lazy `import('@/lib/db/sqlite')` branch (which is `server-only`) never
// runs here and no file is touched on disk. The schema is created inline to
// mirror lib/db/sqlite.ts::initSchema without importing that server-only module.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { DatabaseSync } from 'node:sqlite';
import { ensureUser } from './users.ts';

function freshDb(): DatabaseSync {
  const db = new DatabaseSync(':memory:');
  db.exec(`
    CREATE TABLE users (
      id INTEGER PRIMARY KEY,
      steam_id TEXT UNIQUE NOT NULL,
      display_name TEXT,
      avatar_url TEXT,
      created_at TEXT,
      last_login_at TEXT
    );
  `);
  return db;
}

test('ensureUser inserts on first login and returns a stable id', async () => {
  const db = freshDb();
  const id = await ensureUser('76561197960287930', 'zack', 'http://a/1.jpg', db);
  assert.equal(typeof id, 'string');

  const row = db
    .prepare('SELECT steam_id, display_name, avatar_url, created_at, last_login_at FROM users WHERE id = ?')
    .get(Number(id)) as Record<string, unknown>;
  assert.equal(row.steam_id, '76561197960287930');
  assert.equal(row.display_name, 'zack');
  assert.equal(row.avatar_url, 'http://a/1.jpg');
  assert.ok(row.created_at, 'created_at is set');
  assert.equal(row.created_at, row.last_login_at);

  // A second call for the same steam_id returns the same id (no duplicate row).
  const again = await ensureUser('76561197960287930', 'zack', 'http://a/1.jpg', db);
  assert.equal(again, id);
  const count = db.prepare('SELECT COUNT(*) AS n FROM users').get() as { n: number | bigint };
  assert.equal(Number(count.n), 1);
});

test('ensureUser updates display_name/avatar/last_login_at on conflict, keeping created_at', async () => {
  const db = freshDb();
  const id = await ensureUser('76561197960287930', 'old', 'http://old', db);
  const before = db
    .prepare('SELECT created_at, last_login_at FROM users WHERE id = ?')
    .get(Number(id)) as { created_at: string; last_login_at: string };

  // Ensure the clock advances so last_login_at is observably newer.
  await new Promise((r) => setTimeout(r, 5));

  const id2 = await ensureUser('76561197960287930', 'new', 'http://new', db);
  assert.equal(id2, id);

  const after = db
    .prepare('SELECT display_name, avatar_url, created_at, last_login_at FROM users WHERE id = ?')
    .get(Number(id)) as { display_name: string; avatar_url: string; created_at: string; last_login_at: string };
  assert.equal(after.display_name, 'new');
  assert.equal(after.avatar_url, 'http://new');
  assert.equal(after.created_at, before.created_at, 'created_at is preserved on update');
  assert.ok(after.last_login_at >= before.last_login_at, 'last_login_at advances on update');
});
