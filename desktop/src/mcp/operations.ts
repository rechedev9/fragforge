import { isJsonObject, type JsonObject, type JsonValue } from './json.ts';
import { OrchestratorClient } from './orchestrator-client.ts';

export type OperationRisk = 'costly' | 'destructive' | 'read' | 'write';

export interface OperationDefinition {
  category: 'artifacts' | 'catalog' | 'jobs' | 'renders' | 'streams' | 'studio' | 'voices';
  description: string;
  inputSchema: JsonObject;
  keywords: readonly string[];
  name: string;
  preview: (input: JsonObject) => JsonObject;
  risk: OperationRisk;
  run: (client: OrchestratorClient, input: JsonObject, signal?: AbortSignal) => Promise<JsonValue>;
  title: string;
}

export class MissingOperationInputError extends Error {
  readonly field: string;

  constructor(field: string, message = `arguments.${field} is required`) {
    super(message);
    this.field = field;
    this.name = 'MissingOperationInputError';
  }
}

const SAFE_TOKEN_PATTERN = '^[A-Za-z0-9][A-Za-z0-9_-]*$';
const MAX_LIVE_VARIANT_NAME_LENGTH = 128;
const MAX_LIVE_VARIANT_NAMES_IN_ERROR = 20;
const UUID_PATTERN = '^[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}$';
const UUID_PROPERTY: JsonObject = {
  description: 'FragForge job UUID discovered through search.',
  pattern: UUID_PATTERN,
  type: 'string',
};
const GENERATION_ID_PROPERTY: JsonObject = {
  description: 'Caption or killfeed generation UUID returned by the corresponding read operation.',
  pattern: UUID_PATTERN,
  type: 'string',
};
const SAFE_TOKEN_PROPERTY: JsonObject = {
  pattern: SAFE_TOKEN_PATTERN,
  type: 'string',
};
const VOICE_PROFILE_ID_PROPERTY: JsonObject = {
  description: 'Stable local voice profile ID using lowercase letters, numbers, and hyphens.',
  maxLength: 64,
  minLength: 1,
  pattern: '^[a-z0-9-]+$',
  type: 'string',
};
const VARIANT_PROPERTY: JsonObject = {
  description: 'Render variant discovered from the live preset/loadout catalog.',
  pattern: SAFE_TOKEN_PATTERN,
  type: 'string',
};
const EDIT_PROPERTY: JsonObject = {
  additionalProperties: false,
  description: 'Optional deterministic reel edit. Omitted fields use the product defaults.',
  properties: {
    format: { enum: ['short-9x16', 'landscape-16x9'], type: 'string' },
    hook_text: { type: 'boolean' },
    intro: { type: 'boolean' },
    intro_text: { maxLength: 80, type: 'string' },
    kill_counter: { type: 'boolean' },
    killEffect: { enum: ['clean', 'punch-in', 'velocity', 'freeze-flash'], type: 'string' },
    outro: { type: 'boolean' },
    outro_text: { maxLength: 80, type: 'string' },
    transition: { enum: ['cut', 'flash', 'whip', 'dip'], type: 'string' },
  },
  type: 'object',
};
const CROP_RECT_PROPERTY: JsonObject = {
  additionalProperties: false,
  properties: {
    height: { exclusiveMinimum: 0, maximum: 1, type: 'number' },
    width: { exclusiveMinimum: 0, maximum: 1, type: 'number' },
    x: { maximum: 1, minimum: 0, type: 'number' },
    y: { maximum: 1, minimum: 0, type: 'number' },
  },
  required: ['x', 'y', 'width', 'height'],
  type: 'object',
};
const FACE_CROP_RECT_PROPERTY: JsonObject = {
  additionalProperties: false,
  description: 'Normalized face crop. Live stream variant metadata determines whether this crop is required.',
  properties: {
    height: { maximum: 1, minimum: 0, type: 'number' },
    width: { maximum: 1, minimum: 0, type: 'number' },
    x: { maximum: 1, minimum: 0, type: 'number' },
    y: { maximum: 1, minimum: 0, type: 'number' },
  },
  required: ['x', 'y', 'width', 'height'],
  type: 'object',
};
const KILLFEED_KILL_PROPERTY: JsonObject = {
  additionalProperties: false,
  description: 'One confirmed kill notice rendered as a synthetic killfeed entry.',
  properties: {
    assister_name: { type: 'string' },
    assister_side: { enum: ['CT', 'T'], type: 'string' },
    attacker_name: { minLength: 1, type: 'string' },
    attacker_side: { enum: ['CT', 'T'], type: 'string' },
    blind: { type: 'boolean' },
    flash_assist: { type: 'boolean' },
    headshot: { type: 'boolean' },
    in_air: { type: 'boolean' },
    noscope: { type: 'boolean' },
    smoke: { type: 'boolean' },
    victim_name: { minLength: 1, type: 'string' },
    victim_side: { enum: ['CT', 'T'], type: 'string' },
    wallbang: { type: 'boolean' },
    weapon: { description: 'Weapon icon catalog key such as ak47.', minLength: 1, type: 'string' },
  },
  required: ['attacker_side', 'attacker_name', 'victim_side', 'victim_name', 'weapon'],
  type: 'object',
};
const CAPTION_WORD_PROPERTY: JsonObject = {
  additionalProperties: false,
  description: 'One reviewed word timed relative to the start of its stream clip.',
  properties: {
    end_seconds: { exclusiveMinimum: 0, type: 'number' },
    start_seconds: { minimum: 0, type: 'number' },
    word: { maxLength: 80, minLength: 1, type: 'string' },
  },
  required: ['word', 'start_seconds', 'end_seconds'],
  type: 'object',
};
const STREAM_CAPTION_REVIEW_CLIP_PROPERTY: JsonObject = {
  additionalProperties: false,
  description: 'One explicit review decision: provide reviewed words or set no_speech=true.',
  properties: {
    clip_id: SAFE_TOKEN_PROPERTY,
    no_speech: { type: 'boolean' },
    words: { items: CAPTION_WORD_PROPERTY, type: 'array' },
  },
  required: ['clip_id'],
  type: 'object',
};
// Vertical-center bounds shared by the streamer banner and text overlays,
// mirroring min/maxVerticalPositionY in internal/streamclips/types.go.
const POSITION_Y_PROPERTY: JsonObject = { maximum: 0.975, minimum: 0.025, type: 'number' };
const TEXT_OVERLAY_PROPERTY: JsonObject = {
  additionalProperties: false,
  description: 'One burned-in text line. Times are relative to the clip start in source seconds; omitted bounds extend to the clip edges.',
  properties: {
    end_seconds: { exclusiveMinimum: 0, type: 'number' },
    font_size: { description: 'Output pixels; omit for the default 64.', maximum: 120, minimum: 24, type: 'integer' },
    position_y: { ...POSITION_Y_PROPERTY, description: 'Normalized vertical center of the text line.' },
    start_seconds: { minimum: 0, type: 'number' },
    text: { maxLength: 120, minLength: 1, type: 'string' },
  },
  required: ['text', 'position_y'],
  type: 'object',
};
// Shared between the edit plan's clips[].edit object and streams.edit_clip.
// Bounds mirror the clip-edit constants in internal/streamclips/types.go
// (min/maxClipSpeed, maxSourceVolume, maxClipFadeSeconds, font sizes).
const CLIP_EDIT_OPTION_PROPERTIES: JsonObject = {
  fade_in_seconds: { description: 'Fade from black/silence at the clip start, in output (post-speed) seconds.', maximum: 5, minimum: 0, type: 'number' },
  fade_out_seconds: { description: 'Fade to black/silence at the clip end, in output (post-speed) seconds.', maximum: 5, minimum: 0, type: 'number' },
  source_volume: { description: 'Original-audio gain: 0 mutes, 1 keeps, up to 2 boosts. Music keeps its own volume.', maximum: 2, minimum: 0, type: 'number' },
  speed: { description: 'Playback rate; 1 keeps real time.', maximum: 3, minimum: 0.25, type: 'number' },
  text_overlays: { items: TEXT_OVERLAY_PROPERTY, maxItems: 4, type: 'array' },
};
const CLIP_EDIT_PROPERTY: JsonObject = {
  additionalProperties: false,
  description: 'Optional per-clip edit options: playback speed, original-audio volume, boundary fades, and burned-in text overlays. Omit for an untouched clip.',
  properties: CLIP_EDIT_OPTION_PROPERTIES,
  type: 'object',
};
const STREAM_EDIT_PLAN_PROPERTY: JsonObject = {
  additionalProperties: false,
  description: 'Complete stream edit plan. Search with stream_job_id to retrieve the current plan before replacing it.',
  properties: {
    captions: {
      additionalProperties: false,
      description: 'Burned-in subtitles generated through the configured xAI speech-to-text capability.',
      properties: {
        enabled: { type: 'boolean' },
        language: { description: 'Subtitle output language. FragForge currently supports Spanish only.', enum: ['es'], type: 'string' },
      },
      required: ['enabled'],
      type: 'object',
    },
    clips: {
      items: {
        additionalProperties: false,
        properties: {
          edit: CLIP_EDIT_PROPERTY,
          end_seconds: { exclusiveMinimum: 0, type: 'number' },
          id: SAFE_TOKEN_PROPERTY,
          killfeed_kills: {
            description: 'Per-cue confirmed kills, index-aligned with killfeed_seconds. A cue with an empty entry keeps the frozen-crop behavior; a cue with kills renders synthetic notices.',
            items: { items: KILLFEED_KILL_PROPERTY, type: 'array' },
            type: 'array',
          },
          killfeed_seconds: {
            description: 'Absolute source timestamps whose selected killfeed notice should be frozen for this clip.',
            items: { type: 'number' },
            type: 'array',
          },
          start_seconds: { minimum: 0, type: 'number' },
          title: { type: 'string' },
        },
        required: ['id', 'start_seconds', 'end_seconds'],
        type: 'object',
      },
      type: 'array',
    },
    effects: {
      additionalProperties: false,
      properties: { grade: { type: 'boolean' } },
      type: 'object',
    },
    face_crop: FACE_CROP_RECT_PROPERTY,
    gameplay_crop: CROP_RECT_PROPERTY,
    killfeed_crop: {
      ...CROP_RECT_PROPERTY,
      description: 'Normalized source crop for frozen killfeed notices. Required when a clip contains killfeed_seconds cues.',
    },
    music: {
      additionalProperties: false,
      properties: {
        key: { pattern: '^[a-z0-9][a-z0-9-]*$', type: 'string' },
        volume: { maximum: 1, minimum: 0, type: 'number' },
      },
      type: 'object',
    },
    schema_version: { description: 'Current edit plans use 1.1; persisted 1.0 plans are migrated by the API.', enum: ['1.0', '1.1'], type: 'string' },
    streamer_banner: {
      additionalProperties: false,
      properties: {
        nick: { pattern: '^[A-Za-z0-9_]{1,25}$', type: 'string' },
        position_y: POSITION_Y_PROPERTY,
        slide_enabled: { type: 'boolean' },
      },
      type: 'object',
    },
    updated_at: { description: 'Server timestamp; preserved from the current plan or omitted.', type: 'string' },
    variant: VARIANT_PROPERTY,
  },
  required: ['gameplay_crop', 'variant'],
  type: 'object',
};
const RULES_PROPERTY: JsonObject = {
  additionalProperties: false,
  description: 'Complete parser rules object. Omit the whole field to use product defaults; partial objects are not accepted by the HTTP API.',
  properties: {
    exclude_team_kills: { type: 'boolean' },
    include_headshot_only: { type: 'boolean' },
    max_round: { minimum: 0, type: 'integer' },
    min_kills_in_window: { minimum: 1, type: 'integer' },
    min_round: { minimum: 1, type: 'integer' },
    post_roll_seconds: { minimum: 0, type: 'integer' },
    pre_roll_seconds: { minimum: 0, type: 'integer' },
    weapons: { items: { type: 'string' }, minItems: 1, type: 'array' },
    window_seconds: { minimum: 0, type: 'integer' },
  },
  required: [
    'weapons',
    'min_kills_in_window',
    'window_seconds',
    'pre_roll_seconds',
    'post_roll_seconds',
    'include_headshot_only',
    'exclude_team_kills',
    'min_round',
    'max_round',
  ],
  type: 'object',
};

