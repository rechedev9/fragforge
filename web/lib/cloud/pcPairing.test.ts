// Unit tests for the pairing logic behind /api/pc/pair and the login guest
// migration. Run: node --test pcPairing.test.ts
// A fake Supabase client covers only the call surface these functions use, so
// the guest-minting and DB-insert decisions are testable with no dependencies.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import type { SupabaseClient } from '@supabase/supabase-js';
import { issuePairingCode, migrateGuestAgents, pairingCode } from './pcPairing.ts';
import { verifySession, isGuest, GUEST_PERSONA, type SessionPayload } from '../auth/session.ts';

const STEAM_SESSION: SessionPayload = {
  steamid64: '76561198000000000',
  persona: 'kekO',
  avatar: 'https://example.com/a.jpg',
  matchHistoryLinked: true,
};

type Captured = {
  upsert?: { row: Record<string, unknown>; opts: { onConflict?: string } };
  insert?: Record<string, unknown>;
  update?: { row: Record<string, unknown>; eq: { col: string; val: string } };
};

// One fake client backing both ensureUser (upsert→select→single) and the agents
// insert/update. insertError forces the pairing-failed branch.
function fakeDb(captured: Captured, insertError?: string): SupabaseClient {
  const db = {
    from() {
      return db;
    },
    upsert(row: Record<string, unknown>, opts: { onConflict?: string }) {
      captured.upsert = { row, opts };
      return db;
    },
    select() {
      return db;
    },
    async single() {
      return { data: { id: 'user-1' }, error: null };
    },
    insert(row: Record<string, unknown>) {
      captured.insert = row;
      return { error: insertError ? { message: insertError } : null };
    },
    update(row: Record<string, unknown>) {
      db.pendingUpdate = row;
      return db;
    },
    pendingUpdate: {} as Record<string, unknown>,
    eq(col: string, val: string) {
      captured.update = { row: db.pendingUpdate, eq: { col, val } };
      return { error: null };
    },
  };
  // Partial test double covering only the call surface these functions use.
  return db as unknown as SupabaseClient;
}

test('pairingCode is 8 chars from the unambiguous alphabet', () => {
  const code = pairingCode();
  assert.equal(code.length, 8);
  assert.match(code, /^[ABCDEFGHJKMNPQRSTUVWXYZ23456789]{8}$/);
});

test('no session mints a guest, upserts it, and returns a code plus a cookie', async () => {
  const captured: Captured = {};
  const outcome = await issuePairingCode(null, fakeDb(captured));
  assert.ok(outcome);
  assert.equal(outcome.pairingCode.length, 8);

  // A guest cookie is returned for the caller to set, and it round-trips.
  assert.ok(outcome.sessionCookie);
  const minted = verifySession(outcome.sessionCookie.token);
  assert.ok(minted);
  assert.equal(isGuest(minted), true);
  assert.equal(minted.persona, GUEST_PERSONA);

  // The guest user is upserted and a pending agent row is inserted.
  assert.match(String(captured.upsert?.row.steam_id), /^guest:/);
  assert.match(String(captured.insert?.name), /^pending:\d+$/);
  assert.ok(captured.insert?.token_hash);
});

test('an existing Steam session pairs without minting a cookie', async () => {
  const captured: Captured = {};
  const outcome = await issuePairingCode(STEAM_SESSION, fakeDb(captured));
  assert.ok(outcome);
  assert.equal(outcome.sessionCookie, null);
  assert.equal(captured.upsert?.row.steam_id, STEAM_SESSION.steamid64);
});

test('a failed agents insert reports pairing failure', async () => {
  const outcome = await issuePairingCode(null, fakeDb({}, 'insert boom'));
  assert.equal(outcome, null);
});

test('migrateGuestAgents reassigns the guest user rows to the Steam user', async () => {
  const captured: Captured = {};
  await migrateGuestAgents('guest-user', 'steam-user', fakeDb(captured));
  assert.deepEqual(captured.update, {
    row: { user_id: 'steam-user' },
    eq: { col: 'user_id', val: 'guest-user' },
  });
});

test('migrateGuestAgents surfaces a DB error', async () => {
  const failing = {
    from() {
      return failing;
    },
    update() {
      return failing;
    },
    eq() {
      return { error: { message: 'update boom' } };
    },
  };
  await assert.rejects(
    () => migrateGuestAgents('a', 'b', failing as unknown as SupabaseClient),
    /migrateGuestAgents: update boom/,
  );
});
