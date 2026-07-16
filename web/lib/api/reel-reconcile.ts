import type { VideoStatus, CaptureProgress } from './types';

/**
 * Reel reconcile core — pure, framework-free, the testable heart of the durable
 * upload→reel path. Given the orchestrator's truth for a reel (its job status and
 * the render-variant status), it returns the UI status to show plus the single,
 * idempotent step to drive next. The caller (RealApiClient) performs `action` and
 * relies entirely on this mapping, so a page reload that re-reads server state
 * resumes exactly where it left off and never double-drives a stage.
 *
 * Unit-tested in reel-reconcile.test.ts (node:test).
 */

/** Render-variant lifecycle as the orchestrator reports it; 'none' = not started. */
export type RenderStatus = 'none' | 'queued' | 'rendering' | 'ready' | 'failed';

/** The one pipeline step to issue this tick (idempotent against server state). */
export type ReelAction = 'record' | 'render' | 'none';

/**
 * Job statuses at which a render variant can already exist on the orchestrator, so
 * the render-status GET is worth issuing. A render POST is only ever driven at or
 * after 'recorded' — deriveReelView returns the 'render' action from 'recorded'
 * onward — so for every earlier status ('queued'/'scanning'/'scanned'/'parsing'/
 * 'parsed'/'recording') the GET is a guaranteed 404 and can be skipped; that 404 is
 * what floods the browser DevTools network console during the whole recording phase.
 * 'failed' is included because a render can reach 'ready' before the job later flags
 * an error, and deriveReelView must still surface that reel as ready (a finished
 * render wins over a failed job); skipping the GET for a failed job would wrongly
 * downgrade an already-rendered reel to failed.
 */
const RENDER_STATE_STATUSES = new Set<string>(['recorded', 'composing', 'composed', 'done', 'failed']);

/**
 * Whether a job's render variant can possibly exist yet, i.e. whether issuing the
 * render-status GET can return anything but a 404. Gate the network call on this so
 * a pending render stops spamming the DevTools console with expected 404s.
 */
export function canHaveRenderState(status: string): boolean {
  return RENDER_STATE_STATUSES.has(status);
}

export type ReconcileInput = {
  jobStatus: string;
  jobFailureReason?: string;
  renderStatus: RenderStatus;
  renderFailureReason?: string;
  /** Live capture progress from the job poll; meaningful only while recording. */
  captureProgress?: CaptureProgress;
};

export type ReelView = {
  status: VideoStatus;
  action: ReelAction;
  /** Set only when status is 'failed' and the orchestrator supplied a reason. */
  failureReason?: string;
  /** Set only when status is 'recording' and the orchestrator reported progress. */
  captureProgress?: CaptureProgress;
};

function failed(reason?: string): ReelView {
  return reason
    ? { status: 'failed', action: 'none', failureReason: reason }
    : { status: 'failed', action: 'none' };
}

export function deriveReelView(input: ReconcileInput): ReelView {
  const { jobStatus, jobFailureReason, renderStatus, renderFailureReason, captureProgress } = input;

  // A finished render is always ready — even if the job later flags an error.
  if (renderStatus === 'ready') return { status: 'ready', action: 'none' };
  if (jobStatus === 'failed') return failed(jobFailureReason);
  if (renderStatus === 'failed') return failed(renderFailureReason);
  if (renderStatus === 'queued' || renderStatus === 'rendering') {
    return { status: 'composing', action: 'none' };
  }

  // renderStatus === 'none': decide the next step from the job's own progress.
  switch (jobStatus) {
    case 'recording':
      // Carry through the real segments-done/total so the card can show a
      // percent; omit the key entirely when the poll reported no progress yet,
      // so the card keeps its indeterminate rendering.
      return captureProgress
        ? { status: 'recording', action: 'none', captureProgress }
        : { status: 'recording', action: 'none' };
    case 'parsed':
      return { status: 'queued', action: 'record' };
    case 'recorded':
    case 'composed':
    case 'done':
      return { status: 'composing', action: 'render' };
    case 'composing':
      return { status: 'composing', action: 'none' };
    default:
      // queued / scanning / scanned / parsing / unknown: not yet drivable as a reel.
      return { status: 'queued', action: 'none' };
  }
}
