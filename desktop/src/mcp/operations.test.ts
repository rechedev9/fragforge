import assert from 'node:assert/strict';
import test from 'node:test';
import { isJsonObject, type JsonObject, type JsonValue } from './json.ts';
import {
  discoverDynamicInputs,
  MissingOperationInputError,
  operationNamed,
  validateJsonSchema,
  validateLiveOperationInput,
  validateOperationInput,
  type OperationDefinition,
} from './operations.ts';
import { OrchestratorClient, type OrchestratorRequest } from './orchestrator-client.ts';

const JOB_ID = '11111111-1111-4111-8111-111111111111';
const STREAM_JOB_ID = '22222222-2222-4222-8222-222222222222';

interface ClientDoubleState {
  artifactPaths: string[];
  requests: OrchestratorRequest[];
  uploads: Array<{ config: JsonObject; filePath: string }>;
  voiceUploads: Array<{ filePath: string; id: string; metadata: JsonObject }>;
}

type RequestResponder = (request: OrchestratorRequest, index: number) => JsonValue | Promise<JsonValue>;

function clientDouble(responder: RequestResponder): { client: OrchestratorClient; state: ClientDoubleState } {
  const state: ClientDoubleState = { artifactPaths: [], requests: [], uploads: [], voiceUploads: [] };
  const client = {
    artifactUrl: async (apiPath: string): Promise<string> => {
      state.artifactPaths.push(apiPath);
      return `http://127.0.0.1:8080${apiPath}`;
    },
    request: async (request: OrchestratorRequest): Promise<JsonValue> => {
      const index = state.requests.length;
      state.requests.push(request);
      return responder(request, index);
    },
    uploadStreamVideo: async (filePath: string, config: JsonObject): Promise<JsonValue> => {
      state.uploads.push({ config, filePath });
      return { id: STREAM_JOB_ID, status: 'uploaded' };
    },
    uploadVoiceProfile: async (id: string, filePath: string, metadata: JsonObject): Promise<JsonValue> => {
      state.voiceUploads.push({ filePath, id, metadata });
      return { id, name: metadata.name ?? 'Mi voz' };
    },
  };
  // The operation intentionally depends only on this narrow external-client surface.
  return { client: client as unknown as OrchestratorClient, state };
}

function operation(name: string): OperationDefinition {
  const found = operationNamed(name);
  if (found === undefined) throw new Error(`missing operation ${name}`);
  return found;
}

function objectResult(value: JsonValue): JsonObject {
  if (!isJsonObject(value)) throw new Error('expected an object result');
  return value;
}

function objectField(value: JsonObject, field: string): JsonObject {
  const child = value[field];
  if (!isJsonObject(child)) throw new Error(`expected object field ${field}`);
  return child;
}

function recoveryOperations(result: JsonObject): unknown[] {
  const recovery = objectField(result, 'recovery');
  if (!Array.isArray(recovery.steps)) throw new Error('expected recovery steps');
  return recovery.steps.filter(isJsonObject).map((step) => step.operation);
}

test('streams.start_render sends the approved edit-plan revision as a request precondition', async () => {
  const double = clientDouble(() => ({ status: 'rendering' }));
  const revision = '2026-07-20T20:00:00.123Z';

  await operation('streams.start_render').run(double.client, {
    expected_edit_plan_updated_at: revision,
    stream_job_id: STREAM_JOB_ID,
    variant: 'streamer-vertical-stack-40-60',
  });

  assert.deepEqual(double.state.requests, [{
    body: { expected_edit_plan_updated_at: revision },
    method: 'POST',
    path: `/api/stream-jobs/${STREAM_JOB_ID}/renders/streamer-vertical-stack-40-60`,
    signal: undefined,
  }]);
});

test('streams.create_from_file keeps its successful upload-and-initialize result', async () => {
  const plan: JsonObject = { gameplay_crop: { height: 1, width: 1, x: 0, y: 0 } };
  const readyPlan: JsonObject = { ...plan, updated_at: '2026-07-13T12:00:00Z' };
  const readyJob: JsonObject = { id: STREAM_JOB_ID, status: 'ready' };
  const double = clientDouble((_request, index) => {
    if (index === 0) return plan;
    if (index === 1) return readyPlan;
    if (index === 2) return readyJob;
    throw new Error('unexpected request');
  });

  const result = await operation('streams.create_from_file').run(double.client, {
    title: 'Final de Mirage',
    video_path: 'C:\\captures\\match.mp4',
  });

  assert.deepEqual(result, { edit_plan: readyPlan, job: readyJob });
  assert.deepEqual(double.state.uploads, [{
    config: { title: 'Final de Mirage' },
    filePath: 'C:\\captures\\match.mp4',
  }]);
  assert.deepEqual(double.state.requests.map((request) => [request.method ?? 'GET', request.path]), [
    ['GET', `/api/stream-jobs/${STREAM_JOB_ID}/edit-plan`],
    ['PUT', `/api/stream-jobs/${STREAM_JOB_ID}/edit-plan`],
    ['GET', `/api/stream-jobs/${STREAM_JOB_ID}`],
  ]);
  assert.deepEqual(double.state.requests[1]?.body, plan);
});

