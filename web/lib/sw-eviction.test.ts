// Pins the reload rule for service-worker eviction: reload exactly when the
// page was controlled by a (foreign) worker, something was actually
// unregistered, and this tab has not already reloaded for it.
import test from 'node:test';
import assert from 'node:assert/strict';
import { shouldReloadAfterEviction } from './sw-eviction.ts';

test('shouldReloadAfterEviction: reloads once for a controlled page with evictions', () => {
  const cases: Array<{
    name: string;
    wasControlled: boolean;
    unregisteredCount: number;
    alreadyReloaded: boolean;
    want: boolean;
  }> = [
    { name: 'controlled + evicted + first time', wasControlled: true, unregisteredCount: 1, alreadyReloaded: false, want: true },
    { name: 'uncontrolled page needs no reload', wasControlled: false, unregisteredCount: 1, alreadyReloaded: false, want: false },
    { name: 'nothing evicted needs no reload', wasControlled: true, unregisteredCount: 0, alreadyReloaded: false, want: false },
    { name: 'already reloaded never loops', wasControlled: true, unregisteredCount: 1, alreadyReloaded: true, want: false },
  ];
  for (const c of cases) {
    const got = shouldReloadAfterEviction({
      wasControlled: c.wasControlled,
      unregisteredCount: c.unregisteredCount,
      alreadyReloaded: c.alreadyReloaded,
    });
    assert.equal(got, c.want, c.name);
  }
});
