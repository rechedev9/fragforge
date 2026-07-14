import test from 'node:test';
import assert from 'node:assert/strict';
import * as http from 'node:http';
import {
  operationCatalogResource,
  parseSearchRequest,
  searchOperationCatalog,
} from './discovery.ts';
import { isJsonObject, type JsonObject } from './json.ts';
import {
  operationNamed,
  validateOperationInput,
  type OperationDefinition,
} from './operations.ts';
import { OrchestratorClient } from './orchestrator-client.ts';

type HttpResponder = (
  request: http.IncomingMessage,
  response: http.ServerResponse,
) => void | Promise<void>;

async function startServer(t: test.TestContext, responder: HttpResponder): Promise<string> {
  const server = http.createServer((request, response) => {
    void Promise.resolve(responder(request, response)).catch((error: unknown) => {
      response.statusCode = 500;
      response.end(String(error));
    });
  });
  await new Promise<void>((resolve, reject) => {
    server.once('error', reject);
    server.listen(0, '127.0.0.1', resolve);
  });
  const address = server.address();
  if (address === null || typeof address === 'string') throw new Error('fake server has no TCP address');
  t.after(async () => {
    server.closeAllConnections();
    await new Promise<void>((resolve) => server.close(() => resolve()));
  });
  return `http://127.0.0.1:${address.port}`;
}

function sendJson(response: http.ServerResponse, value: unknown, status = 200): void {
  response.statusCode = status;
  response.setHeader('Content-Type', 'application/json');
  response.end(JSON.stringify(value));
}

function operation(name: string): OperationDefinition {
  const found = operationNamed(name);
  if (found === undefined) throw new Error(`missing operation ${name}`);
  return found;
}

function descriptors(result: JsonObject): JsonObject[] {
  const values = result.operations;
  if (!Array.isArray(values)) throw new Error('search result has no operations array');
  return values.filter(isJsonObject);
}

function dynamicFields(descriptor: JsonObject): JsonObject[] {
  const values = descriptor.dynamic_inputs;
  if (!Array.isArray(values)) throw new Error('operation descriptor has no dynamic inputs');
  return values.filter(isJsonObject);
}

function candidateValues(field: JsonObject): unknown[] {
  const values = field.candidates;
  if (!Array.isArray(values)) return [];
  return values
    .filter(isJsonObject)
    .map((candidate) => candidate.value);
}

function descriptorNames(result: JsonObject): unknown[] {
  return descriptors(result).map((descriptor) => descriptor.name);
}

test('parses search defaults and validates limit, risk, and argument shape', () => {
  assert.deepEqual(parseSearchRequest({ query: '  render video  ' }), {
    arguments: {},
    category: undefined,
    includeDynamicInputs: true,
    limit: 8,
    operation: undefined,
    query: 'render video',
    risk: undefined,
  });
  assert.equal(parseSearchRequest({ include_dynamic_inputs: false }).includeDynamicInputs, false);
  assert.throws(() => parseSearchRequest({ limit: 0 }), /limit must be from 1 to 20/);
  assert.throws(() => parseSearchRequest({ limit: 21 }), /limit must be from 1 to 20/);
  assert.throws(() => parseSearchRequest({ risk: 'unsafe' }), /risk must be read, write, costly, or destructive/);
  assert.throws(() => parseSearchRequest({ arguments: [] }), /arguments must be an object/);
});