function objectSchema(properties: JsonObject, required: readonly string[] = []): JsonObject {
  return {
    additionalProperties: false,
    properties,
    required: [...required],
    type: 'object',
  };
}

function readOperation(options: {
  category: OperationDefinition['category'];
  description: string;
  inputSchema?: JsonObject;
  keywords?: readonly string[];
  name: string;
  path: (input: JsonObject) => string;
  title: string;
}): OperationDefinition {
  return {
    category: options.category,
    description: options.description,
    inputSchema: options.inputSchema ?? objectSchema({}),
    keywords: options.keywords ?? [],
    name: options.name,
    preview: (input) => requestPreview('GET', options.path(input)),
    risk: 'read',
    run: (client, input, signal) => client.request({ path: options.path(input), signal }),
    title: options.title,
  };
}

function mutationOperation(options: {
  body?: (input: JsonObject) => JsonValue | undefined;
  category: OperationDefinition['category'];
  description: string;
  inputSchema: JsonObject;
  keywords?: readonly string[];
    method?: 'DELETE' | 'GET' | 'POST' | 'PUT';
  name: string;
  path: (input: JsonObject) => string;
  risk?: Exclude<OperationRisk, 'read'>;
  title: string;
}): OperationDefinition {
  const method = options.method ?? 'POST';
  return {
    category: options.category,
    description: options.description,
    inputSchema: options.inputSchema,
    keywords: options.keywords ?? [],
    name: options.name,
    preview: (input) => requestPreview(method, options.path(input), options.body?.(input)),
    risk: options.risk ?? 'write',
    run: (client, input, signal) =>
      client.request({
        body: options.body?.(input),
        method,
        path: options.path(input),
        signal,
      }),
    title: options.title,
  };
}

function requestPreview(method: string, path: string, body?: JsonValue): JsonObject {
  const preview: JsonObject = { method, path };
  if (body !== undefined) preview.body = body;
  return preview;
}

function stringInput(input: JsonObject, key: string): string {
  const value = input[key];
  if (typeof value !== 'string') throw new Error(`${key} must be a string`);
  return value;
}

function optionalStringInput(input: JsonObject, key: string): string | undefined {
  const value = input[key];
  return typeof value === 'string' ? value : undefined;
}

function jobPath(input: JsonObject, suffix = ''): string {
  return `/api/jobs/${encodeURIComponent(stringInput(input, 'job_id'))}${suffix}`;
}

function renderPath(input: JsonObject, suffix = ''): string {
  const variant = encodeURIComponent(stringInput(input, 'variant'));
  return jobPath(input, `/renders/${variant}${suffix}`);
}

function streamPath(input: JsonObject, suffix = ''): string {
  return `/api/stream-jobs/${encodeURIComponent(stringInput(input, 'stream_job_id'))}${suffix}`;
}

function streamRenderPath(input: JsonObject, suffix = ''): string {
  const variant = encodeURIComponent(stringInput(input, 'variant'));
  return streamPath(input, `/renders/${variant}${suffix}`);
}

function voiceProfilePath(input: JsonObject, suffix = ''): string {
  return `/api/voice-profiles/${encodeURIComponent(stringInput(input, 'voice_profile_id'))}${suffix}`;
}

function without(input: JsonObject, ...keys: readonly string[]): JsonObject {
  const excluded = new Set(keys);
  return Object.fromEntries(Object.entries(input).filter(([key]) => !excluded.has(key)));
}

const JOB_ID_SCHEMA = objectSchema({ job_id: UUID_PROPERTY }, ['job_id']);
const VOICE_PROFILE_SCHEMA = objectSchema({ voice_profile_id: VOICE_PROFILE_ID_PROPERTY }, ['voice_profile_id']);
const JOB_VARIANT_SCHEMA = objectSchema(
  {
    job_id: UUID_PROPERTY,
    variant: VARIANT_PROPERTY,
  },
  ['job_id', 'variant'],
);
const STREAM_JOB_SCHEMA = objectSchema({ stream_job_id: UUID_PROPERTY }, ['stream_job_id']);
const STREAM_CAPTION_REVIEW_SCHEMA = objectSchema({
  clips: { items: STREAM_CAPTION_REVIEW_CLIP_PROPERTY, minItems: 1, type: 'array' },
  generation_id: GENERATION_ID_PROPERTY,
  stream_job_id: UUID_PROPERTY,
}, ['stream_job_id', 'generation_id', 'clips']);
const STREAM_GENERATION_SCHEMA = objectSchema({
  generation_id: GENERATION_ID_PROPERTY,
  stream_job_id: UUID_PROPERTY,
}, ['stream_job_id', 'generation_id']);
const STREAM_VARIANT_SCHEMA = objectSchema(
  {
    stream_job_id: UUID_PROPERTY,
    variant: {
      description: 'Stream layout variant. Search returns the currently supported choices.',
      pattern: SAFE_TOKEN_PATTERN,
      type: 'string',
    },
  },
  ['stream_job_id', 'variant'],
);

