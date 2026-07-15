import test from 'node:test';
import assert from 'node:assert/strict';
import { RealStreamsApiClient } from './streams.ts';

test('stream upload uses the orchestrator multipart field names', async () => {
  const originalFetch = globalThis.fetch;
  let requestBody: FormData | undefined;
  globalThis.fetch = async (_input, init): Promise<Response> => {
    requestBody = init?.body instanceof FormData ? init.body : undefined;
    return Response.json({
      id: '11111111-1111-4111-8111-111111111111',
      status: 'uploaded',
      created_at: '2026-07-15T00:00:00Z',
    });
  };

  try {
    const file = new File([new Uint8Array([1, 2, 3])], 'clip.mp4', { type: 'video/mp4' });
    await new RealStreamsApiClient().createFromFile(file, 'Clutch');

    assert.ok(requestBody);
    const video = requestBody.get('video');
    assert.ok(video instanceof File);
    assert.equal(video.name, 'clip.mp4');
    assert.equal(requestBody.get('config'), JSON.stringify({ title: 'Clutch' }));
    assert.equal(requestBody.has('file'), false);
    assert.equal(requestBody.has('title'), false);
  } finally {
    globalThis.fetch = originalFetch;
  }
});