test('returns deterministic default ranking and natural-language ranking', async () => {
  const client = new OrchestratorClient({ baseUrl: 'http://127.0.0.1:1' });
  const defaults = await searchOperationCatalog(
    client,
    parseSearchRequest({ include_dynamic_inputs: false, limit: 6 }),
  );
  assert.deepEqual(descriptorNames(defaults), [
    'studio.status',
    'jobs.list',
    'jobs.create',
    'jobs.generate',
    'renders.get',
    'catalog.presets',
  ]);

  const first = await searchOperationCatalog(
    client,
    parseSearchRequest({ include_dynamic_inputs: false, limit: 8, query: 'video render' }),
  );
  const second = await searchOperationCatalog(
    client,
    parseSearchRequest({ include_dynamic_inputs: false, limit: 8, query: 'video render' }),
  );
  assert.deepEqual(descriptorNames(first), descriptorNames(second));

  const destructive = await searchOperationCatalog(
    client,
    parseSearchRequest({ include_dynamic_inputs: false, query: 'delete rendered video' }),
  );
  assert.equal(descriptorNames(destructive)[0], 'renders.delete_video');

  const subtitles = await searchOperationCatalog(
    client,
    parseSearchRequest({ include_dynamic_inputs: false, query: 'subtitulos con grok' }),
  );
  assert.equal(descriptorNames(subtitles)[0], 'streams.configure_captions');
});

test('ranks common Spanish and English intents by whole tokens', async () => {
  const client = new OrchestratorClient({ baseUrl: 'http://127.0.0.1:1' });
  const cases = [
    ['estado del estudio', 'studio.status'],
    ['subir una demo', 'jobs.create'],
    ['ver trabajos recientes', 'jobs.list'],
    ['grabar kills', 'jobs.record'],
    ['descargar video', 'artifacts.get_url'],
    ['lista de canciones', 'catalog.songs'],
    ['ver errores', 'studio.metrics'],
    ['download video', 'artifacts.get_url'],
    ['recent jobs', 'jobs.list'],
  ] as const;

  for (const [query, expected] of cases) {
    const result = await searchOperationCatalog(
      client,
      parseSearchRequest({ include_dynamic_inputs: false, limit: 1, query }),
    );
    assert.equal(descriptorNames(result)[0], expected, query);
  }

  const status = await searchOperationCatalog(
    client,
    parseSearchRequest({ include_dynamic_inputs: false, query: 'estado del estudio' }),
  );
  assert.ok(!descriptorNames(status).slice(0, 3).includes('renders.delete_video'));
});

test('filters by category and risk and supports exact operation lookup', async () => {
  const client = new OrchestratorClient({ baseUrl: 'http://127.0.0.1:1' });
  const filtered = await searchOperationCatalog(
    client,
    parseSearchRequest({
      category: 'renders',
      include_dynamic_inputs: false,
      query: '',
      risk: 'destructive',
    }),
  );
  assert.deepEqual(descriptorNames(filtered), ['renders.delete_video']);

  const exact = await searchOperationCatalog(
    client,
    parseSearchRequest({ include_dynamic_inputs: false, operation: 'jobs.generate', query: 'ignored' }),
  );
  assert.deepEqual(descriptorNames(exact), ['jobs.generate']);
  assert.equal(descriptors(exact)[0]?.risk, 'costly');

  const categoryMismatch = await searchOperationCatalog(
    client,
    parseSearchRequest({ category: 'studio', include_dynamic_inputs: false, operation: 'jobs.generate' }),
  );
  assert.deepEqual(descriptorNames(categoryMismatch), []);

  const riskMismatch = await searchOperationCatalog(
    client,
    parseSearchRequest({ include_dynamic_inputs: false, operation: 'jobs.generate', risk: 'read' }),
  );
  assert.deepEqual(descriptorNames(riskMismatch), []);

  const missing = await searchOperationCatalog(
    client,
    parseSearchRequest({ include_dynamic_inputs: false, operation: 'not.an.operation' }),
  );
  assert.deepEqual(descriptorNames(missing), []);
});

test('keeps the static schema when live dynamic discovery is offline', async () => {
  const client = new OrchestratorClient({
    baseUrl: 'http://127.0.0.1:1',
    requestTimeoutMs: 50,
  });
  const result = await searchOperationCatalog(
    client,
    parseSearchRequest({ operation: 'jobs.get' }),
  );
  const descriptor = descriptors(result)[0];
  if (descriptor === undefined) throw new Error('expected jobs.get descriptor');

  assert.equal(descriptor.name, 'jobs.get');
  assert.ok(isJsonObject(descriptor.input_schema));
  const fields = dynamicFields(descriptor);
  assert.equal(fields[0]?.field, 'job_id');
  assert.equal(fields[0]?.unavailable, true);
  assert.match(String(fields[0]?.error), /Studio is offline or unreachable/);
  assert.equal(descriptor.dynamic_inputs_error, 'dynamic inputs unavailable for: job_id');
});