const operations: readonly OperationDefinition[] = [
  {
    category: 'studio',
    description: 'Check whether FragForge Studio is online and inspect capture/render readiness without exposing local tool paths.',
    inputSchema: objectSchema({}),
    keywords: ['health', 'capabilities', 'ready', 'offline', 'hlae', 'ffmpeg', 'captions', 'subtitles', 'subtitulos', 'xai', 'grok'],
    name: 'studio.status',
    preview: () => ({ method: 'GET', paths: ['/healthz', '/api/capabilities'] }),
    risk: 'read',
    run: async (client, _input, signal) => {
      const [health, capabilities] = await Promise.all([
        client.request({ path: '/healthz', signal }),
        client.request({ path: '/api/capabilities', signal }),
      ]);
      return {
        capabilities: redactCapabilityPaths(capabilities),
        health,
        orchestrator_url: await client.baseUrl(),
      };
    },
    title: 'Studio status',
  },
  {
    category: 'studio',
    description: 'Read local pipeline and error counters in Prometheus text format.',
    inputSchema: objectSchema({}),
    keywords: ['metrics', 'prometheus', 'observability', 'errors', 'failures', 'metricas', 'errores'],
    name: 'studio.metrics',
    preview: () => requestPreview('GET', '/metrics'),
    risk: 'read',
    run: async (client, _input, signal) => ({ format: 'prometheus', text: await client.requestText('/metrics', signal) }),
    title: 'Read Studio metrics',
  },
  readOperation({
    category: 'catalog',
    description: 'List the supported render presets and the product default.',
    keywords: ['preset', 'viral-60-clean', 'format'],
    name: 'catalog.presets',
    path: () => '/api/presets',
    title: 'List render presets',
  }),
  readOperation({
    category: 'catalog',
    description: 'List render loadouts derived from the FragForge registry.',
    keywords: ['loadout', 'variant', 'effects'],
    name: 'catalog.loadouts',
    path: () => '/api/loadouts',
    title: 'List render loadouts',
  }),
  readOperation({
    category: 'catalog',
    description: 'List locally installed music tracks that can be selected by render operations.',
    keywords: ['music', 'song', 'audio', 'track'],
    name: 'catalog.songs',
    path: () => '/api/songs',
    title: 'List music tracks',
  }),
  readOperation({
    category: 'catalog',
    description: 'List stream/VOD layout variants derived from the live Go registry.',
    keywords: ['stream', 'twitch', 'layout', 'variant', 'facecam'],
    name: 'catalog.stream_variants',
    path: () => '/api/stream-variants',
    title: 'List stream layout variants',
  }),
  readOperation({
    category: 'voices',
    description: 'Read metadata for one locally stored voice reference without loading its audio into model context.',
    inputSchema: VOICE_PROFILE_SCHEMA,
    keywords: ['voice', 'profile', 'reference', 'voz', 'perfil'],
    name: 'voices.get_profile',
    path: (input) => voiceProfilePath(input),
    title: 'Get voice profile',
  }),
  {
    category: 'voices',
    description: 'Create or replace a local voice profile from a WAV or OGG reference file. The audio stays in FragForge local storage.',
    inputSchema: objectSchema({
      audio_path: { description: 'Absolute path to a local WAV or OGG voice reference, up to 25 MiB.', minLength: 1, type: 'string' },
      channel: { maxLength: 80, type: 'string' },
      locale: { maxLength: 20, type: 'string' },
      name: { maxLength: 80, type: 'string' },
      voice_profile_id: VOICE_PROFILE_ID_PROPERTY,
    }, ['voice_profile_id', 'audio_path']),
    keywords: ['voice', 'profile', 'reference', 'upload', 'save', 'voz', 'perfil', 'subir'],
    name: 'voices.save_profile',
    preview: (input) => ({
      fields: without(input, 'voice_profile_id', 'audio_path'),
      file: stringInput(input, 'audio_path'),
      method: 'PUT',
      path: voiceProfilePath(input),
    }),
    risk: 'write',
    run: (client, input, signal) => client.uploadVoiceProfile(
      stringInput(input, 'voice_profile_id'),
      stringInput(input, 'audio_path'),
      without(input, 'voice_profile_id', 'audio_path'),
      signal,
    ),
    title: 'Save voice profile',
  },
  mutationOperation({
    category: 'voices',
    description: 'Permanently delete one local voice profile and its reference audio.',
    inputSchema: VOICE_PROFILE_SCHEMA,
    keywords: ['voice', 'profile', 'delete', 'remove', 'voz', 'perfil', 'borrar', 'eliminar'],
    method: 'DELETE',
    name: 'voices.delete_profile',
    path: (input) => voiceProfilePath(input),
    risk: 'destructive',
    title: 'Delete voice profile',
  }),
  readOperation({
    category: 'jobs',
    description: 'List recent CS2 demo jobs and their current pipeline states.',
    inputSchema: objectSchema({ limit: { maximum: 100, minimum: 1, type: 'integer' } }),
    keywords: ['demos', 'matches', 'recent', 'pipeline'],
    name: 'jobs.list',
    path: (input) => `/api/jobs?limit=${input.limit ?? 50}`,
    title: 'List demo jobs',
  }),
  {
    category: 'jobs',
    description: 'Upload a local .dem file. With target_steamid it immediately parses; without it FragForge scans the roster first.',
    inputSchema: objectSchema(
      {
        demo_path: { description: 'Absolute path to a local CS2 .dem file.', minLength: 1, type: 'string' },
        rules: RULES_PROPERTY,
        target_steamid: { description: 'Optional SteamID64. Search the roster when unknown.', pattern: '^[0-9]{1,20}$', type: 'string' },
      },
      ['demo_path'],
    ),
    keywords: ['upload', 'demo', 'scan', 'create match'],
    name: 'jobs.create',
    preview: (input) => ({ file: stringInput(input, 'demo_path'), method: 'POST', path: '/api/jobs' }),
    risk: 'write',
    run: (client, input, signal) => client.uploadDemo(stringInput(input, 'demo_path'), without(input, 'demo_path'), signal),
    title: 'Create a demo job',
  },
  readOperation({ category: 'jobs', description: 'Get one demo job, including status and capture progress.', inputSchema: JOB_ID_SCHEMA, name: 'jobs.get', path: (input) => jobPath(input), title: 'Get a demo job' }),
  readOperation({ category: 'jobs', description: 'Get roster players discovered for a job that was uploaded without a SteamID.', inputSchema: JOB_ID_SCHEMA, keywords: ['players', 'steamid', 'target'], name: 'jobs.roster', path: (input) => jobPath(input, '/roster'), title: 'Get job roster' }),
  mutationOperation({
    body: (input) => without(input, 'job_id'),
    category: 'jobs',
    description: 'Select the target player after roster discovery and start deterministic demo parsing.',
    inputSchema: objectSchema({ job_id: UUID_PROPERTY, rules: RULES_PROPERTY, target_steamid: { pattern: '^[0-9]{1,20}$', type: 'string' } }, ['job_id', 'target_steamid']),
    keywords: ['player', 'parse', 'steamid'],
    name: 'jobs.parse',
    path: (input) => jobPath(input, '/parse'),
    title: 'Parse a demo job',
  }),
  readOperation({ category: 'jobs', description: 'Read the kill plan generated from the demo.', inputSchema: JOB_ID_SCHEMA, keywords: ['segments', 'ticks', 'kills'], name: 'jobs.plan', path: (input) => jobPath(input, '/plan'), title: 'Get kill plan' }),
  readOperation({ category: 'jobs', description: 'Read scored and reviewable moments derived from the kill plan.', inputSchema: JOB_ID_SCHEMA, keywords: ['highlights', 'segments', 'best kills'], name: 'jobs.moments', path: (input) => jobPath(input, '/moments'), title: 'Get scored moments' }),
  mutationOperation({
    body: (input) => without(input, 'job_id'),
    category: 'jobs',
    description: 'Start costly local HLAE/CS2 capture for selected segments. Requires Studio capture readiness.',
    inputSchema: objectSchema({ edit: EDIT_PROPERTY, job_id: UUID_PROPERTY, preset: VARIANT_PROPERTY, segment_ids: { items: { type: 'string' }, type: 'array' } }, ['job_id']),
    keywords: ['capture', 'hlae', 'cs2', 'record'],
    name: 'jobs.record',
    path: (input) => jobPath(input, '/record'),
    risk: 'costly',
    title: 'Record demo segments',
  }),
  mutationOperation({
    body: (input) => without(input, 'job_id'),
    category: 'jobs',
    description: 'Run guided capture and automatic render as one costly action.',
    inputSchema: objectSchema(
      { edit: EDIT_PROPERTY, job_id: UUID_PROPERTY, music: { type: 'string' }, preset: VARIANT_PROPERTY, segment_ids: { items: { type: 'string' }, type: 'array' } },
      ['job_id', 'preset'],
    ),
    keywords: ['one click', 'short', 'video', 'record render'],
    name: 'jobs.generate',
    path: (input) => jobPath(input, '/generate'),
    risk: 'costly',
    title: 'Generate a reel',
  }),
  mutationOperation({ category: 'jobs', description: 'Concatenate recorded segments into the legacy final artifact.', inputSchema: JOB_ID_SCHEMA, keywords: ['concat', 'final'], name: 'jobs.compose', path: (input) => jobPath(input, '/compose'), risk: 'costly', title: 'Compose final video' }),
  mutationOperation({
    category: 'jobs',
    description:
      'Permanently remove one analyzed demo job: its recordings, renders, and stored demo copy. Refused (409) while the job is still scanning, parsing, recording, or composing.',
    inputSchema: JOB_ID_SCHEMA,
    keywords: ['delete', 'remove', 'borrar', 'partida'],
    method: 'DELETE',
    name: 'jobs.delete',
    path: (input) => jobPath(input),
    risk: 'destructive',
    title: 'Delete demo job',
  }),
  readOperation({ category: 'renders', description: 'Get a render variant state and its real video/cover artifact names.', inputSchema: JOB_VARIANT_SCHEMA, keywords: ['video', 'ready', 'cover', 'progress'], name: 'renders.get', path: (input) => renderPath(input), title: 'Get render state' }),
  mutationOperation({
    body: (input) => without(input, 'job_id', 'variant'),
    category: 'renders',
    description: 'Start a render for a recorded job with an optional music track and edit request.',
    inputSchema: objectSchema({ edit: EDIT_PROPERTY, job_id: UUID_PROPERTY, music: { type: 'string' }, variant: VARIANT_PROPERTY }, ['job_id', 'variant']),
    keywords: ['short', 'render', 'viral-60-clean'],
    name: 'renders.start',
    path: (input) => renderPath(input),
    risk: 'costly',
    title: 'Start a render',
  }),
  readOperation({ category: 'renders', description: 'Read the publish readiness board for a render.', inputSchema: JOB_VARIANT_SCHEMA, keywords: ['publish', 'ready', 'manifest'], name: 'renders.publish', path: (input) => renderPath(input, '/publish'), title: 'Get publish board' }),
  readOperation({ category: 'renders', description: 'Read deterministic quality checks for a render.', inputSchema: JOB_VARIANT_SCHEMA, keywords: ['qa', 'quality', 'validate'], name: 'renders.quality', path: (input) => renderPath(input, '/quality'), title: 'Get render quality' }),
  mutationOperation({ category: 'renders', description: 'Start local caption/title/hashtag candidate generation for a render.', inputSchema: JOB_VARIANT_SCHEMA, keywords: ['captions', 'subtitles', 'subtitulos', 'title', 'hashtags', 'metadata'], name: 'renders.start_caption_agent', path: (input) => renderPath(input, '/agent/captions'), risk: 'costly', title: 'Start caption assistant' }),
  readOperation({ category: 'renders', description: 'Read completed caption/title/hashtag candidates.', inputSchema: JOB_VARIANT_SCHEMA, keywords: ['captions', 'title', 'hashtags'], name: 'renders.caption_candidates', path: (input) => renderPath(input, '/agent/captions'), title: 'Get caption candidates' }),
  mutationOperation({
    category: 'renders',
    description: 'Generate factual YouTube metadata and Madrid-time schedule guidance. This can perform a bounded external Firecrawl trend lookup when configured.',
    inputSchema: objectSchema({ days: { maximum: 14, minimum: 1, type: 'integer' }, job_id: UUID_PROPERTY, name: SAFE_TOKEN_PROPERTY, variant: VARIANT_PROPERTY }, ['job_id', 'variant', 'name']),
    keywords: ['youtube', 'publish', 'caption', 'schedule', 'firecrawl'],
    method: 'GET',
    name: 'renders.publish_assistant',
    path: (input) => `${renderPath(input, `/videos/${encodeURIComponent(stringInput(input, 'name'))}/publish-assistant`)}?days=${input.days ?? 7}`,
    risk: 'costly',
    title: 'Get publish assistant',
  }),
  mutationOperation({
    category: 'renders',
    description: 'Permanently remove one rendered video and its cover/caption artifacts from FragForge storage.',
    inputSchema: objectSchema({ job_id: UUID_PROPERTY, name: SAFE_TOKEN_PROPERTY, variant: VARIANT_PROPERTY }, ['job_id', 'variant', 'name']),
    method: 'DELETE',
    name: 'renders.delete_video',
    path: (input) => renderPath(input, `/videos/${encodeURIComponent(stringInput(input, 'name'))}`),
    risk: 'destructive',
    title: 'Delete rendered video',
  }),
  {
    category: 'artifacts',
    description: 'Return a loopback URL for a known final/render artifact without loading binary media into model context.',
    inputSchema: objectSchema(
      {
        job_id: UUID_PROPERTY,
        kind: { enum: ['final', 'gallery', 'pack', 'edit_document', 'video', 'cover', 'caption'], type: 'string' },
        name: { ...SAFE_TOKEN_PROPERTY, description: 'Required for video, cover, and caption.' },
        variant: { description: 'Required for every kind except final.', pattern: SAFE_TOKEN_PATTERN, type: 'string' },
      },
      ['job_id', 'kind'],
    ),
    keywords: ['download', 'url', 'mp4', 'cover', 'gallery'],
    name: 'artifacts.get_url',
    preview: (input) => ({ method: 'GET', path: artifactPath(input) }),
    risk: 'read',
    run: async (client, input, signal) => ({ url: await client.artifactUrl(artifactPath(input), signal) }),
    title: 'Get artifact URL',
  },
  {
    category: 'artifacts',
    description: 'Return a loopback URL for one locally installed music track without embedding audio in model context.',
    inputSchema: objectSchema({ song_id: { minLength: 1, type: 'string' } }, ['song_id']),
    keywords: ['music', 'song', 'audio', 'track', 'download', 'musica', 'cancion'],
    name: 'artifacts.get_song_url',
    preview: (input) => ({ method: 'GET', path: songArtifactPath(input) }),
    risk: 'read',
    run: async (client, input, signal) => ({ url: await client.artifactUrl(songArtifactPath(input), signal) }),
    title: 'Get music track URL',
  },
  {
    category: 'artifacts',
    description: 'Return a checked loopback URL for one local voice reference without embedding audio in model context.',
    inputSchema: VOICE_PROFILE_SCHEMA,
    keywords: ['voice', 'profile', 'audio', 'reference', 'download', 'voz', 'perfil'],
    name: 'artifacts.get_voice_profile_audio_url',
    preview: (input) => ({ method: 'GET', path: voiceProfilePath(input, '/audio') }),
    risk: 'read',
    run: async (client, input, signal) => ({ url: await client.artifactUrl(voiceProfilePath(input, '/audio'), signal) }),
    title: 'Get voice profile audio URL',
  },
  readOperation({
    category: 'streams',
    description: 'List recent Twitch/VOD stream clip jobs.',
    inputSchema: objectSchema({ limit: { maximum: 100, minimum: 1, type: 'integer' } }),
    keywords: ['twitch', 'vod', 'clips'],
    name: 'streams.list',
    path: (input) => `/api/stream-jobs?limit=${input.limit ?? 50}`,
    title: 'List stream jobs',
  }),
  {
    category: 'streams',
    description: 'Upload a local stream video and create a clip job. If the request is cancelled, list recent streams and inspect the matching job before retrying so an accepted upload is not duplicated.',
    inputSchema: objectSchema({ title: { type: 'string' }, video_path: { minLength: 1, type: 'string' } }, ['video_path']),
    keywords: ['upload', 'twitch', 'vod', 'mp4'],
    name: 'streams.create_from_file',
    preview: (input) => ({
      file: stringInput(input, 'video_path'),
      steps: [
        { method: 'POST', path: '/api/stream-jobs', purpose: 'upload video and create the stream job' },
        { method: 'GET', path: '/api/stream-jobs/{created_id}/edit-plan', purpose: 'load the deterministic default edit plan' },
        { method: 'PUT', path: '/api/stream-jobs/{created_id}/edit-plan', purpose: 'persist the plan and transition the job to ready' },
        { method: 'GET', path: '/api/stream-jobs/{created_id}', purpose: 'return the refreshed ready job' },
      ],
    }),
    risk: 'write',
    run: async (client, input, signal) => {
      const created = await client.uploadStreamVideo(stringInput(input, 'video_path'), without(input, 'video_path'), signal);
      if (!isJsonObject(created) || typeof created.id !== 'string') return created;
      const streamJobID = created.id;
      let editPlan: JsonObject;
      try {
        const value = await client.request({ path: `/api/stream-jobs/${encodeURIComponent(streamJobID)}/edit-plan`, signal });
        if (!isJsonObject(value)) throw new Error('stream edit plan response must be an object');
        editPlan = value;
      } catch (error: unknown) {
        return streamInitializationFailure(
          created,
          streamJobID,
          'load_edit_plan',
          error,
          undefined,
          isOperationCancellation(error, signal),
        );
      }
      let readyPlan: JsonValue;
      try {
        readyPlan = await client.request({
          body: editPlan,
          method: 'PUT',
          path: `/api/stream-jobs/${encodeURIComponent(streamJobID)}/edit-plan`,
          signal,
        });
      } catch (error: unknown) {
        return streamInitializationFailure(
          created,
          streamJobID,
          'persist_edit_plan',
          error,
          editPlan,
          isOperationCancellation(error, signal),
        );
      }
      let readyJob: JsonValue;
      try {
        readyJob = await client.request({
          path: `/api/stream-jobs/${encodeURIComponent(streamJobID)}`,
          signal,
        });
      } catch (error: unknown) {
        return streamInitializationFailure(
          created,
          streamJobID,
          'refresh_job',
          error,
          readyPlan,
          isOperationCancellation(error, signal),
        );
      }
      return { edit_plan: readyPlan, job: readyJob };
    },
    title: 'Create stream job from file',
  },
  mutationOperation({
    body: (input) => input,
    category: 'streams',
    description: 'Acquire a supported Twitch/VOD source URL using the configured local yt-dlp.',
    inputSchema: objectSchema({
      source_url: {
        description: 'Public HTTPS Twitch clip or VOD URL.',
        format: 'uri',
        maxLength: 2_048,
        pattern: '^[Hh][Tt][Tt][Pp][Ss]://(?:(?:[Ww]{3}\\.)?[Tt][Ww][Ii][Tt][Cc][Hh]\\.[Tt][Vv]/(?:videos/[0-9]+|[A-Za-z0-9_]+/clip/[A-Za-z0-9_-]+)|[Cc][Ll][Ii][Pp][Ss]\\.[Tt][Ww][Ii][Tt][Cc][Hh]\\.[Tt][Vv]/[A-Za-z0-9_-]+)(?:[?#].*)?$',
        type: 'string',
      },
      title: { type: 'string' },
    }, ['source_url']),
    keywords: ['twitch', 'download', 'vod', 'url'],
    name: 'streams.create_from_url',
    path: () => '/api/stream-jobs',
    risk: 'costly',
    title: 'Create stream job from URL',
  }),
  readOperation({ category: 'streams', description: 'Get one stream clip job.', inputSchema: STREAM_JOB_SCHEMA, name: 'streams.get', path: (input) => streamPath(input), title: 'Get stream job' }),
  readOperation({ category: 'streams', description: 'Read the current stream crop, clip, subtitle, music, and effects edit plan.', inputSchema: STREAM_JOB_SCHEMA, keywords: ['crop', 'clips', 'captions', 'subtitles', 'subtitulos', 'transcription', 'xai', 'grok', 'music'], name: 'streams.get_edit_plan', path: (input) => streamPath(input, '/edit-plan'), title: 'Get stream edit plan' }),
  {
    category: 'streams',
    description: 'Finish initializing an already uploaded stream job without uploading the source video again.',
    inputSchema: STREAM_JOB_SCHEMA,
    keywords: ['recover', 'resume', 'initialize', 'ready', 'retry'],
    name: 'streams.resume_initialization',
    preview: (input) => ({
      steps: [
        { method: 'GET', path: streamPath(input, '/edit-plan'), purpose: 'load the current edit plan' },
        { method: 'PUT', path: streamPath(input, '/edit-plan'), purpose: 'persist the unchanged plan and transition the job to ready' },
        { method: 'GET', path: streamPath(input), purpose: 'return the refreshed ready job' },
      ],
    }),
    risk: 'write',
    run: resumeStreamInitialization,
    title: 'Resume stream initialization',
  },
  mutationOperation({
    body: (input) => {
      const plan = input.plan;
      if (!isJsonObject(plan)) throw new Error('plan must be an object');
      return plan;
    },
    category: 'streams',
    description: 'Replace and validate the complete stream edit plan. Search with stream_job_id first to retrieve and preserve its current fields.',
    inputSchema: objectSchema({ plan: STREAM_EDIT_PLAN_PROPERTY, stream_job_id: UUID_PROPERTY }, ['stream_job_id', 'plan']),
    keywords: ['crop', 'clips', 'captions', 'subtitles', 'subtitulos', 'transcription', 'xai', 'grok', 'music', 'edit'],
    method: 'PUT',
    name: 'streams.update_edit_plan',
    path: (input) => streamPath(input, '/edit-plan'),
    title: 'Update stream edit plan',
  }),
  {
    category: 'streams',
    description: 'Enable or disable Spanish burned-in subtitles while preserving the rest of the current stream edit plan.',
    inputSchema: objectSchema({
      enabled: { type: 'boolean' },
      language: { description: 'Subtitle output language. Spanish is the only supported value.', enum: ['es'], type: 'string' },
      stream_job_id: UUID_PROPERTY,
    }, ['stream_job_id', 'enabled']),
    keywords: ['captions', 'caption', 'subtitles', 'subtitle', 'subtitulos', 'subtitulo', 'transcription', 'speech to text', 'stt', 'xai', 'grok'],
    name: 'streams.configure_captions',
    preview: (input) => ({
      captions: { enabled: input.enabled, language: 'es' },
      steps: [
        { method: 'GET', path: streamPath(input, '/edit-plan'), purpose: 'preserve the current edit plan' },
        { method: 'PUT', path: streamPath(input, '/edit-plan'), purpose: 'replace only caption settings inside that plan' },
      ],
    }),
    risk: 'write',
    run: configureStreamCaptions,
    title: 'Configure stream subtitles',
  },
  mutationOperation({
    category: 'streams',
    description: 'Generate Spanish caption candidates for the enabled stream edit plan. This can use the configured xAI speech-to-text service.',
    inputSchema: STREAM_JOB_SCHEMA,
    keywords: ['captions', 'caption', 'subtitles', 'subtitulos', 'generate', 'transcription', 'speech to text', 'stt', 'xai', 'grok'],
    name: 'streams.start_caption_candidates',
    path: (input) => streamPath(input, '/captions'),
    risk: 'costly',
    title: 'Generate stream caption candidates',
  }),
  readOperation({
    category: 'streams',
    description: 'Read the current stream caption candidates and the generation ID required to review them.',
    inputSchema: STREAM_JOB_SCHEMA,
    keywords: ['captions', 'caption', 'subtitles', 'subtitulos', 'review', 'transcription'],
    name: 'streams.get_caption_candidates',
    path: (input) => streamPath(input, '/captions'),
    title: 'Get stream caption candidates',
  }),
  mutationOperation({
    body: (input) => without(input, 'stream_job_id'),
    category: 'streams',
    description: 'Persist reviewed caption words or explicit no-speech decisions for the current caption generation. This updates the stream edit plan.',
    inputSchema: STREAM_CAPTION_REVIEW_SCHEMA,
    keywords: ['captions', 'caption', 'subtitles', 'subtitulos', 'review', 'approve', 'words', 'no speech'],
    name: 'streams.review_caption_candidates',
    path: (input) => streamPath(input, '/captions/review'),
    title: 'Review stream caption candidates',
  }),
  {
    category: 'streams',
    description: 'Set one clip\'s edit options — playback speed, original-audio volume (0 mutes), boundary fades, and burned-in text overlays — while preserving the rest of the current stream edit plan. Sending a default value (speed 1, source_volume 1, fades 0, empty text_overlays) resets that option.',
    inputSchema: objectSchema({
      clip_id: SAFE_TOKEN_PROPERTY,
      stream_job_id: UUID_PROPERTY,
      ...CLIP_EDIT_OPTION_PROPERTIES,
    }, ['stream_job_id', 'clip_id']),
    keywords: ['edit', 'edicion', 'speed', 'velocidad', 'slow motion', 'camara lenta', 'volume', 'volumen', 'mute', 'silenciar', 'fade', 'fundido', 'text', 'texto', 'overlay'],
    name: 'streams.edit_clip',
    preview: (input) => ({
      edit: without(input, 'stream_job_id', 'clip_id'),
      steps: [
        { method: 'GET', path: streamPath(input, '/edit-plan'), purpose: 'preserve the current edit plan' },
        { method: 'PUT', path: streamPath(input, '/edit-plan'), purpose: 'replace only this clip\'s edit options inside that plan' },
      ],
    }),
    risk: 'write',
    run: editStreamClip,
    title: 'Edit stream clip options',
  },
  mutationOperation({ category: 'streams', description: 'Start a costly stream clip render from the saved edit plan, including xAI subtitles when enabled.', inputSchema: STREAM_VARIANT_SCHEMA, keywords: ['twitch', 'vertical', 'render', 'captions', 'subtitles', 'subtitulos', 'xai', 'grok'], name: 'streams.start_render', path: (input) => streamRenderPath(input), risk: 'costly', title: 'Start stream render' }),
  readOperation({ category: 'streams', description: 'Read stream render progress and real video entries.', inputSchema: STREAM_VARIANT_SCHEMA, keywords: ['twitch', 'render', 'videos'], name: 'streams.get_render', path: (input) => streamRenderPath(input), title: 'Get stream render state' }),
  readOperation({
    category: 'streams',
    description: 'List the weapon icon catalog keys a synthetic killfeed notice may use.',
    keywords: ['killfeed', 'weapon', 'notice', 'icon', 'synthetic'],
    name: 'streams.list_killfeed_weapons',
    path: () => '/api/stream-killfeed/weapons',
    title: 'List killfeed weapons',
  }),
  mutationOperation({
    category: 'streams',
    description: 'Analyze the current stream killfeed crop and clips. This queues local FFmpeg work and can use configured xAI vision for structured kills.',
    inputSchema: STREAM_JOB_SCHEMA,
    keywords: ['killfeed', 'analyze', 'analysis', 'ocr', 'vision', 'ffmpeg', 'xai', 'grok'],
    name: 'streams.start_killfeed_analysis',
    path: (input) => streamPath(input, '/killfeed'),
    risk: 'costly',
    title: 'Analyze stream killfeed',
  }),
  readOperation({
    category: 'streams',
    description: 'Read the current stream killfeed analysis and the generation ID required to apply it.',
    inputSchema: STREAM_JOB_SCHEMA,
    keywords: ['killfeed', 'analysis', 'ocr', 'vision', 'review'],
    name: 'streams.get_killfeed_analysis',
    path: (input) => streamPath(input, '/killfeed'),
    title: 'Get stream killfeed analysis',
  }),
  mutationOperation({
    body: (input) => without(input, 'stream_job_id'),
    category: 'streams',
    description: 'Apply one current killfeed analysis generation to the stream edit plan. This changes its rendered killfeed cues.',
    inputSchema: STREAM_GENERATION_SCHEMA,
    keywords: ['killfeed', 'analysis', 'apply', 'cues', 'ocr', 'vision'],
    name: 'streams.apply_killfeed_analysis',
    path: (input) => streamPath(input, '/killfeed/apply'),
    title: 'Apply stream killfeed analysis',
  }),
  mutationOperation({
    body: (input) => ({ clip_id: stringInput(input, 'clip_id'), cue_seconds: input.cue_seconds }),
    category: 'streams',
    description: 'Read the confirmed kills visible at a killfeed cue with the configured xAI vision reader. Requires local ffmpeg and an xAI key; returns {kills} for the cue.',
    inputSchema: objectSchema(
      {
        clip_id: SAFE_TOKEN_PROPERTY,
        cue_seconds: { description: 'Absolute source timestamp of the cue, inside the clip range.', minimum: 0, type: 'number' },
        stream_job_id: UUID_PROPERTY,
      },
      ['stream_job_id', 'clip_id', 'cue_seconds'],
    ),
    keywords: ['killfeed', 'kills', 'read', 'xai', 'grok', 'vision'],
    name: 'streams.read_killfeed',
    path: (input) => streamPath(input, '/killfeed-read'),
    risk: 'costly',
    title: 'Read killfeed kills',
  }),
  {
    category: 'streams',
    description: 'Render one kill notice to the exact synthetic PNG the render uses. The endpoint returns image bytes, so this reports the request rather than embedding the image; view notices in the web editor.',
    inputSchema: KILLFEED_KILL_PROPERTY,
    keywords: ['killfeed', 'notice', 'preview', 'png', 'synthetic'],
    name: 'streams.preview_killfeed_notice',
    preview: (input) => requestPreview('POST', '/api/stream-killfeed/notice-preview', input),
    risk: 'read',
    run: async (_client, input) => ({
      note: 'The kill notice is returned as image bytes and is not embedded in agent context; open the FragForge web editor to view the synthetic notice.',
      request: requestPreview('POST', '/api/stream-killfeed/notice-preview', input),
    }),
    title: 'Preview killfeed notice',
  },
  {
    category: 'artifacts',
    description: 'Return a loopback URL for a stream source, gallery, MP4, or upload-ready delivery asset.',
    inputSchema: objectSchema({ clip_id: SAFE_TOKEN_PROPERTY, name: SAFE_TOKEN_PROPERTY, kind: { enum: ['source', 'gallery', 'video', 'delivery'], type: 'string' }, stream_job_id: UUID_PROPERTY, variant: VARIANT_PROPERTY }, ['stream_job_id', 'kind']),
    keywords: ['twitch', 'download', 'url', 'mp4'],
    name: 'artifacts.get_stream_url',
    preview: (input) => ({ method: 'GET', path: streamArtifactPath(input) }),
    risk: 'read',
    run: async (client, input, signal) => ({ url: await client.artifactUrl(streamArtifactPath(input), signal) }),
    title: 'Get stream artifact URL',
  },
];

