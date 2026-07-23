import { browserWindowActivity, type WindowActivity } from './window-activity.ts';

// A self-rescheduling poll loop that survives a throwing tick.
//
// The library page polls the orchestrator on a timer that reschedules itself
// after each tick. The subtle failure mode this module exists to prevent: if a
// tick rejects once (a transient proxy/orchestrator hiccup) and the caller
// forgets to catch it, the next tick is never scheduled and the loop dies
// silently forever - a finished render freezes mid-pipeline in the UI. Here a
// throwing tick is caught and still schedules the next run (at the idle
// cadence), so one transient error costs one slow beat, not the whole loop.

// PollCadence is what a tick returns to pick the delay before the next tick:
// 'fast' while work is in flight, 'idle' when everything is settled.
export type PollCadence = 'fast' | 'idle';

export interface PollLoopOptions {
  // tick runs one poll and returns the cadence for the next one. It may reject;
  // the loop treats a rejection as an 'idle' beat and keeps going.
  tick: () => Promise<PollCadence>;
  fastMs: number;
  idleMs: number;
  /**
   * Activity source used to suspend polling. The browser window is the default;
   * injection keeps the lifecycle deterministic in tests.
   */
  activity?: WindowActivity;
}

// startPollLoop runs the first tick immediately, then reschedules itself using
// the cadence each tick returns. It returns a stop function that cancels any
// pending timer and prevents any further scheduling, including across a tick's
// in-flight await. Ticks never overlap: the next one is scheduled only after the
// current one settles.
export function startPollLoop(opts: PollLoopOptions): () => void {
  let stopped = false;
  let running = false;
  let refreshPending = false;
  let timer: ReturnType<typeof setTimeout> | undefined;
  const activity = opts.activity ?? browserWindowActivity;

  function schedule(delayMs: number): void {
    if (stopped || !activity.isActive()) return;
    timer = setTimeout(() => void run(), delayMs);
  }

  async function run(): Promise<void> {
    if (stopped || !activity.isActive()) return;
    if (running) {
      refreshPending = true;
      return;
    }
    running = true;
    let cadence: PollCadence;
    try {
      cadence = await opts.tick();
    } catch {
      // A transient tick failure must never kill the loop; back off to idle and
      // try again on the next beat.
      cadence = 'idle';
    } finally {
      running = false;
    }
    // The loop may have been stopped while the tick was awaiting; do not
    // schedule another run past stop().
    if (stopped) return;
    if (refreshPending && activity.isActive()) {
      refreshPending = false;
      void run();
      return;
    }
    schedule(cadence === 'fast' ? opts.fastMs : opts.idleMs);
  }

  const unsubscribe = activity.subscribe(() => {
    if (stopped) return;
    if (!activity.isActive()) {
      refreshPending = false;
      if (timer !== undefined) {
        clearTimeout(timer);
        timer = undefined;
      }
      return;
    }
    if (timer !== undefined) {
      clearTimeout(timer);
      timer = undefined;
    }
    if (running) {
      refreshPending = true;
    } else {
      void run();
    }
  });

  if (activity.isActive()) void run();

  return function stop(): void {
    stopped = true;
    unsubscribe();
    if (timer !== undefined) {
      clearTimeout(timer);
      timer = undefined;
    }
  };
}