test('preserves successful dynamic fields when one source is unavailable', async (t) => {
  const baseUrl = await startServer(t, (request, response) => {
    const url = request.url ?? '';
    if (url === '/api/jobs?limit=100') {
      sendJson(response, { jobs: [{ id: 'job-a', status: 'parsed' }] });
      return;
    }
    if (url === '/api/presets') {
      sendJson(response, { error: 'preset registry unavailable' }, 503);
      return;
    }
    if (url === '/api/songs') {
      sendJson(response, { songs: [{ id: 'track-1', title: 'Drop' }] });
      return;
    }
    if (url === '/api/jobs/job-a/moments') {
      sendJson(response, { moments: [{ segment_id: 'seg-1' }] });
      return;
    }
    sendJson(response, { error: `unexpected ${url}` }, 404);
  });
  const client = new OrchestratorClient({ baseUrl });
  const result = await searchOperationCatalog(
    client,
    parseSearchRequest({ arguments: { job_id: 'job-a' }, operation: 'jobs.generate' }),
  );
  const descriptor = descriptors(result)[0];
  if (descriptor === undefined) throw new Error('expected jobs.generate descriptor');
  const fields = dynamicFields(descriptor);

  assert.deepEqual(fields.map((field) => field.field), ['job_id', 'preset', 'music', 'segment_ids']);
  assert.deepEqual(candidateValues(fields[0] ?? {}), ['job-a']);
  assert.equal(fields[1]?.unavailable, true);
  assert.match(String(fields[1]?.error), /preset registry unavailable/);
  assert.deepEqual(candidateValues(fields[2] ?? {}), ['track-1']);
  assert.deepEqual(candidateValues(fields[3] ?? {}), ['seg-1']);
  assert.equal(descriptor.dynamic_inputs_error, 'dynamic inputs unavailable for: preset');
});