type StreamInitializationStage = 'load_edit_plan' | 'persist_edit_plan' | 'refresh_job';

function streamInitializationFailure(
  created: JsonObject,
  streamJobID: string,
  failedStep: StreamInitializationStage,
  error: unknown,
  knownPlan?: JsonValue,
  cancelled = false,
): JsonObject {
  const steps: JsonObject[] = [{
    arguments: { stream_job_id: streamJobID },
    confirmed: true,
    mode: 'apply',
    operation: 'streams.resume_initialization',
  }];

  const message = error instanceof Error ? error.message : 'unknown stream initialization failure';
  const result: JsonObject = {
    initialization: {
      failed_step: failedStep,
      message: message.length > 500 ? `${message.slice(0, 500)}…` : message,
      ready_state: 'unknown',
      status: 'partial',
    },
    job: created,
    partial: true,
    recovery: {
      note: 'The upload already succeeded. Do not call streams.create_from_file again; submit this step unchanged to execute.',
      reupload_required: false,
      retry_create_from_file: false,
      safe_to_retry_steps: true,
      steps,
    },
    stream_job_id: streamJobID,
  };
  if (cancelled) {
    result.cancelled = true;
    result.error = 'operation was cancelled after the stream upload succeeded; do not upload the file again';
  }
  if (knownPlan !== undefined) result.edit_plan = knownPlan;
  return result;
}

