export const NEWS_VOICE_PROFILE_ID = 'raizerinhocs2' as const;
export const NEWS_VOICE_PROFILE_URL = '/api/news/voice-profile' as const;
export const NEWS_DRAFT_STORAGE_KEY = 'fragforge:news-short:draft:v1' as const;

export type NewsVoiceProfile = {
  id: string;
  name: string;
  channel: string;
  locale: string;
  source_file_name: string;
  content_type: string;
  size_bytes: number;
  sha256: string;
  created_at: string;
  updated_at: string;
  audio_url: string;
};

export type NewsShortDraft = {
  sourceUrl: string;
  channel: string;
  title: string;
  hook: string;
  script: string;
  updatedAt: string;
};

async function responseError(res: Response): Promise<Error> {
  const body = (await res.json().catch(() => null)) as { error?: unknown } | null;
  return new Error(body && typeof body.error === 'string' ? body.error : `request failed (${res.status})`);
}

function isNewsVoiceProfile(value: unknown): value is NewsVoiceProfile {
  if (value === null || typeof value !== 'object') return false;
  const profile = value as Partial<NewsVoiceProfile>;
  return typeof profile.id === 'string'
    && typeof profile.name === 'string'
    && typeof profile.channel === 'string'
    && typeof profile.locale === 'string'
    && typeof profile.source_file_name === 'string'
    && typeof profile.content_type === 'string'
    && typeof profile.size_bytes === 'number'
    && typeof profile.sha256 === 'string'
    && typeof profile.created_at === 'string'
    && typeof profile.updated_at === 'string'
    && typeof profile.audio_url === 'string';
}

export async function loadNewsVoiceProfile(): Promise<NewsVoiceProfile | null> {
  const res = await fetch(NEWS_VOICE_PROFILE_URL, { cache: 'no-store' });
  if (res.status === 404) return null;
  if (!res.ok) throw await responseError(res);
  const body = (await res.json()) as unknown;
  if (!isNewsVoiceProfile(body)) throw new Error('invalid voice profile response');
  return body;
}

export async function saveNewsVoiceProfile(file: File, channel: string): Promise<NewsVoiceProfile> {
  const form = new FormData();
  form.append('voice', file, file.name);
  form.append('name', 'Mi voz');
  form.append('channel', channel);
  form.append('locale', 'es-ES');
  const res = await fetch(NEWS_VOICE_PROFILE_URL, { method: 'PUT', body: form });
  if (!res.ok) throw await responseError(res);
  const body = (await res.json()) as unknown;
  if (!isNewsVoiceProfile(body)) throw new Error('invalid voice profile response');
  return body;
}

export async function deleteNewsVoiceProfile(): Promise<void> {
  const res = await fetch(NEWS_VOICE_PROFILE_URL, { method: 'DELETE' });
  if (!res.ok) throw await responseError(res);
}

export function loadNewsDraft(storage: Pick<Storage, 'getItem'>): NewsShortDraft | null {
  try {
    const raw = storage.getItem(NEWS_DRAFT_STORAGE_KEY);
    if (raw === null) return null;
    const value = JSON.parse(raw) as unknown;
    if (value === null || typeof value !== 'object') return null;
    const draft = value as Partial<NewsShortDraft>;
    if (
      typeof draft.sourceUrl !== 'string'
      || typeof draft.channel !== 'string'
      || typeof draft.title !== 'string'
      || typeof draft.hook !== 'string'
      || typeof draft.script !== 'string'
      || typeof draft.updatedAt !== 'string'
    ) return null;
    return {
      sourceUrl: draft.sourceUrl,
      channel: draft.channel,
      title: draft.title,
      hook: draft.hook,
      script: draft.script,
      updatedAt: draft.updatedAt,
    };
  } catch {
    return null;
  }
}

export function saveNewsDraft(storage: Pick<Storage, 'setItem'>, draft: NewsShortDraft): boolean {
  try {
    storage.setItem(NEWS_DRAFT_STORAGE_KEY, JSON.stringify(draft));
    return true;
  } catch {
    return false;
  }
}
