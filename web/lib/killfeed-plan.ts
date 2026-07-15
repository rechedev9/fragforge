import type { KillfeedKill, KillfeedReadEvent, StreamClipRange, StreamEditPlan } from './api/streams';

const READ_CUE_MERGE_SECONDS = 0.02;
const EDIT_PLAN_SCHEMA_VERSION = '1.1';

export function initialStreamClipEnd(durationSeconds: number): number {
  return Number.isFinite(durationSeconds) && durationSeconds > 0
    ? Math.min(durationSeconds, 20)
    : 20;
}

function killIdentity(kill: KillfeedKill): string {
  return [
    kill.attacker_side,
    kill.attacker_name,
    kill.victim_side,
    kill.victim_name,
    kill.assister_side ?? '',
    kill.assister_name ?? '',
    kill.weapon,
    kill.headshot ?? false,
    kill.wallbang ?? false,
    kill.noscope ?? false,
    kill.smoke ?? false,
    kill.blind ?? false,
    kill.in_air ?? false,
    kill.flash_assist ?? false,
  ].join('\u0000');
}

function mergeKills(left: KillfeedKill[], right: KillfeedKill[]): KillfeedKill[] {
  const merged = [...left];
  const seen = new Set(left.map(killIdentity));
  for (const kill of right) {
    const identity = killIdentity(kill);
    if (seen.has(identity)) continue;
    seen.add(identity);
    merged.push(kill);
  }
  return merged;
}

/**
 * Keeps a clip's killfeed cues and their kills consistent: drops cues outside
 * the clip range, sorts by timestamp, dedupes (preferring the entry that has
 * kills), and keeps `killfeed_kills` index-aligned with `killfeed_seconds`.
 *
 * The Go plan accepts either an omitted `killfeed_kills` or one whose length
 * matches `killfeed_seconds`, so this emits `killfeed_kills` only when at least
 * one cue has kills; otherwise it omits it and every cue keeps the frozen-crop
 * behavior.
 */
export function normalizeClipKillfeed(clip: StreamClipRange): StreamClipRange {
  const cues = clip.killfeed_seconds ?? [];
  const kills = clip.killfeed_kills ?? [];
  const pairs = cues
    .map((cue, index) => ({ cue, kills: kills[index] ?? [] }))
    .filter(
      ({ cue }) =>
        Number.isFinite(cue) && cue >= clip.start_seconds && cue < clip.end_seconds,
    )
    .sort((left, right) => left.cue - right.cue);

  const deduped: { cue: number; kills: KillfeedKill[] }[] = [];
  for (const pair of pairs) {
    const previous = deduped[deduped.length - 1];
    if (previous && previous.cue === pair.cue) {
      previous.kills = mergeKills(previous.kills, pair.kills);
      continue;
    }
    deduped.push({ cue: pair.cue, kills: pair.kills });
  }

  const next = { ...clip };
  if (deduped.length > 0) {
    next.killfeed_seconds = deduped.map((pair) => pair.cue);
  } else {
    delete next.killfeed_seconds;
  }
  if (deduped.some((pair) => pair.kills.length > 0)) {
    next.killfeed_kills = deduped.map((pair) => pair.kills);
  } else {
    delete next.killfeed_kills;
  }
  return next;
}

/**
 * Normalizes every clip's killfeed cues/kills, and when the plan has no
 * killfeed crop strips all cues and kills so a render never carries orphaned
 * killfeed data.
 */
export function normalizeKillfeedPlan(plan: StreamEditPlan): StreamEditPlan {
  const legacySnapshots = plan.schema_version === '' || plan.schema_version === '1.0';
  const clips = plan.clips.map((clip) => {
    if (!plan.killfeed_crop) {
      const withoutKillfeed = { ...clip };
      delete withoutKillfeed.killfeed_seconds;
      delete withoutKillfeed.killfeed_kills;
      return withoutKillfeed;
    }
    const normalized = normalizeClipKillfeed(clip);
    return legacySnapshots ? migrateCumulativeKillfeedSnapshots(normalized) : normalized;
  });
  return {
    ...plan,
    schema_version: legacySnapshots ? EDIT_PLAN_SCHEMA_VERSION : plan.schema_version,
    clips,
  };
}

function migrateCumulativeKillfeedSnapshots(clip: StreamClipRange): StreamClipRange {
  if (!clip.killfeed_kills) return clip;
  let previous: Set<string> | undefined;
  const events = clip.killfeed_kills.map((snapshot) => {
    if (snapshot.length === 0) return [];
    const current = new Set<string>();
    const delta: KillfeedKill[] = [];
    for (const kill of snapshot) {
      const identity = killIdentity(kill);
      if (current.has(identity)) continue;
      current.add(identity);
      if (!previous?.has(identity)) delta.push(kill);
    }
    previous = current;
    return delta;
  });
  return normalizeClipKillfeed({ ...clip, killfeed_kills: events });
}