async function resumeStreamInitialization(
  client: OrchestratorClient,
  input: JsonObject,
  signal?: AbortSignal,
): Promise<JsonValue> {
  const editPlanPath = streamPath(input, '/edit-plan');
  const currentPlan = await client.request({ path: editPlanPath, signal });
  if (!isJsonObject(currentPlan)) throw new Error('stream edit plan response must be an object');
  const readyPlan = await client.request({ body: currentPlan, method: 'PUT', path: editPlanPath, signal });
  const readyJob = await client.request({ path: streamPath(input), signal });
  return { edit_plan: readyPlan, job: readyJob };
}

function isOperationCancellation(error: unknown, signal?: AbortSignal): boolean {
  return signal?.aborted === true || (error instanceof Error && error.name === 'AbortError');
}

const operationByName = new Map(operations.map((operation) => [operation.name, operation]));

export function listOperations(): readonly OperationDefinition[] {
  return operations;
}

export function operationNamed(name: string): OperationDefinition | undefined {
  return operationByName.get(name);
}

export function validateOperationInput(operation: OperationDefinition, input: JsonObject): void {
  validateJsonSchema(operation.inputSchema, input, 'arguments');
  if (operation.name === 'jobs.record' && input.edit !== undefined && input.preset === undefined) {
    throw new MissingOperationInputError('preset', 'arguments.preset is required when arguments.edit is provided');
  }
  if (isJsonObject(input.rules)) validateRules(input.rules);
  if (operation.name === 'artifacts.get_url') validateArtifactInput(input);
  if (operation.name === 'artifacts.get_stream_url') validateStreamArtifactInput(input);
  if (operation.name === 'streams.update_edit_plan') {
    const plan = input.plan;
    if (!isJsonObject(plan)) throw new Error('arguments.plan must be an object');
    validateStreamEditPlan(plan);
  }
  if (operation.name === 'streams.review_caption_candidates') validateStreamCaptionReview(input);
}

type LiveVariantRegistry = 'render' | 'stream';

interface LiveVariantSelection {
  field: 'arguments.plan.variant' | 'arguments.preset' | 'arguments.variant';
  registry: LiveVariantRegistry;
  value: string;
}

interface LiveVariantDescriptor {
  fullFrame?: boolean;
  name: string;
}

export async function validateLiveOperationInput(
  client: OrchestratorClient,
  operation: OperationDefinition,
  input: JsonObject,
  signal?: AbortSignal,
): Promise<void> {
  const selection = liveVariantSelection(operation, input);
  if (selection === undefined) return;

  const variants = await liveVariants(client, selection.registry, signal);
  const selected = requireLiveVariant(selection, variants);
  if (selection.field === 'arguments.plan.variant' && isJsonObject(input.plan)) {
    validateStreamPlanLayout(input.plan, selected.fullFrame === true);
  }
}

function requireLiveVariant(
  selection: LiveVariantSelection,
  variants: readonly LiveVariantDescriptor[],
): LiveVariantDescriptor {
  const selected = variants.find((variant) => variant.name === selection.value);
  if (selected !== undefined) return selected;
  const received = selection.value.length <= MAX_LIVE_VARIANT_NAME_LENGTH
    ? selection.value
    : `${selection.value.slice(0, MAX_LIVE_VARIANT_NAME_LENGTH - 1)}…`;
  const names = variants.map((variant) => variant.name);
  const visibleNames = names.slice(0, MAX_LIVE_VARIANT_NAMES_IN_ERROR).join(', ');
  const suffix = names.length > MAX_LIVE_VARIANT_NAMES_IN_ERROR ? ', …' : '';
  throw new Error(`${selection.field} ${JSON.stringify(received)} is not one of the live ${selection.registry} variants: ${visibleNames}${suffix}`);
}

function liveVariantSelection(operation: OperationDefinition, input: JsonObject): LiveVariantSelection | undefined {
  if (operation.name === 'artifacts.get_url' && input.kind === 'final') return undefined;
  if (operation.name === 'artifacts.get_stream_url' && input.kind === 'source') return undefined;

  if (typeof input.preset === 'string') {
    return { field: 'arguments.preset', registry: 'render', value: input.preset };
  }

  if (typeof input.variant === 'string') {
    const registry = operation.category === 'streams' || operation.name === 'artifacts.get_stream_url'
      ? 'stream'
      : 'render';
    return { field: 'arguments.variant', registry, value: input.variant };
  }

  if (operation.name !== 'streams.update_edit_plan' || !isJsonObject(input.plan)) return undefined;
  const variant = input.plan.variant;
  return typeof variant === 'string'
    ? { field: 'arguments.plan.variant', registry: 'stream', value: variant }
    : undefined;
}

