import type { StreamEditPlan, StreamJob } from './api/streams.ts';

const PREFIX = 'fragforge.stream-draft.';

export type StreamDraft = {
  savedAt: string;
  basePlanFingerprint: string;
  plan: StreamEditPlan;
  editorSessionId?: string;
  revision?: number;
};

export type StreamDraftRevision = {
  editorSessionId: string;
  revision: number;
};

function canonicalJSON(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(canonicalJSON);
  if (value !== null && typeof value === 'object') {
    return Object.fromEntries(
      Object.entries(value as Record<string, unknown>)
        .filter(([key]) => key !== 'updated_at')
        .sort(([left], [right]) => left.localeCompare(right))
        .map(([key, nested]) => [key, canonicalJSON(nested)]),
    );
  }
  return value;
}

export function streamEditPlanFingerprint(plan: StreamEditPlan): string {
  return JSON.stringify(canonicalJSON(plan));
}

export function recoverableStreamJobs(jobs: readonly StreamJob[]): StreamJob[] {
  return jobs
    .filter((job) => job.status !== 'failed')
    .sort((a, b) => Date.parse(b.updated_at ?? b.created_at) - Date.parse(a.updated_at ?? a.created_at));
}

export function saveStreamDraft(
  storage: Pick<Storage, 'setItem'>,
  jobId: string,
  plan: StreamEditPlan,
  savedAt = new Date().toISOString(),
  basePlanFingerprint = streamEditPlanFingerprint(plan),
  draftRevision?: StreamDraftRevision,
): void {
  try {
    storage.setItem(`${PREFIX}${jobId}`, JSON.stringify({ savedAt, basePlanFingerprint, plan, ...draftRevision } satisfies StreamDraft));
  } catch {
    // Server autosave remains available when browser storage is blocked or full.
  }
}

export function loadStreamDraft(storage: Pick<Storage, 'getItem'>, jobId: string): StreamDraft | null {
  try {
    const raw = storage.getItem(`${PREFIX}${jobId}`);
    if (raw === null) return null;
    const value: unknown = JSON.parse(raw);
    if (!value || typeof value !== 'object') return null;
    const candidate = value as Partial<StreamDraft>;
    const plan = candidate.plan as Partial<StreamEditPlan> | undefined;
    const hasRevision = candidate.editorSessionId !== undefined || candidate.revision !== undefined;
    const validRevision = !hasRevision || (
      typeof candidate.editorSessionId === 'string' &&
      candidate.editorSessionId.length > 0 &&
      typeof candidate.revision === 'number' &&
      Number.isSafeInteger(candidate.revision) &&
      candidate.revision >= 0
    );
    return typeof candidate.savedAt === 'string' &&
      Number.isFinite(Date.parse(candidate.savedAt)) &&
      typeof candidate.basePlanFingerprint === 'string' &&
      validRevision &&
      plan !== undefined &&
      typeof plan.schema_version === 'string' &&
      typeof plan.variant === 'string' &&
      Array.isArray(plan.clips)
      ? candidate as StreamDraft
      : null;
  } catch {
    return null;
  }
}

export function selectStreamDraftPlan(draft: StreamDraft | null, serverPlan: StreamEditPlan): StreamEditPlan | null {
  if (draft === null) return null;
  const serverFingerprint = streamEditPlanFingerprint(serverPlan);
  return draft.basePlanFingerprint === serverFingerprint || streamEditPlanFingerprint(draft.plan) === serverFingerprint
    ? draft.plan
    : null;
}

/** Confirm one server write without discarding a newer local edit queued behind it. */
export function reconcileStreamDraftAfterSave(
  storage: Pick<Storage, 'getItem' | 'setItem' | 'removeItem'>,
  jobId: string,
  submittedPlan: StreamEditPlan,
  savedPlan: StreamEditPlan,
  submittedRevision: StreamDraftRevision,
): void {
  try {
    const draft = loadStreamDraft(storage, jobId);
    if (draft === null) return;
    const submittedFingerprint = streamEditPlanFingerprint(submittedPlan);
    const savedFingerprint = streamEditPlanFingerprint(savedPlan);
    const draftFingerprint = streamEditPlanFingerprint(draft.plan);
    if (draftFingerprint === submittedFingerprint || draftFingerprint === savedFingerprint) {
      storage.removeItem(`${PREFIX}${jobId}`);
      return;
    }
    const isNewerRevision = draft.editorSessionId === submittedRevision.editorSessionId &&
      typeof draft.revision === 'number' &&
      draft.revision > submittedRevision.revision;
    if (isNewerRevision) {
      storage.setItem(`${PREFIX}${jobId}`, JSON.stringify({ ...draft, basePlanFingerprint: savedFingerprint } satisfies StreamDraft));
    }
  } catch {
    // A blocked browser store must not turn a confirmed server save into an error.
  }
}
