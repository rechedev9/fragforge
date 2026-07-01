import { test } from 'node:test';
import assert from 'node:assert/strict';
import { agentOwnsKey } from './blobAuth.ts';

// Mirrors the demos.test.mjs fake-db style: `.from('demos').select().eq().maybeSingle()`
// resolves directly (no `.then` needed since the route awaits `maybeSingle()`).
function fakeDbWithOwner(userId) {
  return {
    from(table) {
      assert.equal(table, 'demos');
      return {
        select() {
          return {
            eq(column, value) {
              assert.equal(column, 'id');
              return {
                async maybeSingle() {
                  return { data: { user_id: userId }, error: null };
                },
              };
            },
          };
        },
      };
    },
  };
}

test('demos/ keys are authorized by a pure prefix check, no db needed', async () => {
  assert.equal(await agentOwnsKey('demos/u1/x.dem', 'u1'), true);
  assert.equal(await agentOwnsKey('demos/u2/x.dem', 'u1'), false);
});

test('artifacts keys are authorized by looking up the owning demo', async () => {
  const db = fakeDbWithOwner('u1');
  assert.equal(await agentOwnsKey('jobs/d1/roster.json', 'u1', db), true);
  assert.equal(await agentOwnsKey('jobs/d1/roster.json', 'u2', db), false);
});

test('malformed artifact keys are rejected', async () => {
  assert.equal(await agentOwnsKey('secret.txt', 'u1'), false);
});

test('empty or blank keys are rejected', async () => {
  assert.equal(await agentOwnsKey('', 'u1'), false);
  assert.equal(await agentOwnsKey('   ', 'u1'), false);
});