async function liveVariants(
  client: OrchestratorClient,
  registry: LiveVariantRegistry,
  signal?: AbortSignal,
): Promise<LiveVariantDescriptor[]> {
  let collectionKey = 'presets';
  let path = '/api/presets';
  if (registry === 'stream') {
    collectionKey = 'variants';
    path = '/api/stream-variants';
  }
  const response = await client.request({ path, signal });
  const collection = isJsonObject(response) ? response[collectionKey] : undefined;
  if (!Array.isArray(collection)) throw new Error(`live ${registry} variant registry response is malformed`);

  const variants: LiveVariantDescriptor[] = [];
  const seen = new Set<string>();
  const safeToken = new RegExp(SAFE_TOKEN_PATTERN);
  for (const item of collection) {
    if (!isJsonObject(item) || typeof item.name !== 'string'
      || item.name.length > MAX_LIVE_VARIANT_NAME_LENGTH || !safeToken.test(item.name)
      || (registry === 'stream' && typeof item.full_frame !== 'boolean')) {
      throw new Error(`live ${registry} variant registry response is malformed`);
    }
    if (!seen.has(item.name)) {
      seen.add(item.name);
      variants.push({
        fullFrame: registry === 'stream' ? item.full_frame === true : undefined,
        name: item.name,
      });
    }
  }
  if (variants.length === 0) throw new Error(`live ${registry} variant registry is empty`);
  return variants;
}

export async function discoverDynamicInputs(client: OrchestratorClient, operation: OperationDefinition, input: JsonObject, signal?: AbortSignal): Promise<JsonObject[]> {
  const fields: Array<Promise<JsonObject>> = [];
  const properties = operation.inputSchema.properties;
  if (isJsonObject(properties)) {
    if ('job_id' in properties) fields.push(jobCandidates(client, operation.name, signal));
    if ('stream_job_id' in properties) fields.push(streamJobCandidates(client, operation.name, signal));
    if ('preset' in properties) fields.push(presetCandidates(client, 'preset', signal));
    if ('variant' in properties && operation.category !== 'streams' && operation.name !== 'artifacts.get_stream_url'
      && !(operation.name === 'artifacts.get_url' && input.kind === 'final')) {
      fields.push(presetCandidates(client, 'variant', signal));
    }
    if ('variant' in properties && (operation.category === 'streams' || operation.name === 'artifacts.get_stream_url')
      && !(operation.name === 'artifacts.get_stream_url' && input.kind === 'source')) {
      fields.push(streamVariantCandidates(client, signal));
    }
    if ('music' in properties) fields.push(songCandidates(client, 'music', signal));
    if ('song_id' in properties) fields.push(songCandidates(client, 'song_id', signal));
  }
  const jobID = optionalStringInput(input, 'job_id');
  if (jobID !== undefined && operation.name === 'jobs.parse') fields.push(rosterCandidates(client, jobID, signal));
  if (jobID !== undefined && (operation.name === 'jobs.record' || operation.name === 'jobs.generate')) fields.push(momentCandidates(client, jobID, signal));
  if (jobID !== undefined && (operation.name === 'renders.delete_video' || operation.name === 'renders.publish_assistant' || operation.name === 'artifacts.get_url')) {
    const variant = optionalStringInput(input, 'variant');
    if (variant !== undefined) fields.push(renderArtifactCandidates(client, jobID, variant, renderArtifactKind(operation, input), signal));
  }
  const streamJobID = optionalStringInput(input, 'stream_job_id');
  if (streamJobID !== undefined && (operation.name === 'streams.update_edit_plan' || operation.name === 'streams.configure_captions' || operation.name === 'streams.edit_clip')) {
    fields.push(streamEditPlanCurrentValue(client, streamJobID, signal));
  }
  if (streamJobID !== undefined && operation.name === 'artifacts.get_stream_url' && input.kind === 'video') {
    const variant = optionalStringInput(input, 'variant');
    if (variant !== undefined) fields.push(streamClipCandidates(client, streamJobID, variant, signal));
  }
  return Promise.all(fields);
}

function artifactPath(input: JsonObject): string {
  const kind = stringInput(input, 'kind');
  if (kind === 'final') return jobPath(input, '/final');
  const variant = stringInput(input, 'variant');
  const base = `/api/jobs/${encodeURIComponent(stringInput(input, 'job_id'))}/renders/${encodeURIComponent(variant)}`;
  if (kind === 'gallery') return `${base}/gallery`;
  if (kind === 'pack') return `${base}/pack`;
  if (kind === 'edit_document') return `${base}/edit-document`;
  const name = encodeURIComponent(stringInput(input, 'name'));
  if (kind === 'video') return `${base}/videos/${name}`;
  if (kind === 'cover') return `${base}/covers/${name}`;
  if (kind === 'caption') return `${base}/captions/${name}`;
  throw new Error(`unsupported artifact kind ${kind}`);
}

function streamArtifactPath(input: JsonObject): string {
  const kind = stringInput(input, 'kind');
  if (kind === 'source') return streamPath(input, '/source');
  if (kind === 'gallery') return streamRenderPath(input, '/gallery');
  if (kind === 'video') return streamRenderPath(input, `/videos/${encodeURIComponent(stringInput(input, 'clip_id'))}`);
  if (kind === 'delivery') return streamRenderPath(input, `/delivery/${encodeURIComponent(stringInput(input, 'name'))}`);
  throw new Error(`unsupported stream artifact kind ${kind}`);
}

function songArtifactPath(input: JsonObject): string {
  return `/api/songs/${encodeURIComponent(stringInput(input, 'song_id'))}/audio`;
}

function validateArtifactInput(input: JsonObject): void {
  if (input.kind === 'final') return;
  if (typeof input.variant !== 'string') {
    throw new MissingOperationInputError('variant', 'arguments.variant is required for this artifact kind');
  }
  if ((input.kind === 'video' || input.kind === 'cover' || input.kind === 'caption') && typeof input.name !== 'string') {
    throw new MissingOperationInputError('name', 'arguments.name is required for this artifact kind');
  }
}

function validateStreamArtifactInput(input: JsonObject): void {
  if (input.kind === 'source') return;
  if (typeof input.variant !== 'string') {
    throw new MissingOperationInputError('variant', 'arguments.variant is required for this stream artifact kind');
  }
  if (input.kind === 'video' && typeof input.clip_id !== 'string') {
    throw new MissingOperationInputError('clip_id', 'arguments.clip_id is required when kind is video');
  }
  if (input.kind === 'delivery' && typeof input.name !== 'string') {
    throw new MissingOperationInputError('name', 'arguments.name is required when kind is delivery');
  }
}

export function validateJsonSchema(schema: JsonObject, value: JsonValue, path: string): void {
  const expectedType = schema.type;
  if (expectedType === 'object') {
    if (!isJsonObject(value)) throw new Error(`${path} must be an object`);
    const properties = isJsonObject(schema.properties) ? schema.properties : {};
    const required = Array.isArray(schema.required) ? schema.required.filter((item): item is string => typeof item === 'string') : [];
    for (const key of required) {
      if (!Object.hasOwn(value, key)) {
        throw new MissingOperationInputError(inputField(path, key), `${path}.${key} is required`);
      }
    }
    if (schema.additionalProperties === false) {
      for (const key of Object.keys(value)) {
        if (!Object.hasOwn(properties, key)) throw new Error(`${path}.${key} is not allowed`);
      }
    }
    for (const [key, child] of Object.entries(value)) {
      if (!Object.hasOwn(properties, key)) continue;
      const childSchema = properties[key];
      if (isJsonObject(childSchema)) validateJsonSchema(childSchema, child, `${path}.${key}`);
    }
    const condition = schema.if;
    if (isJsonObject(condition)) {
      const branch = matchesJsonSchema(condition, value) ? schema.then : schema.else;
      if (isJsonObject(branch)) validateJsonSchema(branch, value, path);
    }
    return;
  }
  if (expectedType === 'array') {
    if (!Array.isArray(value)) throw new Error(`${path} must be an array`);
    if (typeof schema.minItems === 'number' && value.length < schema.minItems) throw new Error(`${path} has too few items`);
    if (typeof schema.maxItems === 'number' && value.length > schema.maxItems) throw new Error(`${path} has too many items`);
    const items = schema.items;
    if (isJsonObject(items)) value.forEach((item, index) => validateJsonSchema(items, item, `${path}[${index}]`));
    return;
  }
  if (expectedType === 'string') {
    if (typeof value !== 'string') throw new Error(`${path} must be a string`);
    // Count code points, not UTF-16 units, so limits agree with the Go
    // validators' rune counts (an emoji is one character on both sides).
    const length = [...value].length;
    if (typeof schema.minLength === 'number' && length < schema.minLength) throw new Error(`${path} is too short`);
    if (typeof schema.maxLength === 'number' && length > schema.maxLength) throw new Error(`${path} is too long`);
    if (schema.format === 'uri') {
      try {
        new URL(value);
      } catch {
        throw new Error(`${path} must be a valid URL`);
      }
    }
    if (typeof schema.pattern === 'string' && !new RegExp(schema.pattern).test(value)) throw new Error(`${path} has an invalid format`);
  } else if (expectedType === 'integer') {
    if (typeof value !== 'number' || !Number.isInteger(value)) throw new Error(`${path} must be an integer`);
  } else if (expectedType === 'number') {
    if (typeof value !== 'number' || !Number.isFinite(value)) throw new Error(`${path} must be a number`);
  } else if (expectedType === 'boolean' && typeof value !== 'boolean') {
    throw new Error(`${path} must be a boolean`);
  }
  if (schema.const !== undefined && schema.const !== value) throw new Error(`${path} must equal ${String(schema.const)}`);
  if (Array.isArray(schema.enum) && !schema.enum.some((item) => item === value)) throw new Error(`${path} must be one of: ${schema.enum.join(', ')}`);
  if (typeof value === 'number' && typeof schema.minimum === 'number' && value < schema.minimum) throw new Error(`${path} is below the minimum`);
  if (typeof value === 'number' && typeof schema.maximum === 'number' && value > schema.maximum) throw new Error(`${path} is above the maximum`);
  if (typeof value === 'number' && typeof schema.exclusiveMinimum === 'number' && value <= schema.exclusiveMinimum) throw new Error(`${path} must be above the exclusive minimum`);
}

function inputField(path: string, key: string): string {
  if (path === 'arguments') return key;
  if (path.startsWith('arguments.')) return `${path.slice('arguments.'.length)}.${key}`;
  return `${path}.${key}`;
}

