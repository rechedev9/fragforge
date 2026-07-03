// Wire status ints mirror the Go job.Status enum (internal/job/job.go).
export const GO_STATUS = {
  queued: 0,
  parsing: 1,
  parsed: 2,
  failed: 8,
  scanning: 9,
  scanned: 10,
} as const;

export const STATE_FROM_GO: Record<number, string> = Object.fromEntries(
  Object.entries(GO_STATUS).map(([k, v]) => [v, k]),
);

// Job lifecycle strings stored in the jobs table; the Go-derived ones must
// match STATE_FROM_GO above.
export const JOB_STATE = {
  queued: 'queued',
  running: 'running',
  parsing: 'parsing',
  parsed: 'parsed',
  scanning: 'scanning',
  scanned: 'scanned',
  done: 'done',
  failed: 'failed',
} as const;
export type JobState = (typeof JOB_STATE)[keyof typeof JOB_STATE];

export const JOB_TYPE = {
  scan: 'scan',
  parse: 'parse',
} as const;
export type JobType = (typeof JOB_TYPE)[keyof typeof JOB_TYPE];

// PostgREST "in" filter for jobs already in a terminal state.
export const TERMINAL_STATES_FILTER = `(${JOB_STATE.done},${JOB_STATE.failed})`;

export type JobRow = {
  demo_id: string;
  target_steamid: string;
  rules: unknown;
  demos: { storage_key: string; sha256: string };
};

export type JobDto = {
  id: string;
  status: string;
  demo_path: string;
  demo_sha256: string;
  target_steamid: string;
  rules: unknown;
};

// Narrows a raw Supabase jobs row (untyped client, joined demos may be typed
// as an array) into a JobRow, or null when the shape is not what we selected.
export function parseJobRow(value: unknown): JobRow | null {
  if (typeof value !== 'object' || value === null) return null;
  const row = value as Record<string, unknown>;
  const demos = Array.isArray(row.demos) ? row.demos[0] : row.demos;
  if (typeof demos !== 'object' || demos === null) return null;
  const demo = demos as Record<string, unknown>;
  if (typeof row.demo_id !== 'string' || typeof row.target_steamid !== 'string') return null;
  if (typeof demo.storage_key !== 'string' || typeof demo.sha256 !== 'string') return null;
  return {
    demo_id: row.demo_id,
    target_steamid: row.target_steamid,
    rules: row.rules,
    demos: { storage_key: demo.storage_key, sha256: demo.sha256 },
  };
}

export function toJobDto(row: JobRow): JobDto {
  return {
    id: row.demo_id,
    status: STATE_FROM_GO[GO_STATUS.queued],
    demo_path: row.demos.storage_key,
    demo_sha256: row.demos.sha256,
    target_steamid: row.target_steamid,
    rules: row.rules ?? {},
  };
}
