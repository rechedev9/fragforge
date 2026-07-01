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