function matchesJsonSchema(schema: JsonObject, value: JsonValue): boolean {
  try {
    validateJsonSchema(schema, value, 'condition');
    return true;
  } catch {
    return false;
  }
}

function validateRules(rules: JsonObject): void {
  const minRound = rules.min_round;
  const maxRound = rules.max_round;
  if (typeof minRound === 'number' && typeof maxRound === 'number' && maxRound !== 0 && maxRound < minRound) {
    throw new Error('arguments.rules.max_round must be zero or greater than or equal to min_round');
  }
}

/** The streams.edit_clip fields merged into the clip's edit object. */
const CLIP_EDIT_OPTION_KEYS = ['speed', 'source_volume', 'fade_in_seconds', 'fade_out_seconds', 'text_overlays'] as const;

/**
 * Drops default-valued known fields so a fully reset edit disappears from the
 * plan. Unknown fields are deliberately preserved: the following schema
 * validation then rejects them loudly instead of a targeted edit silently
 * erasing data written by a newer FragForge.
 */
function pruneClipEditObject(edit: JsonObject): JsonObject | undefined {
  const next: JsonObject = { ...edit };
  if (next.speed === 1) delete next.speed;
  if (next.source_volume === 1) delete next.source_volume;
  if (next.fade_in_seconds === 0) delete next.fade_in_seconds;
  if (next.fade_out_seconds === 0) delete next.fade_out_seconds;
  if (Array.isArray(next.text_overlays) && next.text_overlays.length === 0) delete next.text_overlays;
  return Object.keys(next).length > 0 ? next : undefined;
}

/**
 * Shared read-modify-write pipeline for every plan-mutating operation: fetch
 * the current plan, apply the mutation, re-validate the whole plan (schema,
 * cross-field, live variant), then persist it.
 */
async function updateStreamEditPlan(
  client: OrchestratorClient,
  input: JsonObject,
  mutate: (plan: JsonObject) => JsonObject,
  signal?: AbortSignal,
): Promise<JsonValue> {
  const editPlanPath = streamPath(input, '/edit-plan');
  const current = await client.request({ path: editPlanPath, signal });
  if (!isJsonObject(current)) throw new Error('stream edit plan response must be an object');
  const updated = mutate(current);
  validateJsonSchema(STREAM_EDIT_PLAN_PROPERTY, updated, 'stream edit plan');
  validateStreamEditPlan(updated);
  await validateLiveStreamEditPlan(client, updated, signal);
  return client.request({ body: updated, method: 'PUT', path: editPlanPath, signal });
}

async function editStreamClip(client: OrchestratorClient, input: JsonObject, signal?: AbortSignal): Promise<JsonValue> {
  return updateStreamEditPlan(client, input, (current) => {
    const clipID = stringInput(input, 'clip_id');
    const clips = Array.isArray(current.clips) ? current.clips : [];
    const clipIDs = clips.filter(isJsonObject).flatMap((clip) => (typeof clip.id === 'string' ? [clip.id] : []));
    if (!clipIDs.includes(clipID)) {
      throw new Error(`arguments.clip_id ${JSON.stringify(clipID)} is not one of the plan's clips: ${clipIDs.join(', ')}`);
    }
    const updatedClips = clips.map((clip): JsonValue => {
      if (!isJsonObject(clip) || clip.id !== clipID) return clip;
      const merged: JsonObject = isJsonObject(clip.edit) ? { ...clip.edit } : {};
      for (const key of CLIP_EDIT_OPTION_KEYS) {
        if (input[key] !== undefined) merged[key] = input[key];
      }
      const next: JsonObject = { ...clip };
      const pruned = pruneClipEditObject(merged);
      if (pruned === undefined) delete next.edit;
      else next.edit = pruned;
      return next;
    });
    return { ...current, clips: updatedClips };
  }, signal);
}

async function configureStreamCaptions(client: OrchestratorClient, input: JsonObject, signal?: AbortSignal): Promise<JsonValue> {
  const captions: JsonObject = { enabled: input.enabled === true, language: 'es' };
  return updateStreamEditPlan(client, input, (current) => ({ ...current, captions }), signal);
}

function validateStreamEditPlan(plan: JsonObject): void {
  for (const key of ['face_crop', 'gameplay_crop', 'killfeed_crop']) {
    const crop = plan[key];
    if (!isJsonObject(crop)) continue;
    const x = crop.x;
    const y = crop.y;
    const width = crop.width;
    const height = crop.height;
    if (typeof x === 'number' && typeof width === 'number' && x + width > 1) {
      throw new Error(`arguments.plan.${key} must stay within the source frame`);
    }
    if (typeof y === 'number' && typeof height === 'number' && y + height > 1) {
      throw new Error(`arguments.plan.${key} must stay within the source frame`);
    }
  }
  const clips = plan.clips;
  if (!Array.isArray(clips)) return;
  const hasKillfeedCrop = isJsonObject(plan.killfeed_crop);
  const seen = new Set<string>();
  for (const [index, value] of clips.entries()) {
    if (!isJsonObject(value)) continue;
    if (typeof value.start_seconds === 'number' && typeof value.end_seconds === 'number'
      && value.end_seconds <= value.start_seconds) {
      throw new Error(`arguments.plan.clips[${index}].end_seconds must be greater than start_seconds`);
    }
    const killfeedSeconds = value.killfeed_seconds;
    if (Array.isArray(killfeedSeconds)) {
      if (killfeedSeconds.length > 0 && !hasKillfeedCrop) {
        throw new Error(`arguments.plan.killfeed_crop is required when arguments.plan.clips[${index}].killfeed_seconds contains cues`);
      }
      const seenKillfeedSeconds = new Set<number>();
      for (const [cueIndex, cue] of killfeedSeconds.entries()) {
        const field = `arguments.plan.clips[${index}].killfeed_seconds[${cueIndex}]`;
        if (typeof cue !== 'number' || !Number.isFinite(cue)) {
          throw new Error(`${field} must be a finite number`);
        }
        if (seenKillfeedSeconds.has(cue)) {
          throw new Error(`${field} must not duplicate an earlier cue`);
        }
        seenKillfeedSeconds.add(cue);
        if (typeof value.start_seconds === 'number' && typeof value.end_seconds === 'number'
          && (cue < value.start_seconds || cue >= value.end_seconds)) {
          throw new Error(`${field} must be greater than or equal to start_seconds and less than end_seconds`);
        }
      }
    }
    const killfeedKills = value.killfeed_kills;
    if (Array.isArray(killfeedKills)) {
      const cueCount = Array.isArray(killfeedSeconds) ? killfeedSeconds.length : 0;
      if (killfeedKills.length !== cueCount) {
        throw new Error(`arguments.plan.clips[${index}].killfeed_kills must have one entry per killfeed_seconds cue`);
      }
    }
    if (typeof value.id === 'string') {
      if (seen.has(value.id)) throw new Error(`arguments.plan.clips contains duplicate id ${value.id}`);
      seen.add(value.id);
    }
    if (isJsonObject(value.edit)) validateClipEdit(value.edit, value, index);
  }
}

function validateStreamCaptionReview(input: JsonObject): void {
  const clips = input.clips;
  if (!Array.isArray(clips)) return;

  const seenClipIDs = new Set<string>();
  for (const [clipIndex, review] of clips.entries()) {
    if (!isJsonObject(review)) continue;
    const clipID = review.clip_id;
    if (typeof clipID !== 'string') continue;
    if (seenClipIDs.has(clipID)) {
      throw new Error(`arguments.clips[${clipIndex}].clip_id duplicates an earlier review`);
    }
    seenClipIDs.add(clipID);

    const noSpeech = review.no_speech === true;
    const words = review.words;
    if (noSpeech && Array.isArray(words) && words.length > 0) {
      throw new Error(`arguments.clips[${clipIndex}] cannot include words when no_speech is true`);
    }
    if (!noSpeech && (!Array.isArray(words) || words.length === 0)) {
      throw new Error(`arguments.clips[${clipIndex}] requires reviewed words or no_speech=true`);
    }
    if (!Array.isArray(words)) continue;

    let previousEnd = 0;
    for (const [wordIndex, wordCue] of words.entries()) {
      if (!isJsonObject(wordCue)) continue;
      const word = wordCue.word;
      const start = wordCue.start_seconds;
      const end = wordCue.end_seconds;
      const wordPath = `arguments.clips[${clipIndex}].words[${wordIndex}]`;
      if (typeof word !== 'string') continue;
      if (word.trim() === '') throw new Error(`${wordPath}.word must not be blank`);
      if (/\r|\n/.test(word)) throw new Error(`${wordPath}.word must not contain a line break`);
      if (typeof start !== 'number' || typeof end !== 'number') continue;
      if (end <= start) throw new Error(`${wordPath}.end_seconds must be greater than start_seconds`);
      if (end - start > 2.5) throw new Error(`${wordPath} must last no more than 2.5 seconds`);
      if (wordIndex > 0 && start < previousEnd) {
        throw new Error(`${wordPath}.start_seconds must not overlap the previous word`);
      }
      previousEnd = end;
    }
  }
}

/**
 * Cross-field checks the JSON schema cannot express, mirroring
 * streamclips.ClipEdit.validate: fades must fit the sped-up output duration
 * and overlay windows must stay inside the clip.
 */
function validateClipEdit(edit: JsonObject, clip: JsonObject, index: number): void {
  const duration = typeof clip.start_seconds === 'number' && typeof clip.end_seconds === 'number'
    ? clip.end_seconds - clip.start_seconds
    : undefined;
  const speed = typeof edit.speed === 'number' ? edit.speed : 1;
  const fadeIn = typeof edit.fade_in_seconds === 'number' ? edit.fade_in_seconds : 0;
  const fadeOut = typeof edit.fade_out_seconds === 'number' ? edit.fade_out_seconds : 0;
  if (duration !== undefined && speed > 0 && fadeIn + fadeOut > duration / speed) {
    throw new Error(`arguments.plan.clips[${index}].edit fades must fit within the clip's output duration`);
  }
  if (!Array.isArray(edit.text_overlays)) return;
  for (const [overlayIndex, overlay] of edit.text_overlays.entries()) {
    if (!isJsonObject(overlay)) continue;
    const field = `arguments.plan.clips[${index}].edit.text_overlays[${overlayIndex}]`;
    const start = typeof overlay.start_seconds === 'number' ? overlay.start_seconds : undefined;
    const end = typeof overlay.end_seconds === 'number' ? overlay.end_seconds : undefined;
    if (duration !== undefined && start !== undefined && start >= duration) {
      throw new Error(`${field}.start_seconds must be inside the clip`);
    }
    if (duration !== undefined && end !== undefined && end > duration) {
      throw new Error(`${field}.end_seconds must be inside the clip`);
    }
    if (start !== undefined && end !== undefined && end <= start) {
      throw new Error(`${field}.end_seconds must be greater than start_seconds`);
    }
  }
}

