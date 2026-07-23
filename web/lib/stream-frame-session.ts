const LAST_FRAME_MARGIN_SECONDS = 0.001;
const SEEK_TOLERANCE_SECONDS = 0.005;

function normalizedTarget(seconds: number, duration: number): number {
  const requested = Number.isFinite(seconds) ? Math.max(0, seconds) : 0;
  const lastFrame = Number.isFinite(duration) && duration > 0
    ? Math.max(0, duration - LAST_FRAME_MARGIN_SECONDS)
    : requested;
  return Math.min(requested, lastFrame);
}

/** Coalesces rapid scrub requests while one media seek is already in flight. */
export class LatestFrameRequest {
  #desiredSeconds = 0;
  #seeking = false;

  request(seconds: number): void {
    this.#desiredSeconds = Number.isFinite(seconds) ? Math.max(0, seconds) : 0;
  }

  next(currentSeconds: number, duration: number): number | null {
    if (this.#seeking) return null;
    const target = normalizedTarget(this.#desiredSeconds, duration);
    if (Math.abs(currentSeconds - target) <= SEEK_TOLERANCE_SECONDS) return null;
    this.#seeking = true;
    return target;
  }

  settled(currentSeconds: number, duration: number): number | null {
    this.#seeking = false;
    return this.next(currentSeconds, duration);
  }

  reset(seconds = 0): void {
    this.#desiredSeconds = Number.isFinite(seconds) ? Math.max(0, seconds) : 0;
    this.#seeking = false;
  }
}