test('discovers live jobs, roster, presets, songs, moments, and artifact names', async (t) => {
  const calls: string[] = [];
  const baseUrl = await startServer(t, (request, response) => {
    const url = request.url ?? '';
    calls.push(url);
    if (url === '/api/jobs?limit=100') {
      sendJson(response, {
        jobs: [
          { id: 'job-a', kill_plan: { should_not_leak: 'large-plan' }, status: 'parsed', target_steamid: '76561198000000001' },
          { id: 7, status: 'ignored-non-string-id' },
        ],
      });
      return;
    }
    if (url === '/api/jobs/job-a/roster') {
      sendJson(response, {
        players: [
          { name: 'Martínez', steamid64: '76561198000000001', team: 'CT' },
          { name: 'invalid without steam id' },
        ],
      });
      return;
    }
    if (url === '/api/jobs/job-a/moments') {
      sendJson(response, {
        moments: [
          { id: 'seg-fallback', label: 'fallback id', round: 4, score: 0.8 },
          { label: 'Ace', round: 8, score: 0.99, segment_id: 'seg-ace' },
        ],
      });
      return;
    }
    if (url === '/api/presets') {
      sendJson(response, {
        presets: [
          { default: true, description: 'Clean vertical short', label: 'Viral clean', name: 'viral-60-clean' },
        ],
      });
      return;
    }
    if (url === '/api/songs') {
      sendJson(response, {
        songs: [
          { artist: 'CC Artist', genre: 'electronic', id: 'track-1', license: 'CC0', title: 'Drop' },
        ],
      });
      return;
    }
    if (url === '/api/stream-variants') {
      sendJson(response, {
        default: 'streamer-vertical-stack-40-60',
        variants: [
          { default: true, description: 'Facecam over gameplay', label: '40 / 60', name: 'streamer-vertical-stack-40-60' },
        ],
      });
      return;
    }
    if (url === '/api/stream-jobs?limit=100') {
      sendJson(response, { jobs: [{ id: 'stream-a', status: 'ready', title: 'Stream A' }] });
      return;
    }
    if (url === '/api/jobs/job-a/renders/viral-60-clean') {
      sendJson(response, {
        covers: ['long-compilation'],
        videos: ['long-compilation', 'seg-ace'],
      });
      return;
    }
    if (url === '/api/jobs/job-a/renders/viral-60-clean/publish') {
      sendJson(response, {
        items: [
          { caption_ready: true, segment_id: 'long-compilation', status: 'ready' },
          { caption_ready: false, segment_id: 'seg-ace', status: 'needs_caption' },
        ],
      });
      return;
    }
    if (url === '/api/stream-jobs/stream-a/edit-plan') {
      sendJson(response, {
        captions: { enabled: false },
        clips: [{ end_seconds: 12, id: 'clip-1', start_seconds: 2 }],
        gameplay_crop: { height: 1, width: 1, x: 0, y: 0 },
        schema_version: '1.0',
        variant: 'streamer-vertical-stack-40-60',
      });
      return;
    }
    if (url === '/api/stream-jobs/stream-a/renders/streamer-vertical-stack-40-60') {
      sendJson(response, { videos: [{ clip_id: 'clip-1', duration_seconds: 10, title: 'Clutch' }] });
      return;
    }
    sendJson(response, { error: `unexpected ${url}` }, 404);
  });
  const client = new OrchestratorClient({ baseUrl });

  const generate = await searchOperationCatalog(
    client,
    parseSearchRequest({ arguments: { job_id: 'job-a' }, operation: 'jobs.generate' }),
  );
  const generateDescriptor = descriptors(generate)[0];
  if (generateDescriptor === undefined) throw new Error('expected jobs.generate descriptor');
  const generateFields = dynamicFields(generateDescriptor);
  assert.deepEqual(generateFields.map((field) => field.field), [
    'job_id',
    'preset',
    'music',
    'segment_ids',
  ]);
  assert.deepEqual(candidateValues(generateFields[0] ?? {}), ['job-a']);
  assert.doesNotMatch(JSON.stringify(generateFields[0]), /large-plan/);
  assert.deepEqual(candidateValues(generateFields[1] ?? {}), ['viral-60-clean']);
  assert.deepEqual(candidateValues(generateFields[2] ?? {}), ['track-1']);
  assert.deepEqual(candidateValues(generateFields[3] ?? {}), ['seg-fallback', 'seg-ace']);

  const parse = await searchOperationCatalog(
    client,
    parseSearchRequest({ arguments: { job_id: 'job-a' }, operation: 'jobs.parse' }),
  );
  const parseDescriptor = descriptors(parse)[0];
  if (parseDescriptor === undefined) throw new Error('expected jobs.parse descriptor');
  const parseFields = dynamicFields(parseDescriptor);
  assert.deepEqual(parseFields.map((field) => field.field), ['job_id', 'target_steamid']);
  assert.deepEqual(candidateValues(parseFields[1] ?? {}), ['76561198000000001']);

  const artifact = await searchOperationCatalog(
    client,
    parseSearchRequest({
      arguments: { job_id: 'job-a', kind: 'video', variant: 'viral-60-clean' },
      operation: 'artifacts.get_url',
    }),
  );
  const artifactDescriptor = descriptors(artifact)[0];
  if (artifactDescriptor === undefined) throw new Error('expected artifacts.get_url descriptor');
  const artifactFields = dynamicFields(artifactDescriptor);
  assert.deepEqual(artifactFields.map((field) => field.field), ['job_id', 'variant', 'name']);
  assert.deepEqual(artifactFields[2]?.candidates, [
    { value: 'long-compilation' },
    { value: 'seg-ace' },
  ]);

  const captionArtifact = await searchOperationCatalog(
    client,
    parseSearchRequest({
      arguments: { job_id: 'job-a', kind: 'caption', variant: 'viral-60-clean' },
      operation: 'artifacts.get_url',
    }),
  );
  const captionFields = dynamicFields(descriptors(captionArtifact)[0] ?? {});
  assert.deepEqual(captionFields[2]?.candidates, [
    { context: { status: 'ready' }, value: 'long-compilation' },
  ]);

  assert.ok(calls.includes('/api/jobs/job-a/roster'));
  assert.ok(calls.includes('/api/jobs/job-a/moments'));
  assert.ok(calls.includes('/api/jobs/job-a/renders/viral-60-clean'));

  const streamRender = await searchOperationCatalog(
    client,
    parseSearchRequest({ operation: 'streams.start_render' }),
  );
  const streamDescriptor = descriptors(streamRender)[0];
  if (streamDescriptor === undefined) throw new Error('expected streams.start_render descriptor');
  const streamFields = dynamicFields(streamDescriptor);
  assert.deepEqual(streamFields.map((field) => field.field), ['stream_job_id', 'variant']);
  assert.deepEqual(candidateValues(streamFields[1] ?? {}), ['streamer-vertical-stack-40-60']);

  const captions = await searchOperationCatalog(
    client,
    parseSearchRequest({ arguments: { stream_job_id: 'stream-a' }, operation: 'streams.configure_captions' }),
  );
  const captionSettings = dynamicFields(descriptors(captions)[0] ?? {});
  assert.deepEqual(captionSettings.map((field) => field.field), ['stream_job_id']);

  const streamVideo = await searchOperationCatalog(
    client,
    parseSearchRequest({
      arguments: { kind: 'video', stream_job_id: 'stream-a', variant: 'streamer-vertical-stack-40-60' },
      operation: 'artifacts.get_stream_url',
    }),
  );
  const streamVideoFields = dynamicFields(descriptors(streamVideo)[0] ?? {});
  assert.deepEqual(streamVideoFields.map((field) => field.field), ['stream_job_id', 'variant', 'clip_id']);
  assert.deepEqual(candidateValues(streamVideoFields[2] ?? {}), ['clip-1']);
});