/**
 * Fits only the historical fixed 20-second endpoint to a shorter probed
 * source. Custom overruns stay unchanged so the backend can report them
 * instead of the editor silently rewriting user-authored ranges. A legacy
 * clip wholly beyond EOF is dropped, and cue/kill pairs are normalized after
 * an endpoint is shortened.
 */
export function fitPlanToSourceDuration(
  plan: StreamEditPlan,
  durationSeconds: number,
): StreamEditPlan {
  if (!Number.isFinite(durationSeconds) || durationSeconds <= 0) {
    return normalizeKillfeedPlan(plan);
  }
  const clips = plan.clips.flatMap((clip) => {
    const isLegacyEndpoint =
      Math.abs(clip.end_seconds - 20) <= 0.001 &&
      clip.end_seconds > durationSeconds + 0.001;
    if (!isLegacyEndpoint || !Number.isFinite(clip.start_seconds)) return [clip];
    if (clip.start_seconds >= durationSeconds) return [];
    return [{ ...clip, end_seconds: durationSeconds }];
  });
  return normalizeKillfeedPlan({ ...plan, clips });
}

/** Adds a killfeed cue with no kills yet, keeping cue/kill alignment. */
export function addClipCue(clip: StreamClipRange, cue: number): StreamClipRange {
  const cues = clip.killfeed_seconds ?? [];
  if (cues.includes(cue)) return normalizeClipKillfeed(clip);
  const kills = clip.killfeed_kills;
  return normalizeClipKillfeed({
    ...clip,
    killfeed_seconds: [...cues, cue],
    killfeed_kills: kills ? [...kills, []] : undefined,
  });
}

/** Removes a killfeed cue and its aligned kills entry by index. */
export function removeClipCue(clip: StreamClipRange, cue: number): StreamClipRange {
  const cues = clip.killfeed_seconds ?? [];
  const index = cues.indexOf(cue);
  if (index < 0) return normalizeClipKillfeed(clip);
  return normalizeClipKillfeed({
    ...clip,
    killfeed_seconds: cues.filter((_value, i) => i !== index),
    killfeed_kills: (clip.killfeed_kills ?? []).filter((_value, i) => i !== index),
  });
}

/** Replaces the kills for the cue at `cue` in `clip`, keeping alignment. */
export function setClipCueKills(
  clip: StreamClipRange,
  cue: number,
  kills: KillfeedKill[],
): StreamClipRange {
  const cues = clip.killfeed_seconds ?? [];
  const index = cues.indexOf(cue);
  if (index < 0) return clip;
  const nextKills = cues.map((_value, i) => (i === index ? kills : clip.killfeed_kills?.[i] ?? []));
  return normalizeClipKillfeed({ ...clip, killfeed_kills: nextKills });
}

/**
 * Replaces one requested snapshot cue with the source events recovered by the
 * server's visual timeline scan. Existing cumulative copies of those same kills
 * inside the scanned interval are removed, then near-identical event times are
 * merged so repeated AI reads remain idempotent.
 */
export function applyClipKillfeedRead(
  clip: StreamClipRange,
  requestedCue: number,
  events: KillfeedReadEvent[],
): StreamClipRange {
  const validEvents = events.filter(
    (event) =>
      Number.isFinite(event.cue_seconds) &&
      event.cue_seconds >= clip.start_seconds &&
      event.cue_seconds < clip.end_seconds,
  );
  if (validEvents.length === 0) return clip;

  const readIdentities = new Set(validEvents.flatMap((event) => event.kills.map(killIdentity)));
  const intervalStart = Math.min(requestedCue, ...validEvents.map((event) => event.cue_seconds)) - READ_CUE_MERGE_SECONDS;
  const intervalEnd = Math.max(requestedCue, ...validEvents.map((event) => event.cue_seconds)) + READ_CUE_MERGE_SECONDS;
  const cues = clip.killfeed_seconds ?? [];
  const pairs = cues.flatMap((cue, index) => {
    const pair = { cue, kills: clip.killfeed_kills?.[index] ?? [] };
    if (cue < intervalStart || cue > intervalEnd) return [pair];
    const isRequestedCue = Math.abs(cue - requestedCue) <= Number.EPSILON;
    if (isRequestedCue && readIdentities.size === 0) return [];
    if (pair.kills.length === 0) {
      return isRequestedCue ? [] : [pair];
    }
    const remainingKills = pair.kills.filter((kill) => !readIdentities.has(killIdentity(kill)));
    return remainingKills.length > 0 ? [{ ...pair, kills: remainingKills }] : [];
  });

  for (const event of validEvents) {
    const existing = pairs.find(
      (pair) => Math.abs(pair.cue - event.cue_seconds) <= READ_CUE_MERGE_SECONDS,
    );
    if (existing) {
      existing.kills = mergeKills(existing.kills, event.kills);
      continue;
    }
    pairs.push({ cue: event.cue_seconds, kills: event.kills });
  }

  return normalizeClipKillfeed({
    ...clip,
    killfeed_seconds: pairs.map((pair) => pair.cue),
    killfeed_kills: pairs.map((pair) => pair.kills),
  });
}
