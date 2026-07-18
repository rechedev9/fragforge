import test from 'node:test';
import assert from 'node:assert/strict';
import { RealStreamsApiClient } from './streams.ts';

test('stream caption client uses generation, polling, and explicit review routes', async () => {
  const originalFetch = globalThis.fetch;
  const requests: { url: string; method: string; body?: string }[] = [];
  globalThis.fetch = async (input, init): Promise<Response> => {
    requests.push({
      url: String(input),
      method: init?.method ?? 'GET',
      body: typeof init?.body === 'string' ? init.body : undefined,
    });
    if (String(input).endsWith('/review')) {
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
    await client.startCaptionGeneration('job-1');
    await client.getCaptionGenerationState('job-1');
    await client.reviewCaptionCandidates('job-1', '22222222-2222-4222-8222-222222222222', [{
      clip_id: 'clip-1',
      words: [{ word: 'hola', start_seconds: 0, end_seconds: 0.4 }],
    }]);

    assert.deepEqual(requests, [
      { url: '/api/streams/job-1/captions', method: 'POST', body: undefined },
      { url: '/api/streams/job-1/captions', method: 'GET', body: undefined },
      {
        url: '/api/streams/job-1/captions/review',
        method: 'POST',
        body: JSON.stringify({
          generation_id: '22222222-2222-4222-8222-222222222222',
          clips: [{
            clip_id: 'clip-1',
            words: [{ word: 'hola', start_seconds: 0, end_seconds: 0.4 }],
          }],
        }),
      },
    ]);
  } finally {
    globalThis.fetch = originalFetch;
  }
});
