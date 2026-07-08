// Unit tests for the self-rescheduling poll loop. Run: node --test "lib/**/*.test.ts"
import test from 'node:test';
import assert from 'node:assert/strict';
import { startPollLoop, type PollCadence } from './poll-loop.ts';

// flushMicrotasks lets the pending tick promise (and its .then/catch chain)
// settle before we advance the fake clock again.
async function flushMicrotasks(): Promise<void> {
  await Promise.resolve();
  await Promise.resolve();
}

test('a throwing tick still schedules the next tick (the freeze bug)', async (t) => {
  t.mock.timers.enable({ apis: ['setTimeout'] });

  let calls = 0;
  const stop = startPollLoop({
    tick: async () => {
      calls += 1;
      if (calls === 1) throw new Error('transient orchestrator hiccup');
      return 'fast';
    },
    fastMs: 1000,
    idleMs: 5000,
  });
  t.after(stop);

  // First tick runs immediately and throws.
  await flushMicrotasks();
  assert.equal(calls, 1);

  // The bug was: after a throw, nothing reschedules. The loop must recover at
  // the idle cadence, so advancing past idleMs runs a second tick.
  t.mock.timers.tick(5000);
  await flushMicrotasks();
  assert.equal(calls, 2, 'loop must keep polling after a tick throws');
});

test('stop() cancels the pending timer and prevents further ticks', async (t) => {
  t.mock.timers.enable({ apis: ['setTimeout'] });

  let calls = 0;
  const stop = startPollLoop({
    tick: async () => {
      calls += 1;
      return 'fast';
    },
    fastMs: 1000,
    idleMs: 5000,
  });

  await flushMicrotasks();
  assert.equal(calls, 1);

  stop();
  t.mock.timers.tick(10000);
  await flushMicrotasks();
  assert.equal(calls, 1, 'no tick should run after stop()');
});

test('stop() during an in-flight tick prevents the next schedule', async (t) => {
  t.mock.timers.enable({ apis: ['setTimeout'] });

  let calls = 0;
  let release: (() => void) | undefined;
  const stop = startPollLoop({
    tick: async () => {
      calls += 1;
      await new Promise<void>((resolve) => {
        release = resolve;
      });
      return 'fast';
    },
    fastMs: 1000,
    idleMs: 5000,
  });

  await flushMicrotasks();
  assert.equal(calls, 1);

  // Stop while the first tick is still awaiting, then let it resolve.
  stop();
  release?.();
  await flushMicrotasks();

  t.mock.timers.tick(10000);
  await flushMicrotasks();
  assert.equal(calls, 1, 'a tick settling after stop() must not reschedule');
});

test('cadence selects fast vs idle delay', async (t) => {
  t.mock.timers.enable({ apis: ['setTimeout'] });

  const cadences: PollCadence[] = ['fast', 'idle', 'fast'];
  let calls = 0;
  const stop = startPollLoop({
    tick: async () => {
      const cadence = cadences[calls] ?? 'idle';
      calls += 1;
      return cadence;
    },
    fastMs: 1000,
    idleMs: 5000,
  });
  t.after(stop);

  await flushMicrotasks();
  assert.equal(calls, 1);

  // First tick returned 'fast': a second tick fires 1000ms later, not before.
  t.mock.timers.tick(999);
  await flushMicrotasks();
  assert.equal(calls, 1, 'fast tick must not fire before fastMs');
  t.mock.timers.tick(1);
  await flushMicrotasks();
  assert.equal(calls, 2);

  // Second tick returned 'idle': the next tick waits idleMs, so 1000ms is not
  // enough.
  t.mock.timers.tick(1000);
  await flushMicrotasks();
  assert.equal(calls, 2, 'idle tick must not fire at the fast cadence');
  t.mock.timers.tick(4000);
  await flushMicrotasks();
  assert.equal(calls, 3);
});