test('streams.create_from_file returns recoverable partial results without re-uploading', async (t) => {
  const plan: JsonObject = { gameplay_crop: { height: 1, width: 1, x: 0, y: 0 } };
  const readyPlan: JsonObject = { ...plan, updated_at: '2026-07-13T12:00:00Z' };
  const cases: Array<{
    expectedOperations: string[];
    failedStep: string;
    requestCount: number;
    respond: RequestResponder;
  }> = [
    {
      expectedOperations: ['streams.resume_initialization'],
      failedStep: 'load_edit_plan',
      requestCount: 1,
      respond: () => {
        throw new Error('edit plan temporarily unavailable');
      },
    },
    {
      expectedOperations: ['streams.resume_initialization'],
      failedStep: 'persist_edit_plan',
      requestCount: 2,
      respond: (_request, index) => {
        if (index === 0) return plan;
        throw new Error('ready transition timed out');
      },
    },
    {
      expectedOperations: ['streams.resume_initialization'],
      failedStep: 'refresh_job',
      requestCount: 3,
      respond: (_request, index) => {
        if (index === 0) return plan;
        if (index === 1) return readyPlan;
        throw new Error('job refresh failed');
      },
    },
  ];

  for (const scenario of cases) {
    await t.test(scenario.failedStep, async () => {
      const double = clientDouble(scenario.respond);
      const result = objectResult(await operation('streams.create_from_file').run(double.client, {
        video_path: 'C:\\captures\\match.mp4',
      }));

      assert.equal(result.partial, true);
      assert.equal(result.stream_job_id, STREAM_JOB_ID);
      assert.deepEqual(result.job, { id: STREAM_JOB_ID, status: 'uploaded' });
      const initialization = objectField(result, 'initialization');
      assert.equal(initialization.failed_step, scenario.failedStep);
      assert.equal(initialization.status, 'partial');
      const recovery = objectField(result, 'recovery');
      assert.equal(recovery.reupload_required, false);
      assert.equal(recovery.retry_create_from_file, false);
      assert.equal(recovery.safe_to_retry_steps, true);
      assert.deepEqual(recoveryOperations(result), scenario.expectedOperations);
      assert.equal(double.state.uploads.length, 1);
      assert.equal(double.state.requests.length, scenario.requestCount);

      if (!Array.isArray(recovery.steps)) throw new Error('expected executable recovery steps');
      assert.deepEqual(recovery.steps, [{
        arguments: { stream_job_id: STREAM_JOB_ID },
        confirmed: true,
        mode: 'apply',
        operation: 'streams.resume_initialization',
      }]);
      const step = recovery.steps[0];
      if (!isJsonObject(step) || typeof step.operation !== 'string' || !isJsonObject(step.arguments)) {
        throw new Error('expected executable recovery step');
      }
      validateOperationInput(operation(step.operation), step.arguments);
      if (scenario.failedStep === 'refresh_job') assert.deepEqual(result.edit_plan, readyPlan);
    });
  }
});

test('streams.create_from_file preserves one durable job and recovery steps after cancellation', async (t) => {
  const plan: JsonObject = { gameplay_crop: { height: 1, width: 1, x: 0, y: 0 } };
  const stages = [0, 1, 2];

  for (const failedRequest of stages) {
    await t.test(`request ${failedRequest + 1}`, async () => {
      const controller = new AbortController();
      const cancellation = new Error('evaluation cancelled');
      cancellation.name = 'AbortError';
      let recovering = false;
      const double = clientDouble((request, index) => {
        if (!recovering) {
          if (index < failedRequest) return plan;
          controller.abort();
          throw cancellation;
        }
        if (request.path.endsWith('/edit-plan')) return request.body ?? plan;
        return { id: STREAM_JOB_ID, status: 'ready' };
      });

      const result = objectResult(await operation('streams.create_from_file').run(double.client, {
        video_path: 'C:\\captures\\match.mp4',
      }, controller.signal));
      assert.equal(result.partial, true);
      assert.equal(result.cancelled, true);
      assert.equal(result.stream_job_id, STREAM_JOB_ID);
      const cancellationError = result.error;
      if (typeof cancellationError !== 'string') throw new Error('expected a cancellation error');
      assert.match(cancellationError, /do not upload the file again/);
      const recovery = objectField(result, 'recovery');
      assert.equal(recovery.retry_create_from_file, false);
      assert.equal(double.state.uploads.length, 1);
      assert.equal(double.state.requests.length, failedRequest + 1);

      recovering = true;
      if (!Array.isArray(recovery.steps) || !isJsonObject(recovery.steps[0])) {
        throw new Error('expected executable recovery step');
      }
      const step = recovery.steps[0];
      assert.deepEqual(step, {
        arguments: { stream_job_id: STREAM_JOB_ID },
        confirmed: true,
        mode: 'apply',
        operation: 'streams.resume_initialization',
      });
      if (typeof step.operation !== 'string' || !isJsonObject(step.arguments)) {
        throw new Error('expected operation and arguments in recovery step');
      }
      const recovered = objectResult(await operation(step.operation).run(double.client, step.arguments));
      const ready = objectField(recovered, 'job');
      assert.equal(ready.status, 'ready');
      assert.equal(double.state.uploads.length, 1, 'recovery must not create a second stream job');
    });
  }
});

