import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createScanJob } from './demos.ts';

// Each insert is awaited directly (no .select), so the builder is a thenable
// that resolves to { error }. The fake mirrors exactly that shape.
function fakeDb(captured) {
  return {
    storage: { from() { return { async upload(p) { captured.upload = p; return { error: null }; } }; } },
    from(table) {
      return {
        insert(row) {
          captured[table] = row;
          return { then: (resolve) => resolve({ error: null }) };
        },
      };
    },
  };
}

test('createScanJob uploads, inserts demo and a queued scan job', async () => {
  const captured = {};
  const { demoId } = await createScanJob('u1', { name: 'm.dem', size: 10, bytes: new ArrayBuffer(10) }, fakeDb(captured));
  assert.match(demoId, /[0-9a-f-]{36}/);
  assert.equal(captured.jobs.type, 'scan');
  assert.equal(captured.jobs.state, 'queued');
  assert.equal(captured.demos.user_id, 'u1');
  assert.equal(captured.demos.id, demoId);
});
