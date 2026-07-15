import test from 'node:test';
import assert from 'node:assert/strict';
import {
  prepareLocalUploadBody,
  readBoundedText,
  type RequestBodySource,
} from './bounded-request-body.ts';

const LOCAL_HEADERS = {
  host: '127.0.0.1:3000',
  origin: 'http://127.0.0.1:3000',
  'sec-fetch-site': 'same-origin',
};

test('rejects an oversized Content-Length before reading the upload body', () => {
  let bodyRead = false;
  const request: RequestBodySource = {
    headers: new Headers({ ...LOCAL_HEADERS, 'content-length': '6' }),
    get body(): ReadableStream<Uint8Array> {
      bodyRead = true;
      return byteStream([new Uint8Array(6)]);
    },
  };

  const result = prepareLocalUploadBody(request, 5);

  assert.deepEqual(result, { ok: false, error: 'request body too large', status: 413 });
  assert.equal(bodyRead, false);
});

test('rejects a hostile Host before reading Content-Length or body', () => {
  let bodyRead = false;
  const request: RequestBodySource = {
    headers: new Headers({ host: 'attacker.example:3000', 'content-length': '1' }),
    get body(): ReadableStream<Uint8Array> {
      bodyRead = true;
      return byteStream([new Uint8Array(1)]);
    },
  };

  const result = prepareLocalUploadBody(request, 5);

  assert.deepEqual(result, { ok: false, error: 'local API host rejected', status: 403 });
  assert.equal(bodyRead, false);
});

test('preserves a valid Content-Length for raw upstream streaming', () => {
  const result = prepareLocalUploadBody({
    headers: new Headers({ ...LOCAL_HEADERS, 'content-length': '5' }),
    body: byteStream([new Uint8Array(5)]),
  }, 5);

  assert.equal(result.ok, true);
  if (!result.ok) return;
  assert.equal(result.contentLength, '5');
});

test('terminates a chunked upload when streamed bytes exceed the limit', async () => {
  const result = prepareLocalUploadBody({
    headers: new Headers(LOCAL_HEADERS),
    body: byteStream([new Uint8Array(3), new Uint8Array(3)]),
  }, 5);

  assert.equal(result.ok, true);
  if (!result.ok) return;
  assert.equal(result.contentLength, undefined);

  const reader = result.body.getReader();
  const first = await reader.read();
  assert.equal(first.value?.byteLength, 3);
  await assert.rejects(reader.read(), /exceeds configured limit/);
  assert.equal(result.exceeded(), true);
});

test('bounded text rejects a chunked control body without buffering past its cap', async () => {
  const result = await readBoundedText({
    headers: new Headers(),
    body: byteStream([new TextEncoder().encode('1234'), new TextEncoder().encode('56')]),
  }, 5);

  assert.deepEqual(result, { ok: false, error: 'request body too large', status: 413 });
});

function byteStream(chunks: Uint8Array[]): ReadableStream<Uint8Array> {
  return new ReadableStream<Uint8Array>({
    start(controller): void {
      for (const chunk of chunks) controller.enqueue(chunk);
      controller.close();
    },
  });
}