test('operation schemas enforce API route and cross-field contracts', () => {
  validateOperationInput(operation('jobs.get'), { job_id: JOB_ID });
  assert.throws(
    () => validateOperationInput(operation('jobs.get'), { job_id: 'not-a-uuid' }),
    /arguments.job_id has an invalid format/,
  );
  assert.throws(
    () => validateOperationInput(operation('streams.get'), { stream_job_id: 'x' }),
    /arguments.stream_job_id has an invalid format/,
  );

  validateOperationInput(operation('streams.create_from_url'), { source_url: 'HTTPS://clips.twitch.tv/SomeSlug' });
  validateOperationInput(operation('streams.create_from_url'), { source_url: 'https://www.twitch.tv/videos/123456' });
  assert.throws(
    () => validateOperationInput(operation('streams.create_from_url'), { source_url: 'file:///C:/captures/stream.mp4' }),
    /arguments.source_url has an invalid format/,
  );
  assert.throws(
    () => validateOperationInput(operation('streams.create_from_url'), { source_url: 'https://example.com/video' }),
    /arguments.source_url has an invalid format/,
  );
  assert.throws(
    () => validateOperationInput(operation('streams.create_from_url'), { source_url: 'https://www.twitch.tv/directory' }),
    /arguments.source_url has an invalid format/,
  );

  for (const name of ['', '..', 'clip.mp4', 'clip/name']) {
    assert.throws(
      () => validateOperationInput(operation('renders.delete_video'), {
        job_id: JOB_ID,
        name,
        variant: 'viral-60-clean',
      }),
      /arguments.name has an invalid format/,
    );
  }
  validateOperationInput(operation('renders.delete_video'), {
    job_id: JOB_ID,
    name: 'long-compilation_1',
    variant: 'viral-60-clean',
  });
  for (const variant of ['', '..', 'bad/variant']) {
    assert.throws(
      () => validateOperationInput(operation('streams.start_render'), {
        stream_job_id: STREAM_JOB_ID,
        variant,
      }),
      /arguments.variant has an invalid format/,
    );
  }
  for (const preset of ['', '..', 'bad/preset']) {
    assert.throws(
      () => validateOperationInput(operation('jobs.record'), {
        job_id: JOB_ID,
        preset,
      }),
      /arguments.preset has an invalid format/,
    );
  }
  assert.throws(
    () => validateOperationInput(operation('artifacts.get_stream_url'), {
      clip_id: '..',
      kind: 'video',
      stream_job_id: STREAM_JOB_ID,
      variant: 'streamer-fullframe-nocam',
    }),
    /arguments.clip_id has an invalid format/,
  );

  const rules: JsonObject = {
    exclude_team_kills: true,
    include_headshot_only: false,
    max_round: 3,
    min_kills_in_window: 1,
    min_round: 4,
    post_roll_seconds: 2,
    pre_roll_seconds: 2,
    weapons: ['ak47'],
    window_seconds: 10,
  };
  assert.throws(
    () => validateOperationInput(operation('jobs.create'), { demo_path: 'C:\\match.dem', rules }),
    /max_round must be zero or greater than or equal to min_round/,
  );
  validateOperationInput(operation('jobs.create'), {
    demo_path: 'C:\\match.dem',
    rules: { ...rules, max_round: 0 },
  });

  const updateEditPlan = operation('streams.update_edit_plan');
  const validateStreamEditPlan = (plan: JsonObject): void => {
    validateOperationInput(updateEditPlan, { plan, stream_job_id: STREAM_JOB_ID });
  };
  const legacyStreamEditPlan: JsonObject = {
    gameplay_crop: { height: 1, width: 1, x: 0, y: 0 },
    variant: 'streamer-vertical-stack-40-60',
  };
  validateStreamEditPlan(legacyStreamEditPlan);
  assert.deepEqual(legacyStreamEditPlan, {
    gameplay_crop: { height: 1, width: 1, x: 0, y: 0 },
    variant: 'streamer-vertical-stack-40-60',
  });
  validateStreamEditPlan({
    gameplay_crop: { height: 1, width: 1, x: 0, y: 0 },
    variant: 'streamer-fullframe-nocam',
  });

  const killfeedClip: JsonObject = {
    end_seconds: 18,
    id: 'ace',
    killfeed_seconds: [12, 14.5, 17.999],
    start_seconds: 12,
    title: 'Ace',
  };
  const validKillfeedPlan: JsonObject = {
    clips: [killfeedClip],
    gameplay_crop: { height: 1, width: 1, x: 0, y: 0 },
    killfeed_crop: { height: 0.14, width: 0.25, x: 0.72, y: 0.04 },
    schema_version: '1.1',
    variant: 'streamer-vertical-stack-40-60',
  };
  validateStreamEditPlan(validKillfeedPlan);

  const killJson: JsonObject = {
    attacker_name: 'hero',
    attacker_side: 'CT',
    victim_name: 'villain',
    victim_side: 'T',
    weapon: 'ak47',
  };
  validateStreamEditPlan({
    ...validKillfeedPlan,
    clips: [{ ...killfeedClip, killfeed_kills: [[killJson], [], [{ ...killJson, headshot: true }]] }],
  });

  const invalidKillfeedPlans: Array<{ error: string; name: string; plan: JsonObject }> = [
    {
      error: 'arguments.plan.clips[0].killfeed_kills must have one entry per killfeed_seconds cue',
      name: 'killfeed_kills length mismatch',
      plan: {
        ...validKillfeedPlan,
        clips: [{ ...killfeedClip, killfeed_kills: [[killJson]] }],
      },
    },
    {
      error: 'arguments.plan.clips[0].killfeed_kills[0][0].attacker_side must be one of: CT, T',
      name: 'killfeed kill invalid side',
      plan: {
        ...validKillfeedPlan,
        clips: [{
          ...killfeedClip,
          killfeed_seconds: [12],
          killfeed_kills: [[{ ...killJson, attacker_side: 'X' }]],
        }],
      },
    },
    {
      error: 'arguments.plan.killfeed_crop is required when arguments.plan.clips[0].killfeed_seconds contains cues',
      name: 'cues without killfeed_crop',
      plan: {
        clips: [killfeedClip],
        gameplay_crop: { height: 1, width: 1, x: 0, y: 0 },
        schema_version: '1.0',
        variant: 'streamer-vertical-stack-40-60',
      },
    },
    {
      error: 'arguments.plan.killfeed_crop must stay within the source frame',
      name: 'killfeed_crop outside the source frame',
      plan: {
        ...validKillfeedPlan,
        killfeed_crop: { height: 0.14, width: 0.25, x: 0.8, y: 0.04 },
      },
    },
    {
      error: 'arguments.plan.clips[0].killfeed_seconds[1] must not duplicate an earlier cue',
      name: 'duplicate killfeed cue',
      plan: {
        ...validKillfeedPlan,
        clips: [{ ...killfeedClip, killfeed_seconds: [12, 12] }],
      },
    },
    {
      error: 'arguments.plan.clips[0].killfeed_seconds[0] must be greater than or equal to start_seconds and less than end_seconds',
      name: 'killfeed cue before clip start',
      plan: {
        ...validKillfeedPlan,
        clips: [{ ...killfeedClip, killfeed_seconds: [11.999] }],
      },
    },
    {
      error: 'arguments.plan.clips[0].killfeed_seconds[0] must be greater than or equal to start_seconds and less than end_seconds',
      name: 'killfeed cue exactly at clip end',
      plan: {
        ...validKillfeedPlan,
        clips: [{ ...killfeedClip, killfeed_seconds: [18] }],
      },
    },
  ];
  for (const invalid of invalidKillfeedPlans) {
    assert.throws(
      () => validateStreamEditPlan(invalid.plan),
      (error: unknown) => error instanceof Error && error.message === invalid.error,
      invalid.name,
    );
  }
  assert.throws(
    () => validateOperationInput(operation('streams.update_edit_plan'), {
      plan: {
        face_crop: { height: 0, width: 0, x: 0, y: 0 },
        gameplay_crop: { height: 1, width: 1, x: 0, y: 0 },
        variant: 'bad/variant',
      },
      stream_job_id: STREAM_JOB_ID,
    }),
    /arguments.plan.variant has an invalid format/,
  );
});

