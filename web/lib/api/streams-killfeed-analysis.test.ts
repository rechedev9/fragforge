import test from 'node:test';
import assert from 'node:assert/strict';
import { RealStreamsApiClient } from './streams.ts';

test('stream killfeed client starts, polls, and atomically applies one generation', async () => {
  const originalFetch = globalThis.fetch;
  const requests: { url: string; method: string; body?: string }[] = [];
  globalThis.fetch = async (input, init): Promise<Response> => {
    requests.push({
      url: String(input),
      method: init?.method ?? 'GET',
      body: typeof init?.body === 'string' ? init.body : undefined,
    });
    if (String(input).endsWith('/apply')) {
      return Response.json({
        schema_version: '1.1',
        variant: 'streamer-fullframe-nocam',
        clips: [],
      });
    }
    return Response.json({
      job_id: '11111111-1111-4111-8111-111111111111',
      generation_id: '22222222-2222-4222-8222-222222222222',
      status: 'queued',
      clips: [],
      updated_at: '2026-07-18T00:00:00Z',
    });
  };

  try {
    const client = new RealStreamsApiClient();
    await client.startKillfeedAnalysis('job-1');
    await client.getKillfeedAnalysisState('job-1');
    await client.applyKillfeedAnalysis('job-1', '22222222-2222-4222-8222-222222222222');

    assert.deepEqual(requests, [
      { url: '/api/streams/job-1/killfeed', method: 'POST', body: undefined },
      { url: '/api/streams/job-1/killfeed', method: 'GET', body: undefined },
      {
        url: '/api/streams/job-1/killfeed/apply',
        method: 'POST',
        body: JSON.stringify({ generation_id: '22222222-2222-4222-8222-222222222222' }),
      },
    ]);
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test('stream killfeed client sends immutable event identity for an exact read', async () => {
  const originalFetch = globalThis.fetch;
  let request: { url: string; method: string; body?: string } | undefined;
  globalThis.fetch = async (input, init): Promise<Response> => {
    request = {
      url: String(input),
      method: init?.method ?? 'GET',
      body: typeof init?.body === 'string' ? init.body : undefined,
    };
    return Response.json({ kills: [], cue_seconds: 1001 / 30000, aligned: true, events: [] });
  };

  try {
    const client = new RealStreamsApiClient();
    const cue = 1001 / 30000;
    await client.readKillfeed('job-1', 'clip-1', cue, {
      eventId: 'event-1001',
      generationId: '22222222-2222-4222-8222-222222222222',
    });
    assert.deepEqual(request, {
      url: '/api/streams/job-1/killfeed-read',
      method: 'POST',
      body: JSON.stringify({
        clip_id: 'clip-1',
        cue_seconds: cue,
        event_id: 'event-1001',
        generation_id: '22222222-2222-4222-8222-222222222222',
      }),
    });
  } finally {
    globalThis.fetch = originalFetch;
  }
});
