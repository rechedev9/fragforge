import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createScanJob } from './demos.ts';
import type { SupabaseClient } from '@supabase/supabase-js';

type Captured = {
  upload?: string;
  inserted: Record<string, Record<string, unknown>>;
};

// Each insert is awaited directly (no .select), so the builder is a thenable
// that resolves to { error }. The fake mirrors exactly that shape.
function fakeDb(captured: Captured): SupabaseClient {
  const db = {
    storage: {
      from() {
        return {
          async upload(p: string) {
            captured.upload = p;
            return { error: null };
          },
        };
      },
    },
    from(table: string) {
      return {
        insert(row: Record<string, unknown>) {
          captured.inserted[table] = row;
          return { then: (resolve: (value: { error: null }) => void) => resolve({ error: null }) };
        },
      };
    },
  };
  // Partial test double covering only the call surface createScanJob uses.
  return db as unknown as SupabaseClient;
}

test('createScanJob uploads, inserts demo and a queued scan job', async () => {
  const captured: Captured = { inserted: {} };
  const { demoId } = await createScanJob('u1', { name: 'm.dem', size: 10, bytes: new ArrayBuffer(10) }, fakeDb(captured));
  assert.match(demoId, /[0-9a-f-]{36}/);
  assert.equal(captured.upload, `u1/${demoId}.dem`);
  assert.equal(captured.inserted.jobs.type, 'scan');
  assert.equal(captured.inserted.jobs.state, 'queued');
  assert.equal(captured.inserted.demos.user_id, 'u1');
  assert.equal(captured.inserted.demos.id, demoId);
  assert.equal(captured.inserted.demos.storage_key, `demos/u1/${demoId}.dem`);
});
