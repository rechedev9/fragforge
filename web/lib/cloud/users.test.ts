// Unit tests for ensureUser's upsert-and-select shape.
// Run: node --test users.test.ts
// Type-checked TypeScript run directly by Node's native type stripping, so
// ensureUser is testable against a fake Supabase client with zero new dependencies.
// Every case below passes a fake `db`, so the lazy `import('@/lib/supabase/server')`
// branch (which loads the server-only Supabase client) never runs here.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { ensureUser } from './users.ts';
import type { SupabaseClient } from '@supabase/supabase-js';

type Captured = {
  row?: Record<string, unknown>;
  opts?: { onConflict?: string };
};

function fakeDb(captured: Captured): SupabaseClient {
  const db = {
    from() { return db; },
    upsert(row: Record<string, unknown>, opts: { onConflict?: string }) { captured.row = row; captured.opts = opts; return db; },
    select() { return db; },
    async single() { return { data: { id: 'user-1' }, error: null }; },
  };
  // Partial test double covering only the call surface ensureUser uses.
  return db as unknown as SupabaseClient;
}

function failingDb(message: string): SupabaseClient {
  const db = {
    from() { return db; },
    upsert() { return db; },
    select() { return db; },
    async single() { return { data: null, error: { message } }; },
  };
  // Partial test double covering only the call surface ensureUser uses.
  return db as unknown as SupabaseClient;
}

test('ensureUser upserts on steam_id and returns the id', async () => {
  const captured: Captured = {};
  const id = await ensureUser('7656', 'zack', 'http://a', fakeDb(captured));
  assert.equal(id, 'user-1');
  assert.equal(captured.row?.steam_id, '7656');
  assert.equal(captured.opts?.onConflict, 'steam_id');
});

test('ensureUser surfaces the db error instead of returning undefined', async () => {
  await assert.rejects(
    () => ensureUser('7656', 'zack', 'http://a', failingDb('unique_violation')),
    /ensureUser: unique_violation/,
  );
});
