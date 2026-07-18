import type {
  KillfeedAnalysisState,
  KillfeedReadEventReference,
  NormalizedRect,
  StreamEditPlan,
} from './api/streams.ts';

function cropFingerprint(crop: NormalizedRect | undefined): string {
  if (!crop) return '';
  return [crop.x, crop.y, crop.width, crop.height].join(':');
}

/**
 * Client-side change detector for the same edit inputs the server binds into
 * its source-backed SHA-256 fingerprint. The source hash stays server-side;
 * this value is only used to debounce and invalidate local UI state.
 */
export function killfeedAnalysisInputsFingerprint(plan: StreamEditPlan): string {
  return JSON.stringify({
    crop: cropFingerprint(plan.killfeed_crop),
    clips: plan.clips.map((clip) => [clip.id, clip.start_seconds, clip.end_seconds]),
  });
}

/**
 * Drops server-owned provenance and detected events after crop/range changes.
 * Keeping old cues would present stale timings while a replacement generation
 * is still running.
 */
export function invalidateKillfeedAnalysis(plan: StreamEditPlan): StreamEditPlan {
  const next = {
    ...plan,
    clips: plan.clips.map((clip) => {
      const clean = { ...clip };
      delete clean.killfeed_seconds;
      delete clean.killfeed_kills;
      return clean;
    }),
  };
  delete next.killfeed_analysis;
  return next;
}

export function killfeedAnalysisNeeded(plan: StreamEditPlan): boolean {
  return plan.killfeed_crop !== undefined && plan.killfeed_analysis === undefined;
}

function stateMatchesAppliedPlan(
  plan: StreamEditPlan,
  state: KillfeedAnalysisState | null,
): state is KillfeedAnalysisState {
  const metadata = plan.killfeed_analysis;
  return Boolean(
    metadata &&
      state &&
      state.status === 'applied' &&
      state.generation_id === metadata.generation_id &&
      state.fingerprint === metadata.fingerprint,
  );
}

/** Whether an exact OCR read must refresh durable generation state first. */
export function killfeedStateNeedsRefreshForRead(
  plan: StreamEditPlan,
  state: KillfeedAnalysisState | null,
): boolean {
  return plan.killfeed_analysis !== undefined && !stateMatchesAppliedPlan(plan, state);
}

/**
 * Finds the first manual placeholder that cannot be rendered exactly. Empty
 * cues are valid only when they are backed by a captured event in the applied
 * generation; a truly manual cue needs at least one reviewed structured kill.
 */
export function killfeedManualCueIssue(
  plan: StreamEditPlan,
  state: KillfeedAnalysisState | null,
): string | undefined {
  if (!plan.killfeed_crop || !stateMatchesAppliedPlan(plan, state)) return undefined;
  for (const clip of plan.clips) {
    const analyzed = (state.clips ?? []).find((candidate) => candidate.clip_id === clip.id);
    const exactCues = new Set((analyzed?.events ?? []).map((event) => event.cue_seconds));
    const planCues = clip.killfeed_seconds ?? [];
    for (const [index, cue] of planCues.entries()) {
      if (exactCues.has(cue) || (clip.killfeed_kills?.[index]?.length ?? 0) > 0) continue;
      return `La marca manual ${cue.toFixed(3)}s de ${clip.id} no tiene kills revisadas. Léela con IA, complétala o elimínala.`;
    }
    const presentCues = new Set(planCues);
    for (const cue of exactCues) {
      if (!presentCues.has(cue)) {
        return `Falta el evento automático ${cue.toFixed(3)}s de ${clip.id}. Reaplica el análisis de killfeed.`;
      }
    }
  }
  return undefined;
}

/**
 * Returns the immutable detector identity for a cue only when both the plan
 * and the loaded state prove it came from the active applied generation.
 * Exact equality is intentional: adjacent video frames must never alias.
 */
export function appliedKillfeedEventReference(
  plan: StreamEditPlan,
  state: KillfeedAnalysisState | null,
  clipId: string,
  cueSeconds: number,
): KillfeedReadEventReference | undefined {
  if (!stateMatchesAppliedPlan(plan, state)) return undefined;
  const clip = (state.clips ?? []).find((candidate) => candidate.clip_id === clipId);
  const event = clip?.events.find((candidate) => candidate.cue_seconds === cueSeconds);
  if (!event) return undefined;
  return { eventId: event.event_id, generationId: state.generation_id };
}
