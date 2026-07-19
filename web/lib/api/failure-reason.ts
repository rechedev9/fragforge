/**
 * Machine-readable prefix the orchestrator stamps on a job whose demo cannot be
 * replayed because it was recorded on an older CS2 build. This failure is
 * deterministic in the `.dem` file itself, so retrying can never help; the
 * reason may end with a "; captured N/M segments before the failure" clause.
 */
export const DEMO_INCOMPATIBLE_PREFIX = 'demo_incompatible:' as const;

/** How many planned segments were captured before an incompatible demo aborted the run. */
export type CapturedCounts = { captured: number; requested: number };

/** Parsed classification of a reel's `failureReason` string. */
export type FailureReason = {
  kind: 'demo-incompatible' | 'generic';
  /** Spanish message the failed-reel card should surface to the user. */
  message: string;
  /** Whether a retry could plausibly resolve the failure. */
  retryCanHelp: boolean;
  /** Populated only for demo-incompatible failures that reported partial capture. */
  counts?: CapturedCounts;
};

const GENERIC_MESSAGE = 'El reel falló en tu equipo.';

const DEMO_INCOMPATIBLE_MESSAGE =
  'Esta demo se grabó en una versión antigua de CS2 y el cliente actual no puede reproducirla. ' +
  'Reintentar no lo arreglará: usa una demo jugada después del último parche.';

// Matches the orchestrator's "; captured N/M segments before the failure" clause.
const CAPTURED_CLAUSE = /captured\s+(\d+)\/(\d+)\s+segments/i;

function capturedSentence(counts: CapturedCounts): string {
  return ` Se capturaron ${counts.captured} de ${counts.requested} jugadas antes del fallo y siguen disponibles.`;
}

/**
 * Classifies a reel's raw `failureReason`. A reason beginning with
 * `demo_incompatible:` is a deterministic, non-retryable demo-build mismatch and
 * gets a Spanish explanation (plus a captured-counts sentence when the
 * orchestrator reported partial capture); everything else stays generic and
 * retryable. Pure so the card component never branches on the raw string.
 */
export function parseFailureReason(reason: string | undefined): FailureReason {
  if (reason === undefined || reason.trim() === '') {
    return { kind: 'generic', message: GENERIC_MESSAGE, retryCanHelp: true };
  }

  if (!reason.startsWith(DEMO_INCOMPATIBLE_PREFIX)) {
    return { kind: 'generic', message: reason, retryCanHelp: true };
  }

  const match = CAPTURED_CLAUSE.exec(reason);
  if (match === null) {
    return { kind: 'demo-incompatible', message: DEMO_INCOMPATIBLE_MESSAGE, retryCanHelp: false };
  }

  const counts: CapturedCounts = { captured: Number(match[1]), requested: Number(match[2]) };
  return {
    kind: 'demo-incompatible',
    message: DEMO_INCOMPATIBLE_MESSAGE + capturedSentence(counts),
    retryCanHelp: false,
    counts,
  };
}
