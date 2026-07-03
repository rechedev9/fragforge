import type { VideoStatus } from './types';

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

export type ReconcileInput = {
  jobStatus: string;
  jobFailureReason?: string;
  renderStatus: RenderStatus;
  renderFailureReason?: string;
  /**
   * True when the render variant reports ready but THIS reel's own video
   * artifact does not exist. The render state is shared by every reel of the
   * job (it is per job+variant), so after reel A finishes, a newer reel B of
   * the same demo sees renderStatus 'ready' even though B's clips were never
   * captured or rendered - without this flag B showed a LISTO card whose
   * download 404'd, and its capture never ran.
   */
  reelVideoMissing?: boolean;
};

export type ReelView = {
  status: VideoStatus;
  action: ReelAction;
  /** Set only when status is 'failed' and the orchestrator supplied a reason. */
  failureReason?: string;
};

function failed(reason?: string): ReelView {
  return reason
    ? { status: 'failed', action: 'none', failureReason: reason }
    : { status: 'failed', action: 'none' };
}

export function deriveReelView(input: ReconcileInput): ReelView {
  const { jobStatus, jobFailureReason, renderStatus, renderFailureReason } = input;

  // A finished render is ready — but only for a reel whose own video exists.
  // A reel still missing its video needs its own capture+render pass: 'record'
  // (the generate flow) captures exactly the missing clips and chains the
  // render, so it covers both halves idempotently. While another reel of the
  // job is mid-capture, wait instead of driving: the record endpoint rejects a
  // job in 'recording' and that rejection would wrongly fail this reel.
  if (renderStatus === 'ready') {
    if (!input.reelVideoMissing) return { status: 'ready', action: 'none' };
    if (jobStatus === 'recording') return { status: 'recording', action: 'none' };
    if (jobStatus === 'failed') return failed(jobFailureReason);
    return { status: 'queued', action: 'record' };
  }
  if (jobStatus === 'failed') return failed(jobFailureReason);
  if (renderStatus === 'failed') return failed(renderFailureReason);
  if (renderStatus === 'queued' || renderStatus === 'rendering') {
    return { status: 'composing', action: 'none' };
  }

  // renderStatus === 'none': decide the next step from the job's own progress.
  switch (jobStatus) {
    case 'recording':
      return { status: 'recording', action: 'none' };
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