test('stream caption and killfeed analysis operations validate and dispatch exact requests', async () => {
  const generationID = '33333333-3333-4333-8333-333333333333';
  const reviewInput: JsonObject = {
    clips: [{
      clip_id: 'clip-1',
      words: [{ end_seconds: 0.5, start_seconds: 0, word: 'hola' }],
    }],
    generation_id: generationID,
    stream_job_id: STREAM_JOB_ID,
  };
  const applyInput: JsonObject = { generation_id: generationID, stream_job_id: STREAM_JOB_ID };

  validateOperationInput(operation('streams.review_caption_candidates'), reviewInput);
  validateOperationInput(operation('streams.apply_killfeed_analysis'), applyInput);
  assert.throws(
    () => validateOperationInput(operation('streams.review_caption_candidates'), {
      ...reviewInput,
      clips: [{
        clip_id: 'clip-1',
        no_speech: true,
        words: [{ end_seconds: 0.5, start_seconds: 0, word: 'hola' }],
      }],
    }),
    /cannot include words when no_speech is true/,
  );
  assert.throws(
    () => validateOperationInput(operation('streams.review_caption_candidates'), {
      ...reviewInput,
      clips: [{ clip_id: 'clip-1' }],
    }),
    /requires reviewed words or no_speech=true/,
  );
  assert.throws(
    () => validateOperationInput(operation('streams.review_caption_candidates'), {
      ...reviewInput,
      clips: [
        { clip_id: 'clip-1', no_speech: true },
        { clip_id: 'clip-1', no_speech: true },
      ],
    }),
    /duplicates an earlier review/,
  );
  assert.throws(
    () => validateOperationInput(operation('streams.review_caption_candidates'), {
      ...reviewInput,
      clips: [{
        clip_id: 'clip-1',
        words: [{ end_seconds: 3, start_seconds: 0, word: 'hola' }],
      }],
    }),
    /must last no more than 2.5 seconds/,
  );

  const double = clientDouble((request) => request.body ?? { status: 'ok' });
  await operation('streams.start_caption_candidates').run(double.client, { stream_job_id: STREAM_JOB_ID });
  await operation('streams.get_caption_candidates').run(double.client, { stream_job_id: STREAM_JOB_ID });
  await operation('streams.review_caption_candidates').run(double.client, reviewInput);
  await operation('streams.start_killfeed_analysis').run(double.client, { stream_job_id: STREAM_JOB_ID });
  await operation('streams.get_killfeed_analysis').run(double.client, { stream_job_id: STREAM_JOB_ID });
  await operation('streams.apply_killfeed_analysis').run(double.client, applyInput);

  assert.deepEqual(double.state.requests.map((request) => [request.method ?? 'GET', request.path, request.body]), [
    ['POST', `/api/stream-jobs/${STREAM_JOB_ID}/captions`, undefined],
    ['GET', `/api/stream-jobs/${STREAM_JOB_ID}/captions`, undefined],
    ['POST', `/api/stream-jobs/${STREAM_JOB_ID}/captions/review`, {
      clips: reviewInput.clips,
      generation_id: generationID,
    }],
    ['POST', `/api/stream-jobs/${STREAM_JOB_ID}/killfeed`, undefined],
    ['GET', `/api/stream-jobs/${STREAM_JOB_ID}/killfeed`, undefined],
    ['POST', `/api/stream-jobs/${STREAM_JOB_ID}/killfeed/apply`, { generation_id: generationID }],
  ]);
  assert.equal(operation('streams.start_caption_candidates').risk, 'costly');
  assert.equal(operation('streams.review_caption_candidates').risk, 'write');
  assert.equal(operation('streams.start_killfeed_analysis').risk, 'costly');
  assert.equal(operation('streams.apply_killfeed_analysis').risk, 'write');
});