test('validates operation inputs including conditional artifact requirements', () => {
  const jobID = '11111111-1111-4111-8111-111111111111';
  const streamJobID = '22222222-2222-4222-8222-222222222222';
  validateOperationInput(operation('jobs.generate'), {
    job_id: jobID,
    preset: 'viral-60-clean',
    segment_ids: ['seg-1'],
  });
  assert.throws(
    () => validateOperationInput(operation('jobs.generate'), { job_id: jobID }),
    /arguments.preset is required/,
  );
  assert.throws(
    () => validateOperationInput(operation('jobs.list'), { limit: 101 }),
    /arguments.limit is above the maximum/,
  );
  assert.throws(
    () => validateOperationInput(operation('jobs.list'), { unexpected: true }),
    /arguments.unexpected is not allowed/,
  );
  assert.throws(
    () => validateOperationInput(operation('streams.create_from_url'), { source_url: 'not a URL' }),
    /arguments.source_url must be a valid URL/,
  );
  assert.throws(
    () => validateOperationInput(operation('jobs.create'), { demo_path: 'C:\\match.dem', rules: { min_round: 2 } }),
    /arguments\.rules\.weapons is required/,
  );
  assert.throws(
    () => validateOperationInput(operation('jobs.record'), { edit: {}, job_id: jobID }),
    /arguments\.preset is required when arguments\.edit is provided/,
  );

  validateOperationInput(operation('artifacts.get_url'), { job_id: jobID, kind: 'final' });
  assert.throws(
    () => validateOperationInput(operation('artifacts.get_url'), { job_id: jobID, kind: 'gallery' }),
    /arguments.variant is required/,
  );
  assert.throws(
    () => validateOperationInput(operation('artifacts.get_url'), {
      job_id: jobID,
      kind: 'video',
      variant: 'viral-60-clean',
    }),
    /arguments.name is required/,
  );
  assert.throws(
    () => validateOperationInput(operation('artifacts.get_stream_url'), {
      kind: 'video',
      stream_job_id: streamJobID,
      variant: 'streamer-fullframe-nocam',
    }),
    /arguments.clip_id is required/,
  );
  validateOperationInput(operation('artifacts.get_stream_url'), {
    kind: 'source',
    stream_job_id: streamJobID,
  });
  assert.throws(
    () => validateOperationInput(operation('artifacts.get_stream_url'), {
      kind: 'gallery',
      stream_job_id: streamJobID,
    }),
    /arguments.variant is required/,
  );
  assert.throws(
    () => validateOperationInput(operation('renders.publish_assistant'), {
      days: 15,
      job_id: jobID,
      name: 'clip',
      variant: 'viral-60-clean',
    }),
    /arguments.days is above the maximum/,
  );
  assert.throws(
    () => validateOperationInput(operation('renders.start'), {
      edit: { hookText: true },
      job_id: jobID,
      variant: 'viral-60-clean',
    }),
    /arguments.edit.hookText is not allowed/,
  );
  validateOperationInput(operation('streams.update_edit_plan'), {
    plan: {
      captions: { enabled: true, language: 'es' },
      clips: [{ end_seconds: 8, id: 'clip-1', start_seconds: 2 }],
      face_crop: { height: 0, width: 0, x: 0, y: 0 },
      gameplay_crop: { height: 1, width: 1, x: 0, y: 0 },
      schema_version: '1.0',
      variant: 'streamer-fullframe-nocam',
    },
    stream_job_id: streamJobID,
  });
  assert.throws(
    () => validateOperationInput(operation('streams.update_edit_plan'), {
      plan: {
        captions: { enabled: true, languaje: 'es' },
        gameplay_crop: { height: 1, width: 1, x: 0, y: 0 },
        variant: 'streamer-vertical-stack-40-60',
      },
      stream_job_id: streamJobID,
    }),
    /arguments.plan.captions.languaje is not allowed/,
  );
  validateOperationInput(operation('streams.update_edit_plan'), {
    plan: {
      face_crop: { height: 0, width: 0, x: 0, y: 0 },
      gameplay_crop: { height: 1, width: 1, x: 0, y: 0 },
      variant: 'streamer-vertical-stack-40-60',
    },
    stream_job_id: streamJobID,
  });
});

