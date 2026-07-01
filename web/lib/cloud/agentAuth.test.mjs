// Unit tests for the pure agentAuth helpers.
// Run: node --test lib/cloud/agentAuth.test.mjs
// Plain .mjs importing the type-stripped .ts module. hashToken/newToken have no
// Supabase dependency, and resolveAgent only loads `@/lib/supabase/server` via a
// lazy dynamic import inside its body, so importing this module never resolves
// the `@/` alias or loads `server-only` here.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { hashToken, newToken } from './agentAuth.ts';

test('hashToken is stable sha256 hex', () => {
  assert.equal(hashToken('abc'), hashToken('abc'));
  assert.match(hashToken('abc'), /^[0-9a-f]{64}$/);
});

test('newToken is 64 hex chars', () => {
  assert.match(newToken(), /^[0-9a-f]{64}$/);
});
