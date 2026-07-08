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
}

// startPollLoop runs the first tick immediately, then reschedules itself using
// the cadence each tick returns. It returns a stop function that cancels any
// pending timer and prevents any further scheduling, including across a tick's
// in-flight await. Ticks never overlap: the next one is scheduled only after the
// current one settles.
export function startPollLoop(opts: PollLoopOptions): () => void {
  let stopped = false;
  let timer: ReturnType<typeof setTimeout> | undefined;

  function schedule(delayMs: number): void {
    if (stopped) return;
    timer = setTimeout(() => void run(), delayMs);
  }

  async function run(): Promise<void> {
    if (stopped) return;
    let cadence: PollCadence;
    try {
      cadence = await opts.tick();
    } catch {
      // A transient tick failure must never kill the loop; back off to idle and
      // try again on the next beat.
      cadence = 'idle';
    }
    // The loop may have been stopped while the tick was awaiting; do not
    // schedule another run past stop().
    if (stopped) return;
    schedule(cadence === 'fast' ? opts.fastMs : opts.idleMs);
  }

  void run();

  return function stop(): void {
    stopped = true;
    if (timer !== undefined) {
      clearTimeout(timer);
      timer = undefined;
    }
  };
}
