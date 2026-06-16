/**
 * Parse a "rounds-rounds" score string (e.g. "13-7") into its two halves.
 * Returns null for either side that is not a number so callers can fall back.
 */
export function parseScore(score: string): { ours: number | null; theirs: number | null } {
  const [left, right] = score.split('-', 2);
  const ours = Number.parseInt(left ?? '', 10);
  const theirs = Number.parseInt(right ?? '', 10);
  return {
    ours: Number.isNaN(ours) ? null : ours,
    theirs: Number.isNaN(theirs) ? null : theirs,
  };
}

/** A match is a win when our round count is strictly higher than theirs. */
export function isWin(score: string): boolean {
  const { ours, theirs } = parseScore(score);
  if (ours === null || theirs === null) return false;
  return ours > theirs;
}
