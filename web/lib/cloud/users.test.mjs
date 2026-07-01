// Unit tests for ensureUser's upsert-and-select shape.
// Run: node --test users.test.mjs
// Plain .mjs (invisible to tsc/Next) importing the type-stripped .ts module, so
// ensureUser is testable against a fake Supabase client with zero new dependencies.
// Every case below passes a fake `db`, so the lazy `import('@/lib/supabase/server')`
// branch (which loads the server-only Supabase client) never runs here.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { ensureUser } from './users.ts';

function fakeDb(captured) {
  return {
    from() { return this; },
    upsert(row, opts) { captured.row = row; captured.opts = opts; return this; },
    select() { return this; },
    async single() { return { data: { id: 'user-1' }, error: null }; },
  };
}

function failingDb(message) {
  return {
    from() { return this; },
    upsert() { return this; },
    select() { return this; },
    async single() { return { data: null, error: { message } }; },
  };
}

test('ensureUser upserts on steam_id and returns the id', async () => {
  const captured = {};
  const id = await ensureUser('7656', 'zack', 'http://a', fakeDb(captured));
  assert.equal(id, 'user-1');
  assert.equal(captured.row.steam_id, '7656');
  assert.equal(captured.opts.onConflict, 'steam_id');
});

test('ensureUser surfaces the db error instead of returning undefined', async () => {
  await assert.rejects(
    () => ensureUser('7656', 'zack', 'http://a', failingDb('unique_violation')),
    /ensureUser: unique_violation/,
  );
});