test('builds encoded, non-executing mutation previews with only API body fields', () => {
  const parsePreview = operation('jobs.parse').preview({
    job_id: 'job/with/slashes',
    rules: { best: true },
    target_steamid: '76561198000000001',
  });
  assert.deepEqual(parsePreview, {
    body: {
      rules: { best: true },
      target_steamid: '76561198000000001',
    },
    method: 'POST',
    path: '/api/jobs/job%2Fwith%2Fslashes/parse',
  });

  const deletePreview = operation('renders.delete_video').preview({
    job_id: 'job/../other',
    kind: 'ignored',
    name: 'clip/../../name',
    variant: 'viral/60-clean',
  });
  assert.deepEqual(deletePreview, {
    method: 'DELETE',
    path: '/api/jobs/job%2F..%2Fother/renders/viral%2F60-clean/videos/clip%2F..%2F..%2Fname',
  });
  assert.equal(operation('renders.delete_video').risk, 'destructive');
  assert.equal(operation('jobs.generate').risk, 'costly');
});

test('exposes a static catalog resource without requiring Studio', () => {
  const catalog = operationCatalogResource();
  const catalogOperations = descriptors(catalog);

  assert.ok(catalogOperations.length > 20);
  assert.equal(catalog.generated_from, 'desktop/src/mcp/operations.ts');
  assert.match(String(catalog.pattern), /progressive disclosure/);
  assert.ok(catalogOperations.every((descriptor) => isJsonObject(descriptor.input_schema)));
});