async function validateLiveStreamEditPlan(
  client: OrchestratorClient,
  plan: JsonObject,
  signal?: AbortSignal,
): Promise<void> {
  const variant = plan.variant;
  if (typeof variant !== 'string') throw new Error('arguments.plan.variant must be a string');
  const selection: LiveVariantSelection = {
    field: 'arguments.plan.variant',
    registry: 'stream',
    value: variant,
  };
  const variants = await liveVariants(client, 'stream', signal);
  const selected = requireLiveVariant(selection, variants);
  validateStreamPlanLayout(plan, selected.fullFrame === true);
}

function validateStreamPlanLayout(plan: JsonObject, fullFrame: boolean): void {
  const faceCrop = plan.face_crop;
  if (!fullFrame && !isJsonObject(faceCrop)) {
    throw new Error('arguments.plan.face_crop is required when the live stream variant has full_frame=false');
  }
  if (fullFrame || !isJsonObject(faceCrop)) return;
  const width = faceCrop.width;
  const height = faceCrop.height;
  if (typeof width !== 'number' || typeof height !== 'number' || width <= 0 || height <= 0) {
    throw new Error('arguments.plan.face_crop must use positive normalized coordinates when the live stream variant has full_frame=false');
  }
}

function redactCapabilityPaths(value: JsonValue): JsonValue {
  if (Array.isArray(value)) return value.map(redactCapabilityPaths);
  if (!isJsonObject(value)) return value;
  return Object.fromEntries(
    Object.entries(value)
      .filter(([key]) => key !== 'path')
      .map(([key, child]) => [key, redactCapabilityPaths(child)]),
  );
}

async function jobCandidates(client: OrchestratorClient, operationName: string, signal?: AbortSignal): Promise<JsonObject> {
  const response = await client.request({ path: '/api/jobs?limit=100', signal });
  const candidates = candidatesFromCollection(response, 'jobs', 'id', ['status', 'target_steamid']);
  annotateEligibility(candidates, operationName, JOB_ELIGIBLE_STATUSES);
  return { candidates, field: 'job_id', source: '/api/jobs' };
}

async function streamJobCandidates(client: OrchestratorClient, operationName: string, signal?: AbortSignal): Promise<JsonObject> {
  const response = await client.request({ path: '/api/stream-jobs?limit=100', signal });
  const candidates = candidatesFromCollection(response, 'jobs', 'id', ['status', 'title']);
  annotateEligibility(candidates, operationName, STREAM_ELIGIBLE_STATUSES);
  return { candidates, field: 'stream_job_id', source: '/api/stream-jobs' };
}

async function rosterCandidates(client: OrchestratorClient, jobID: string, signal?: AbortSignal): Promise<JsonObject> {
  const response = await client.request({ path: `/api/jobs/${encodeURIComponent(jobID)}/roster`, signal });
  return { candidates: candidatesFromCollection(response, 'players', 'steamid64', ['name', 'team']), depends_on: 'job_id', field: 'target_steamid', source: 'job roster' };
}

async function momentCandidates(client: OrchestratorClient, jobID: string, signal?: AbortSignal): Promise<JsonObject> {
  const response = await client.request({ path: `/api/jobs/${encodeURIComponent(jobID)}/moments`, signal });
  let collection: JsonValue[] = [];
  if (isJsonObject(response) && Array.isArray(response.moments)) collection = response.moments;
  else if (Array.isArray(response)) collection = response;
  return { candidates: candidateRecords(collection, 'segment_id', ['label', 'score', 'round']), depends_on: 'job_id', field: 'segment_ids', source: 'job moments' };
}

async function presetCandidates(client: OrchestratorClient, field: string, signal?: AbortSignal): Promise<JsonObject> {
  const response = await client.request({ path: '/api/presets', signal });
  return { candidates: candidatesFromCollection(response, 'presets', 'name', ['label', 'description', 'default']), field, source: '/api/presets' };
}

async function songCandidates(client: OrchestratorClient, field: string, signal?: AbortSignal): Promise<JsonObject> {
  const response = await client.request({ path: '/api/songs', signal });
  return { candidates: candidatesFromCollection(response, 'songs', 'id', ['title', 'artist', 'genre', 'license']), field, source: '/api/songs' };
}

type RenderArtifactCandidateKind = 'caption' | 'cover' | 'video';

function renderArtifactKind(operation: OperationDefinition, input: JsonObject): RenderArtifactCandidateKind {
  if (operation.name === 'renders.delete_video' || operation.name === 'renders.publish_assistant') return 'video';
  if (input.kind === 'cover') return 'cover';
  if (input.kind === 'caption') return 'caption';
  return 'video';
}

async function renderArtifactCandidates(client: OrchestratorClient, jobID: string, variant: string, kind: RenderArtifactCandidateKind, signal?: AbortSignal): Promise<JsonObject> {
  if (kind === 'caption') return renderCaptionCandidates(client, jobID, variant, signal);
  const response = await client.request({ path: `/api/jobs/${encodeURIComponent(jobID)}/renders/${encodeURIComponent(variant)}`, signal });
  const names: JsonValue[] = [];
  if (isJsonObject(response)) {
    const values = response[kind === 'cover' ? 'covers' : 'videos'];
    if (Array.isArray(values)) {
      values.forEach((value) => {
        if (typeof value === 'string') names.push({ value });
      });
    }
  }
  return { candidates: names, depends_on: ['job_id', 'variant'], field: 'name', source: 'render state' };
}

async function renderCaptionCandidates(client: OrchestratorClient, jobID: string, variant: string, signal?: AbortSignal): Promise<JsonObject> {
  const response = await client.request({ path: `/api/jobs/${encodeURIComponent(jobID)}/renders/${encodeURIComponent(variant)}/publish`, signal });
  const values = isJsonObject(response) && Array.isArray(response.items) ? response.items : [];
  const candidates: JsonObject[] = [];
  for (const value of values) {
    if (!isJsonObject(value) || value.caption_ready !== true || typeof value.segment_id !== 'string') continue;
    candidates.push({ context: { status: value.status ?? null }, value: value.segment_id });
  }
  return { candidates, depends_on: ['job_id', 'variant'], field: 'name', source: 'render publish board' };
}

async function streamEditPlanCurrentValue(client: OrchestratorClient, streamJobID: string, signal?: AbortSignal): Promise<JsonObject> {
  const response = await client.request({ path: `/api/stream-jobs/${encodeURIComponent(streamJobID)}/edit-plan`, signal });
  return {
    current_value: response,
    depends_on: 'stream_job_id',
    field: 'plan',
    instructions: 'Preserve every unrelated field when updating the plan. Use streams.configure_captions when only subtitle settings need to change.',
    source: 'stream edit plan',
  };
}

async function streamClipCandidates(client: OrchestratorClient, streamJobID: string, variant: string, signal?: AbortSignal): Promise<JsonObject> {
  const response = await client.request({ path: `/api/stream-jobs/${encodeURIComponent(streamJobID)}/renders/${encodeURIComponent(variant)}`, signal });
  const videos = isJsonObject(response) && Array.isArray(response.videos) ? response.videos : [];
  return {
    candidates: candidateRecords(videos, 'clip_id', ['title', 'duration_seconds']),
    depends_on: ['stream_job_id', 'variant'],
    field: 'clip_id',
    source: 'stream render state',
  };
}

async function streamVariantCandidates(client: OrchestratorClient, signal?: AbortSignal): Promise<JsonObject> {
  const response = await client.request({ path: '/api/stream-variants', signal });
  return {
    candidates: candidatesFromCollection(response, 'variants', 'name', ['label', 'description', 'default']),
    field: 'variant',
    source: '/api/stream-variants',
  };
}

function candidatesFromCollection(response: JsonValue, key: string, valueKey: string, labelKeys: readonly string[]): JsonObject[] {
  if (!isJsonObject(response) || !Array.isArray(response[key])) return [];
  return candidateRecords(response[key], valueKey, labelKeys);
}

function candidateRecords(values: JsonValue[], valueKey: string, labelKeys: readonly string[]): JsonObject[] {
  const candidates: JsonObject[] = [];
  for (const value of values) {
    if (!isJsonObject(value)) continue;
    const candidateValue = value[valueKey] ?? (valueKey === 'segment_id' ? value.id : undefined);
    if (typeof candidateValue !== 'string') continue;
    const candidate: JsonObject = { value: candidateValue };
    const labels = labelKeys
      .map((key) => value[key])
      .filter((part): part is boolean | number | string => typeof part === 'string' || typeof part === 'number' || typeof part === 'boolean')
      .map(String);
    if (labels.length > 0) candidate.label = labels.join(' · ');
    const context: JsonObject = {};
    for (const key of labelKeys) {
      const contextValue = value[key];
      if (contextValue !== undefined) context[key] = contextValue;
    }
    if (Object.keys(context).length > 0) candidate.context = context;
    candidates.push(candidate);
  }
  return candidates;
}

const JOB_ELIGIBLE_STATUSES: Readonly<Record<string, readonly string[]>> = {
  'jobs.compose': ['recorded', 'composed'],
  'jobs.generate': ['parsed', 'recorded', 'failed'],
  'jobs.parse': ['scanned', 'parsed'],
  'jobs.record': ['parsed', 'recorded', 'failed'],
  'renders.start': ['recorded', 'composed', 'done'],
};

const STREAM_ELIGIBLE_STATUSES: Readonly<Record<string, readonly string[]>> = {
  'streams.apply_killfeed_analysis': ['ready', 'rendered'],
  'streams.review_caption_candidates': ['ready', 'rendered'],
  'streams.start_caption_candidates': ['ready', 'rendered'],
  'streams.start_killfeed_analysis': ['ready', 'rendered'],
  'streams.start_render': ['ready', 'rendered'],
};

function annotateEligibility(
  candidates: JsonObject[],
  operationName: string,
  eligibleStatusesByOperation: Readonly<Record<string, readonly string[]>>,
): void {
  const eligibleStatuses = eligibleStatusesByOperation[operationName];
  if (eligibleStatuses === undefined) return;
  for (const candidate of candidates) {
    const context = candidate.context;
    const status = isJsonObject(context) && typeof context.status === 'string' ? context.status : undefined;
    const eligible = status !== undefined && eligibleStatuses.includes(status);
    candidate.eligible = eligible;
    if (!eligible) {
      candidate.ineligible_reason = status === undefined
        ? 'job status is unavailable'
        : `${operationName} requires status ${eligibleStatuses.join(' or ')}; current status is ${status}`;
    } else if (operationName === 'jobs.record' || operationName === 'jobs.generate') {
      candidate.eligibility_note = 'Failed jobs are eligible only when their parsed kill plan is still available.';
    }
  }
}
