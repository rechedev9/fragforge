export const PUBLISH_ASSISTANT_SCHEMA_VERSION = '1.0';
export const PUBLISH_ASSISTANT_TIME_ZONE = 'Europe/Madrid';
export const YOUTUBE_STUDIO_URL = 'https://studio.youtube.com/';

export type PublishMetadata = {
  title: string;
  description: string;
  tags: string[];
};

export type PublishRecommendation = {
  title: string;
  description: string;
  keywords: string[];
  tags: string[];
  score: number;
  rationale: string;
};

export type PublishScheduleSource = {
  title: string;
  url: string;
};

export type PublishScheduleSlot = {
  publishAt: string;
  localTime: string;
  source: 'baseline';
  confidence: number;
  score: number;
  rationale: string;
};

export type PublishScheduleDay = {
  date: string;
  weekday: string;
  slots: PublishScheduleSlot[];
};

export type PublishSchedule = {
  timeZone: typeof PUBLISH_ASSISTANT_TIME_ZONE;
  generatedAt: string;
  days: PublishScheduleDay[];
  sources: PublishScheduleSource[];
  caveat: string;
};

export type PublishTrends = {
  available: boolean;
  terms: string[];
  fetchedAt?: string;
  sources: PublishScheduleSource[];
  reason?: string;
};

export type PublishAssistant = {
  schemaVersion: typeof PUBLISH_ASSISTANT_SCHEMA_VERSION;
  metadata: PublishMetadata;
  recommendations: PublishRecommendation[];
  keywords: string[];
  tags: string[];
  schedule: PublishSchedule;
  trends: PublishTrends;
  studioUrl: typeof YOUTUBE_STUDIO_URL;
};

export type UpcomingPublishSlot = {
  day: PublishScheduleDay;
  slot: PublishScheduleSlot;
};

/** Blocks browser cross-site reads before the Next proxy can trigger Firecrawl. */
export function isCrossSitePublishAssistantRequest(request: Request): boolean {
  const site = request.headers.get('sec-fetch-site') ?? '';
  if (site !== '' && site !== 'same-origin' && site !== 'none') return true;
  const origin = request.headers.get('origin');
  if (site !== '' || !origin) return false;
  try {
    return new URL(origin).origin !== new URL(request.url).origin;
  } catch {
    return true;
  }
}

type UnknownRecord = Record<string, unknown>;

function record(value: unknown): UnknownRecord | undefined {
  return value !== null && typeof value === 'object' && !Array.isArray(value)
    ? (value as UnknownRecord)
    : undefined;
}

function text(value: unknown): string | undefined {
  return typeof value === 'string' && value.trim() !== '' ? value : undefined;
}

function stringValue(value: unknown): string | undefined {
  return typeof value === 'string' ? value : undefined;
}

function stringList(value: unknown): string[] | undefined {
  if (!Array.isArray(value)) return undefined;
  const result: string[] = [];
  for (const item of value) {
    const candidate = text(item);
    if (!candidate) return undefined;
    result.push(candidate);
  }
  return result;
}

function finiteNumber(value: unknown): number | undefined {
  return typeof value === 'number' && Number.isFinite(value) ? value : undefined;
}

function isoInstant(value: unknown): string | undefined {
  const candidate = text(value);
  return candidate && !Number.isNaN(Date.parse(candidate)) ? candidate : undefined;
}

function httpURL(value: unknown): string | undefined {
  const candidate = text(value);
  if (!candidate) return undefined;
  try {
    const url = new URL(candidate);
    return url.protocol === 'http:' || url.protocol === 'https:' ? url.href : undefined;
  } catch {
    return undefined;
  }
}

function sourceLinks(value: unknown): PublishScheduleSource[] | undefined {
  if (!Array.isArray(value)) return undefined;
  const result: PublishScheduleSource[] = [];
  for (const item of value) {
    const source = record(item);
    const title = source ? text(source.title) : undefined;
    const url = source ? httpURL(source.url) : undefined;
    if (!title || !url) return undefined;
    result.push({ title, url });
  }
  return result;
}

function metadataFrom(value: unknown): PublishMetadata | undefined {
  const source = record(value);
  const title = source ? text(source.title) : undefined;
  const description = source ? stringValue(source.description) : undefined;
  const tags = source ? stringList(source.tags) : undefined;
  return title && description !== undefined && tags ? { title, description, tags } : undefined;
}

function recommendationsFrom(value: unknown): PublishRecommendation[] | undefined {
  if (!Array.isArray(value) || value.length < 3 || value.length > 5) return undefined;
  const result: PublishRecommendation[] = [];
  for (const item of value) {
    const source = record(item);
    const title = source ? text(source.title) : undefined;
    const description = source ? stringValue(source.description) : undefined;
    const keywords = source ? stringList(source.keywords) : undefined;
    const tags = source ? stringList(source.tags) : undefined;
    const score = source ? finiteNumber(source.score) : undefined;
    const rationale = source ? text(source.rationale) : undefined;
    if (!title || description === undefined || !keywords || !tags || score === undefined || !rationale) {
      return undefined;
    }
    result.push({ title, description, keywords, tags, score, rationale });
  }
  return result;
}