test('voice profile operations validate IDs and keep reference audio out of MCP results', async () => {
  const profileID = 'raizerinhocs2';
  const input: JsonObject = {
    audio_path: 'C:\\audio\\reference.wav',
    channel: 'RaizerinhoCS2',
    locale: 'es-ES',
    name: 'Mi voz',
    voice_profile_id: profileID,
  };
  validateOperationInput(operation('voices.save_profile'), input);
  for (const voiceProfileID of ['', 'UPPERCASE', '../voice', 'a'.repeat(65)]) {
    assert.throws(
      () => validateOperationInput(operation('voices.get_profile'), { voice_profile_id: voiceProfileID }),
      /arguments.voice_profile_id/,
    );
  }
  assert.throws(
    () => validateOperationInput(operation('voices.save_profile'), { ...input, name: 'a'.repeat(81) }),
    /arguments.name is too long/,
  );

  const double = clientDouble((request) => request.method === 'DELETE' ? null : { id: profileID, name: 'Mi voz' });
  const metadata = await operation('voices.get_profile').run(double.client, { voice_profile_id: profileID });
  const saved = await operation('voices.save_profile').run(double.client, input);
  const audio = await operation('artifacts.get_voice_profile_audio_url').run(double.client, { voice_profile_id: profileID });
  const deleted = await operation('voices.delete_profile').run(double.client, { voice_profile_id: profileID });

  assert.deepEqual(metadata, { id: profileID, name: 'Mi voz' });
  assert.deepEqual(saved, { id: profileID, name: 'Mi voz' });
  assert.deepEqual(audio, { url: `http://127.0.0.1:8080/api/voice-profiles/${profileID}/audio` });
  assert.equal(deleted, null);
  assert.deepEqual(double.state.voiceUploads, [{
    filePath: 'C:\\audio\\reference.wav',
    id: profileID,
    metadata: { channel: 'RaizerinhoCS2', locale: 'es-ES', name: 'Mi voz' },
  }]);
  assert.deepEqual(double.state.artifactPaths, [`/api/voice-profiles/${profileID}/audio`]);
  assert.deepEqual(double.state.requests.map((request) => [request.method ?? 'GET', request.path]), [
    ['GET', `/api/voice-profiles/${profileID}`],
    ['DELETE', `/api/voice-profiles/${profileID}`],
  ]);
  assert.equal(operation('voices.save_profile').risk, 'write');
  assert.equal(operation('voices.delete_profile').risk, 'destructive');
});

test('JSON schema traversal rejects inherited property-name bypasses', () => {
  assert.throws(
    () => validateJsonSchema({ required: ['toString'], type: 'object' }, {}, 'arguments'),
    (error: unknown) => error instanceof MissingOperationInputError && error.field === 'toString',
  );
  assert.throws(
    () => validateJsonSchema({ additionalProperties: false, properties: {}, type: 'object' }, { constructor: 'bypass' }, 'arguments'),
    /arguments.constructor is not allowed/,
  );
});

test('missing operation inputs expose their exact field without parsing prose', () => {
  assert.throws(
    () => validateOperationInput(operation('jobs.record'), { edit: {}, job_id: JOB_ID }),
    (error: unknown) => error instanceof MissingOperationInputError && error.field === 'preset',
  );
  assert.throws(
    () => validateOperationInput(operation('artifacts.get_url'), { job_id: JOB_ID, kind: 'video' }),
    (error: unknown) => error instanceof MissingOperationInputError && error.field === 'variant',
  );
  assert.throws(
    () => validateOperationInput(operation('artifacts.get_stream_url'), {
      kind: 'video',
      stream_job_id: STREAM_JOB_ID,
      variant: 'streamer-fullframe-nocam',
    }),
    (error: unknown) => error instanceof MissingOperationInputError && error.field === 'clip_id',
  );
});

