import { isJsonObject, type JsonObject, type JsonValue } from './json.ts';
import { OrchestratorClient } from './orchestrator-client.ts';

export type OperationRisk = 'costly' | 'destructive' | 'read' | 'write';

export interface OperationDefinition {
  category: 'artifacts' | 'catalog' | 'jobs' | 'renders' | 'streams' | 'studio';
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
const SAFE_TOKEN_PROPERTY: JsonObject = {
  pattern: SAFE_TOKEN_PATTERN,
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
const STREAM_EDIT_PLAN_PROPERTY: JsonObject = {
  additionalProperties: false,
  description: 'Complete stream edit plan. Search with stream_job_id to retrieve the current plan before replacing it.',
  properties: {
    captions: {
      additionalProperties: false,
      description: 'Burned-in subtitles generated locally through the configured xAI/Grok speech-to-text capability.',
      properties: {
        enabled: { type: 'boolean' },
        language: { description: 'Whisper language code such as es or en; omit for automatic detection.', pattern: '^[A-Za-z]{2,3}(?:-[A-Za-z0-9]{2,8})?$', type: 'string' },
      },
      required: ['enabled'],
      type: 'object',
    },
    clips: {
      items: {
        additionalProperties: false,
        properties: {
          end_seconds: { exclusiveMinimum: 0, type: 'number' },
          id: SAFE_TOKEN_PROPERTY,
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
    music: {
      additionalProperties: false,
      properties: {
        key: { pattern: '^[a-z0-9][a-z0-9-]*$', type: 'string' },
        volume: { maximum: 1, minimum: 0, type: 'number' },
      },
      type: 'object',
    },
    schema_version: { enum: ['1.0'], type: 'string' },
    streamer_banner: {
      additionalProperties: false,
      properties: {
        nick: { pattern: '^[A-Za-z0-9_]{1,25}$', type: 'string' },
        position_y: { maximum: 0.975, minimum: 0.025, type: 'number' },
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

function without(input: JsonObject, ...keys: readonly string[]): JsonObject {
  const excluded = new Set(keys);
  return Object.fromEntries(Object.entries(input).filter(([key]) => !excluded.has(key)));
}

const JOB_ID_SCHEMA = objectSchema({ job_id: UUID_PROPERTY }, ['job_id']);
const JOB_VARIANT_SCHEMA = objectSchema(
  {
    job_id: UUID_PROPERTY,
    variant: VARIANT_PROPERTY,
  },
  ['job_id', 'variant'],
);
const STREAM_JOB_SCHEMA = objectSchema({ stream_job_id: UUID_PROPERTY }, ['stream_job_id']);
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
        description: 'Public http(s) Twitch/VOD source URL.',
        format: 'uri',
        pattern: '^[Hh][Tt][Tt][Pp][Ss]?://',
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
    description: 'Enable, disable, or change burned-in subtitles while preserving the rest of the current stream edit plan.',
    inputSchema: objectSchema({
      enabled: { type: 'boolean' },
      language: { description: 'Whisper language code such as es or en; omit for automatic detection.', pattern: '^[A-Za-z]{2,3}(?:-[A-Za-z0-9]{2,8})?$', type: 'string' },
      stream_job_id: UUID_PROPERTY,
    }, ['stream_job_id', 'enabled']),
    keywords: ['captions', 'caption', 'subtitles', 'subtitle', 'subtitulos', 'subtitulo', 'transcription', 'speech to text', 'stt', 'xai', 'grok'],
    name: 'streams.configure_captions',
    preview: (input) => ({
      captions: { enabled: input.enabled, language: input.language ?? '' },
      steps: [
        { method: 'GET', path: streamPath(input, '/edit-plan'), purpose: 'preserve the current edit plan' },
        { method: 'PUT', path: streamPath(input, '/edit-plan'), purpose: 'replace only caption settings inside that plan' },
      ],
    }),
    risk: 'write',
    run: configureStreamCaptions,
    title: 'Configure stream subtitles',
  },
  mutationOperation({ category: 'streams', description: 'Start a costly stream clip render from the saved edit plan, including xAI/Grok subtitles when enabled.', inputSchema: STREAM_VARIANT_SCHEMA, keywords: ['twitch', 'vertical', 'render', 'captions', 'subtitles', 'subtitulos', 'xai', 'grok'], name: 'streams.start_render', path: (input) => streamRenderPath(input), risk: 'costly', title: 'Start stream render' }),
  readOperation({ category: 'streams', description: 'Read stream render progress and real video entries.', inputSchema: STREAM_VARIANT_SCHEMA, keywords: ['twitch', 'render', 'videos'], name: 'streams.get_render', path: (input) => streamRenderPath(input), title: 'Get stream render state' }),
  {
    category: 'artifacts',
    description: 'Return a loopback URL for a stream render gallery or MP4.',
    inputSchema: objectSchema({ clip_id: SAFE_TOKEN_PROPERTY, kind: { enum: ['source', 'gallery', 'video'], type: 'string' }, stream_job_id: UUID_PROPERTY, variant: VARIANT_PROPERTY }, ['stream_job_id', 'kind']),
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
  if (streamJobID !== undefined && (operation.name === 'streams.update_edit_plan' || operation.name === 'streams.configure_captions')) {
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
    const items = schema.items;
    if (isJsonObject(items)) value.forEach((item, index) => validateJsonSchema(items, item, `${path}[${index}]`));
    return;
  }
  if (expectedType === 'string') {
    if (typeof value !== 'string') throw new Error(`${path} must be a string`);
    if (typeof schema.minLength === 'number' && value.length < schema.minLength) throw new Error(`${path} is too short`);
    if (typeof schema.maxLength === 'number' && value.length > schema.maxLength) throw new Error(`${path} is too long`);
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

async function configureStreamCaptions(client: OrchestratorClient, input: JsonObject, signal?: AbortSignal): Promise<JsonValue> {
  const editPlanPath = streamPath(input, '/edit-plan');
  const current = await client.request({ path: editPlanPath, signal });
  if (!isJsonObject(current)) throw new Error('stream edit plan response must be an object');
  const captions: JsonObject = { enabled: input.enabled === true };
  const language = optionalStringInput(input, 'language');
  if (language !== undefined) captions.language = language;
  const updated: JsonObject = { ...current, captions };
  validateJsonSchema(STREAM_EDIT_PLAN_PROPERTY, updated, 'stream edit plan');
  validateStreamEditPlan(updated);
  await validateLiveStreamEditPlan(client, updated, signal);
  return client.request({ body: updated, method: 'PUT', path: editPlanPath, signal });
}

function validateStreamEditPlan(plan: JsonObject): void {
  for (const key of ['face_crop', 'gameplay_crop']) {
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
  const seen = new Set<string>();
  for (const [index, value] of clips.entries()) {
    if (!isJsonObject(value)) continue;
    if (typeof value.start_seconds === 'number' && typeof value.end_seconds === 'number'
      && value.end_seconds <= value.start_seconds) {
      throw new Error(`arguments.plan.clips[${index}].end_seconds must be greater than start_seconds`);
    }
    if (typeof value.id === 'string') {
      if (seen.has(value.id)) throw new Error(`arguments.plan.clips contains duplicate id ${value.id}`);
      seen.add(value.id);
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