function slotsFrom(value: unknown): PublishScheduleSlot[] | undefined {
  if (!Array.isArray(value) || value.length === 0) return undefined;
  const result: PublishScheduleSlot[] = [];
  for (const item of value) {
    const source = record(item);
    const publishAt = source ? isoInstant(source.publish_at) : undefined;
    const localTime = source ? text(source.local_time) : undefined;
    const confidence = source ? finiteNumber(source.confidence) : undefined;
    const score = source ? finiteNumber(source.score) : undefined;
    const rationale = source ? text(source.rationale) : undefined;
    if (
      !publishAt ||
      !localTime ||
      !/^([01]\d|2[0-3]):[0-5]\d$/.test(localTime) ||
      source?.source !== 'baseline' ||
      confidence === undefined ||
      confidence < 0 ||
      confidence > 1 ||
      score === undefined ||
      score < 0 ||
      score > 1 ||
      !rationale
    ) {
      return undefined;
    }
    result.push({ publishAt, localTime, source: 'baseline', confidence, score, rationale });
  }
  return result;
}

function daysFrom(value: unknown): PublishScheduleDay[] | undefined {
  if (!Array.isArray(value) || value.length === 0) return undefined;
  const result: PublishScheduleDay[] = [];
  for (const item of value) {
    const source = record(item);
    const date = source ? text(source.date) : undefined;
    const weekday = source ? text(source.weekday) : undefined;
    const slots = source ? slotsFrom(source.slots) : undefined;
    if (!date || !/^\d{4}-\d{2}-\d{2}$/.test(date) || !weekday || !slots) return undefined;
    result.push({ date, weekday, slots });
  }
  return result;
}

function scheduleFrom(value: unknown): PublishSchedule | undefined {
  const source = record(value);
  if (!source || source.time_zone !== PUBLISH_ASSISTANT_TIME_ZONE) return undefined;
  const generatedAt = isoInstant(source.generated_at);
  const days = daysFrom(source.days);
  const sources = sourceLinks(source.sources);
  const caveat = text(source.caveat);
  return generatedAt && days && sources && caveat
    ? { timeZone: PUBLISH_ASSISTANT_TIME_ZONE, generatedAt, days, sources, caveat }
    : undefined;
}

function trendsFrom(value: unknown): PublishTrends | undefined {
  const source = record(value);
  if (!source || typeof source.available !== 'boolean') return undefined;
  const terms = stringList(source.terms);
  const sources = source.sources === undefined ? [] : sourceLinks(source.sources);
  if (!terms || !sources) return undefined;
  const trends: PublishTrends = { available: source.available, terms, sources };
  if (source.fetched_at !== undefined) {
    const fetchedAt = isoInstant(source.fetched_at);
    if (!fetchedAt) return undefined;
    trends.fetchedAt = fetchedAt;
  }
  const reason = text(source.reason);
  if (reason) trends.reason = reason;
  return trends;
}

export function parsePublishAssistant(value: unknown): PublishAssistant {
  const source = record(value);
  const metadata = source ? metadataFrom(source.metadata) : undefined;
  const recommendations = source ? recommendationsFrom(source.recommendations) : undefined;
  const keywords = source ? stringList(source.keywords) : undefined;
  const tags = source ? stringList(source.tags) : undefined;
  const schedule = source ? scheduleFrom(source.schedule) : undefined;
  const trends = source ? trendsFrom(source.trends) : undefined;
  if (
    source?.schema_version !== PUBLISH_ASSISTANT_SCHEMA_VERSION ||
    source.studio_url !== YOUTUBE_STUDIO_URL ||
    !metadata ||
    !recommendations ||
    !keywords ||
    !tags ||
    !schedule ||
    !trends
  ) {
    throw new Error('invalid publish assistant response');
  }
  return {
    schemaVersion: PUBLISH_ASSISTANT_SCHEMA_VERSION,
    metadata,
    recommendations,
    keywords,
    tags,
    schedule,
    trends,
    studioUrl: YOUTUBE_STUDIO_URL,
  };
}

/** Picks each day's highest-ranked slot that has not passed. */
export function upcomingPublishSlots(schedule: PublishSchedule, now = Date.now()): UpcomingPublishSlot[] {
  const days = [...schedule.days].sort((left, right) => left.date.localeCompare(right.date));
  const result: UpcomingPublishSlot[] = [];
  for (const day of days) {
    const slot = day.slots.find((candidate) => Date.parse(candidate.publishAt) >= now);
    if (slot) result.push({ day, slot });
  }
  return result;
}