test('live variant validation uses the matching registry and fails closed', async (t) => {
  await t.test('render and stream variants use their respective live registries', async () => {
    const double = clientDouble((request): JsonValue => {
      if (request.path === '/api/presets') return { presets: [{ name: 'viral-60-clean' }] };
      if (request.path === '/api/stream-variants') return { variants: [{ full_frame: true, name: 'streamer-fullframe-nocam' }] };
      throw new Error(`unexpected request ${request.path}`);
    });

    await validateLiveOperationInput(double.client, operation('renders.get'), {
      job_id: JOB_ID,
      variant: 'viral-60-clean',
    });
    await validateLiveOperationInput(double.client, operation('jobs.record'), {
      job_id: JOB_ID,
      preset: 'viral-60-clean',
    });
    await validateLiveOperationInput(double.client, operation('jobs.generate'), {
      job_id: JOB_ID,
      preset: 'viral-60-clean',
    });
    await validateLiveOperationInput(double.client, operation('streams.get_render'), {
      stream_job_id: STREAM_JOB_ID,
      variant: 'streamer-fullframe-nocam',
    });

    assert.deepEqual(double.state.requests.map((request) => request.path), [
      '/api/presets',
      '/api/presets',
      '/api/presets',
      '/api/stream-variants',
    ]);
  });

  await t.test('unknown and malformed registries reject the input', async () => {
    const unknown = clientDouble(() => ({ presets: [{ name: 'viral-60-clean' }] }));
    await assert.rejects(
      validateLiveOperationInput(unknown.client, operation('renders.get'), {
        job_id: JOB_ID,
        variant: 'invented-variant',
      }),
      /arguments\.variant "invented-variant" is not one of the live render variants: viral-60-clean/,
    );
    await assert.rejects(
      validateLiveOperationInput(unknown.client, operation('jobs.generate'), {
        job_id: JOB_ID,
        preset: 'invented-preset',
      }),
      /arguments\.preset "invented-preset" is not one of the live render variants: viral-60-clean/,
    );

    const malformed = clientDouble(() => ({ presets: [{ label: 'missing name' }] }));
    await assert.rejects(
      validateLiveOperationInput(malformed.client, operation('renders.get'), {
        job_id: JOB_ID,
        variant: 'viral-60-clean',
      }),
      /live render variant registry response is malformed/,
    );

    const empty = clientDouble(() => ({ variants: [] }));
    await assert.rejects(
      validateLiveOperationInput(empty.client, operation('streams.get_render'), {
        stream_job_id: STREAM_JOB_ID,
        variant: 'streamer-fullframe-nocam',
      }),
      /live stream variant registry is empty/,
    );

    const streamMetadataMissing = clientDouble(() => ({ variants: [{ name: 'streamer-fullframe-nocam' }] }));
    await assert.rejects(
      validateLiveOperationInput(streamMetadataMissing.client, operation('streams.get_render'), {
        stream_job_id: STREAM_JOB_ID,
        variant: 'streamer-fullframe-nocam',
      }),
      /live stream variant registry response is malformed/,
    );
  });

  await t.test('nested stream edit-plan variants are validated', async () => {
    const double = clientDouble(() => ({
      variants: [
        { full_frame: false, name: 'streamer-vertical-stack-40-60' },
        { full_frame: true, name: 'future-fullframe-layout' },
      ],
    }));
    await assert.rejects(
      validateLiveOperationInput(double.client, operation('streams.update_edit_plan'), {
        plan: {
          face_crop: { height: 0.2, width: 0.2, x: 0, y: 0 },
          gameplay_crop: { height: 1, width: 1, x: 0, y: 0 },
          variant: 'invented-stream-layout',
        },
        stream_job_id: STREAM_JOB_ID,
      }),
      /arguments\.plan\.variant "invented-stream-layout" is not one of the live stream variants: streamer-vertical-stack-40-60, future-fullframe-layout/,
    );
    await assert.rejects(
      validateLiveOperationInput(double.client, operation('streams.update_edit_plan'), {
        plan: {
          gameplay_crop: { height: 1, width: 1, x: 0, y: 0 },
          variant: 'streamer-vertical-stack-40-60',
        },
        stream_job_id: STREAM_JOB_ID,
      }),
      /arguments\.plan\.face_crop is required when the live stream variant has full_frame=false/,
    );
    await validateLiveOperationInput(double.client, operation('streams.update_edit_plan'), {
      plan: {
        gameplay_crop: { height: 1, width: 1, x: 0, y: 0 },
        variant: 'future-fullframe-layout',
      },
      stream_job_id: STREAM_JOB_ID,
    });
    await validateLiveOperationInput(double.client, operation('streams.update_edit_plan'), {
      plan: {
        face_crop: { height: 0, width: 0, x: 0, y: 0 },
        gameplay_crop: { height: 1, width: 1, x: 0, y: 0 },
        variant: 'future-fullframe-layout',
      },
      stream_job_id: STREAM_JOB_ID,
    });
    assert.deepEqual(double.state.requests.map((request) => request.path), [
      '/api/stream-variants',
      '/api/stream-variants',
      '/api/stream-variants',
      '/api/stream-variants',
    ]);
  });

  await t.test('final and source artifacts do not consult an irrelevant variant registry', async () => {
    const double = clientDouble((request) => {
      throw new Error(`unexpected request ${request.path}`);
    });
    const finalInput: JsonObject = {
      job_id: JOB_ID,
      kind: 'final',
    };
    const sourceInput: JsonObject = {
      kind: 'source',
      stream_job_id: STREAM_JOB_ID,
    };
    validateOperationInput(operation('artifacts.get_url'), finalInput);
    validateOperationInput(operation('artifacts.get_stream_url'), sourceInput);
    await validateLiveOperationInput(double.client, operation('artifacts.get_url'), finalInput);
    await validateLiveOperationInput(double.client, operation('artifacts.get_stream_url'), sourceInput);
    assert.deepEqual(double.state.requests, []);
  });
});

test('dynamic job candidates expose operation-specific state eligibility', async () => {
  const double = clientDouble((request): JsonValue => {
    if (request.path === '/api/jobs?limit=100') {
      return {
        jobs: [
          { id: JOB_ID, status: 'queued' },
          { id: '33333333-3333-4333-8333-333333333333', status: 'recorded' },
        ],
      };
    }
    if (request.path === '/api/stream-jobs?limit=100') {
      return {
        jobs: [
          { id: STREAM_JOB_ID, status: 'uploaded' },
          { id: '44444444-4444-4444-8444-444444444444', status: 'ready' },
        ],
      };
    }
    if (request.path === '/api/stream-variants') return { variants: [] };
    throw new Error(`unexpected request ${request.path}`);
  });

  const composeFields = await discoverDynamicInputs(double.client, operation('jobs.compose'), {});
  const composeCandidates = composeFields[0]?.candidates;
  if (!Array.isArray(composeCandidates)) throw new Error('expected job candidates');
  assert.deepEqual(composeCandidates.filter(isJsonObject).map((candidate) => ({
    eligible: candidate.eligible,
    reason: candidate.ineligible_reason,
    value: candidate.value,
  })), [
    {
      eligible: false,
      reason: 'jobs.compose requires status recorded or composed; current status is queued',
      value: JOB_ID,
    },
    {
      eligible: true,
      reason: undefined,
      value: '33333333-3333-4333-8333-333333333333',
    },
  ]);

  const streamFields = await discoverDynamicInputs(double.client, operation('streams.start_render'), {});
  const streamCandidates = streamFields[0]?.candidates;
  if (!Array.isArray(streamCandidates)) throw new Error('expected stream candidates');
  assert.deepEqual(streamCandidates.filter(isJsonObject).map((candidate) => candidate.eligible), [false, true]);

  const captionFields = await discoverDynamicInputs(double.client, operation('streams.start_caption_candidates'), {});
  const captionCandidates = captionFields[0]?.candidates;
  if (!Array.isArray(captionCandidates)) throw new Error('expected stream caption candidates');
  assert.deepEqual(captionCandidates.filter(isJsonObject).map((candidate) => candidate.eligible), [false, true]);

  const applyFields = await discoverDynamicInputs(double.client, operation('streams.apply_killfeed_analysis'), {});
  const applyCandidates = applyFields[0]?.candidates;
  if (!Array.isArray(applyCandidates)) throw new Error('expected stream killfeed candidates');
  assert.deepEqual(applyCandidates.filter(isJsonObject).map((candidate) => candidate.eligible), [false, true]);
});

test('streams.configure_captions preserves a valid disabled face crop for full-frame renders', async () => {
  const currentPlan: JsonObject = {
    captions: { enabled: false },
    clips: [{ end_seconds: 8, id: 'clip-1', start_seconds: 2 }],
    face_crop: { height: 0, width: 0, x: 0, y: 0 },
    gameplay_crop: { height: 1, width: 1, x: 0, y: 0 },
    schema_version: '1.1',
    variant: 'streamer-fullframe-nocam',
  };
  const double = clientDouble((request, index) => {
    if (index === 0) return currentPlan;
    if (index === 1) return { variants: [{ full_frame: true, name: 'streamer-fullframe-nocam' }] };
    if (index === 2) return request.body ?? null;
    throw new Error('unexpected request');
  });

  const result = await operation('streams.configure_captions').run(double.client, {
    enabled: true,
    language: 'es',
    stream_job_id: 'stream-123',
  });

  assert.ok(isJsonObject(result));
  assert.deepEqual(result.face_crop, currentPlan.face_crop);
  assert.deepEqual(result.captions, { enabled: true, language: 'es' });
  assert.deepEqual(double.state.requests.map((request) => [request.method ?? 'GET', request.path]), [
    ['GET', '/api/stream-jobs/stream-123/edit-plan'],
    ['GET', '/api/stream-variants'],
    ['PUT', '/api/stream-jobs/stream-123/edit-plan'],
  ]);
});

function planWithClipEdit(edit: JsonValue): JsonObject {
  return {
    clips: [{ edit, end_seconds: 10, id: 'clip-1', start_seconds: 0 }],
    face_crop: { height: 0.3, width: 0.25, x: 0, y: 0 },
    gameplay_crop: { height: 1, width: 1, x: 0, y: 0 },
    variant: 'streamer-vertical-stack-40-60',
  };
}

test('stream edit plan accepts and validates per-clip edit options', () => {
  const editPlanOperation = operation('streams.update_edit_plan');
  validateOperationInput(editPlanOperation, {
    plan: planWithClipEdit({
      fade_in_seconds: 0.5,
      fade_out_seconds: 1,
      source_volume: 0,
      speed: 2,
      text_overlays: [{ end_seconds: 3, font_size: 72, position_y: 0.3, start_seconds: 1, text: 'NICE SHOT' }],
    }),
    stream_job_id: STREAM_JOB_ID,
  });

  assert.throws(
    () => validateOperationInput(editPlanOperation, { plan: planWithClipEdit({ speed: 5 }), stream_job_id: STREAM_JOB_ID }),
    /speed/,
  );
  assert.throws(
    () => validateOperationInput(editPlanOperation, {
      plan: planWithClipEdit({ text_overlays: [{ end_seconds: 1, position_y: 0.5, start_seconds: 3, text: 'hi' }] }),
      stream_job_id: STREAM_JOB_ID,
    }),
    /text_overlays\[0\].end_seconds must be greater than start_seconds/,
  );
  assert.throws(
    () => validateOperationInput(editPlanOperation, {
      plan: planWithClipEdit({ text_overlays: [{ position_y: 0.5, start_seconds: 12, text: 'hi' }] }),
      stream_job_id: STREAM_JOB_ID,
    }),
    /text_overlays\[0\].start_seconds must be inside the clip/,
  );
  assert.throws(
    () => validateOperationInput(editPlanOperation, {
      // 10s source at 2.5x plays back in 4s; 4.5s of fades cannot fit.
      plan: planWithClipEdit({ fade_in_seconds: 2.5, fade_out_seconds: 2, speed: 2.5 }),
      stream_job_id: STREAM_JOB_ID,
    }),
    /fades must fit within the clip's output duration/,
  );
  assert.throws(
    () => validateOperationInput(editPlanOperation, {
      plan: planWithClipEdit({
        text_overlays: [
          { position_y: 0.1, text: '1' },
          { position_y: 0.2, text: '2' },
          { position_y: 0.3, text: '3' },
          { position_y: 0.4, text: '4' },
          { position_y: 0.5, text: '5' },
        ],
      }),
      stream_job_id: STREAM_JOB_ID,
    }),
    /text_overlays has too many items/,
  );
});

test('streams.edit_clip merges the edit into the saved plan and preserves everything else', async () => {
  const currentPlan: JsonObject = {
    captions: { enabled: true, language: 'es' },
    clips: [
      { end_seconds: 8, id: 'clip-1', start_seconds: 2 },
      { edit: { speed: 0.5 }, end_seconds: 30, id: 'clip-2', start_seconds: 20 },
    ],
    face_crop: { height: 0.3, width: 0.25, x: 0, y: 0 },
    gameplay_crop: { height: 1, width: 1, x: 0, y: 0 },
    schema_version: '1.0',
    variant: 'streamer-vertical-stack-40-60',
  };
  const double = clientDouble((request, index) => {
    if (index === 0) return currentPlan;
    if (index === 1) return { variants: [{ full_frame: false, name: 'streamer-vertical-stack-40-60' }] };
    if (index === 2) return request.body ?? null;
    throw new Error('unexpected request');
  });

  const result = objectResult(await operation('streams.edit_clip').run(double.client, {
    clip_id: 'clip-2',
    source_volume: 0.5,
    speed: 2,
    stream_job_id: 'stream-123',
    text_overlays: [{ position_y: 0.3, text: 'NICE' }],
  }));

  const clips = result.clips;
  if (!Array.isArray(clips)) throw new Error('expected clips array');
  assert.deepEqual(clips[0], { end_seconds: 8, id: 'clip-1', start_seconds: 2 });
  assert.deepEqual(clips[1], {
    edit: { source_volume: 0.5, speed: 2, text_overlays: [{ position_y: 0.3, text: 'NICE' }] },
    end_seconds: 30,
    id: 'clip-2',
    start_seconds: 20,
  });
  assert.deepEqual(result.captions, currentPlan.captions);
  assert.deepEqual(double.state.requests.map((request) => [request.method ?? 'GET', request.path]), [
    ['GET', '/api/stream-jobs/stream-123/edit-plan'],
    ['GET', '/api/stream-variants'],
    ['PUT', '/api/stream-jobs/stream-123/edit-plan'],
  ]);
});

test('streams.edit_clip resetting every option to its default drops the edit object', async () => {
  const currentPlan: JsonObject = {
    clips: [{ edit: { speed: 2 }, end_seconds: 10, id: 'clip-1', start_seconds: 0 }],
    face_crop: { height: 0.3, width: 0.25, x: 0, y: 0 },
    gameplay_crop: { height: 1, width: 1, x: 0, y: 0 },
    schema_version: '1.0',
    variant: 'streamer-vertical-stack-40-60',
  };
  const double = clientDouble((request, index) => {
    if (index === 0) return currentPlan;
    if (index === 1) return { variants: [{ full_frame: false, name: 'streamer-vertical-stack-40-60' }] };
    if (index === 2) return request.body ?? null;
    throw new Error('unexpected request');
  });

  const result = objectResult(await operation('streams.edit_clip').run(double.client, {
    clip_id: 'clip-1',
    speed: 1,
    stream_job_id: 'stream-123',
  }));

  const clips = result.clips;
  if (!Array.isArray(clips)) throw new Error('expected clips array');
  assert.deepEqual(clips[0], { end_seconds: 10, id: 'clip-1', start_seconds: 0 });
});

test('streams.edit_clip rejects an unknown clip id and lists the valid ones', async () => {
  const currentPlan: JsonObject = {
    clips: [
      { end_seconds: 8, id: 'clip-1', start_seconds: 2 },
      { end_seconds: 30, id: 'clip-2', start_seconds: 20 },
    ],
    face_crop: { height: 0.3, width: 0.25, x: 0, y: 0 },
    gameplay_crop: { height: 1, width: 1, x: 0, y: 0 },
    schema_version: '1.0',
    variant: 'streamer-vertical-stack-40-60',
  };
  const double = clientDouble((_request, index) => {
    if (index === 0) return currentPlan;
    throw new Error('unexpected request');
  });

  await assert.rejects(
    operation('streams.edit_clip').run(double.client, {
      clip_id: 'clip-9',
      speed: 2,
      stream_job_id: 'stream-123',
    }),
    /arguments\.clip_id "clip-9" is not one of the plan's clips: clip-1, clip-2/,
  );
});

test('streams.edit_clip surfaces unknown stored edit fields instead of silently dropping them', async () => {
  const currentPlan: JsonObject = {
    clips: [{ edit: { speed: 2, zoom: true }, end_seconds: 10, id: 'clip-1', start_seconds: 0 }],
    face_crop: { height: 0.3, width: 0.25, x: 0, y: 0 },
    gameplay_crop: { height: 1, width: 1, x: 0, y: 0 },
    schema_version: '1.0',
    variant: 'streamer-vertical-stack-40-60',
  };
  const double = clientDouble((_request, index) => {
    if (index === 0) return currentPlan;
    throw new Error('unexpected request');
  });

  await assert.rejects(
    operation('streams.edit_clip').run(double.client, {
      clip_id: 'clip-1',
      speed: 0.5,
      stream_job_id: 'stream-123',
    }),
    /zoom is not allowed/,
  );
  assert.equal(double.state.requests.length, 1, 'must fail loudly before the PUT');
});

test('string length limits count code points, matching the Go rune validation', () => {
  const schema: JsonObject = { maxLength: 3, minLength: 2, type: 'string' };
  validateJsonSchema(schema, '🎉🎉🎉', 'field');
  assert.throws(() => validateJsonSchema(schema, 'aaaa', 'field'), /too long/);
  assert.throws(() => validateJsonSchema(schema, '🎉', 'field'), /too short/);
});
