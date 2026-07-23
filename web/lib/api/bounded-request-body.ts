import { localAPIRequestError } from './local-request-guard.ts';

export const MAX_CONTROL_BODY_BYTES = 1 * 1024 * 1024;

export interface RequestBodySource {
  readonly headers: Headers;
  readonly body: ReadableStream<Uint8Array> | null;
}

export type BodyReadFailure = {
  ok: false;
  error: string;
  status: 400 | 403 | 413;
};

export type PreparedUploadBody = {
  ok: true;
  body: ReadableStream<Uint8Array>;
  contentLength?: string;
  exceeded(): boolean;
};

export type BoundedText = BodyReadFailure | { ok: true; text: string };

/**
 * Validates a local upload before touching its body, then returns a streaming
 * byte counter. A missing Content-Length is allowed for browser multipart
 * uploads, but the counter still terminates the stream at maxBytes.
 */
export async function prepareLocalUploadBody(
  request: RequestBodySource,
  maxBytes: number,
): Promise<BodyReadFailure | PreparedUploadBody> {
  const localError = await localAPIRequestError(request.headers, 'POST');
  if (localError !== undefined) return { ok: false, error: localError, status: 403 };

  const length = validateContentLength(request.headers, maxBytes);
  if (!length.ok) return length;

  const body = request.body;
  if (body === null) return { ok: false, error: 'missing request body', status: 400 };

  const bounded = boundStream(body, maxBytes);
  return {
    ok: true,
    body: bounded.body,
    ...(length.contentLength === undefined ? {} : { contentLength: length.contentLength }),
    exceeded: bounded.exceeded,
  };
}

/** Reads a small control body without request.text()/json()'s unbounded buffer. */
export async function readBoundedText(
  request: RequestBodySource,
  maxBytes = MAX_CONTROL_BODY_BYTES,
): Promise<BoundedText> {
  const length = validateContentLength(request.headers, maxBytes);
  if (!length.ok) return length;
  if (request.body === null) return { ok: true, text: '' };

  const bounded = boundStream(request.body, maxBytes);
  try {
    return { ok: true, text: await new Response(bounded.body).text() };
  } catch {
    return bounded.exceeded()
      ? { ok: false, error: 'request body too large', status: 413 }
      : { ok: false, error: 'invalid request body', status: 400 };
  }
}

type ContentLengthResult =
  | BodyReadFailure
  | { ok: true; contentLength?: string };

function validateContentLength(headers: Headers, maxBytes: number): ContentLengthResult {
  const value = headers.get('content-length');
  if (value === null) return { ok: true };
  if (!/^\d+$/.test(value)) return { ok: false, error: 'invalid content-length', status: 400 };

  const bytes = Number(value);
  if (!Number.isSafeInteger(bytes)) return { ok: false, error: 'invalid content-length', status: 400 };
  if (bytes > maxBytes) return { ok: false, error: 'request body too large', status: 413 };
  return { ok: true, contentLength: value };
}

function boundStream(
  body: ReadableStream<Uint8Array>,
  maxBytes: number,
): { body: ReadableStream<Uint8Array>; exceeded(): boolean } {
  let received = 0;
  let overLimit = false;
  const bounded = body.pipeThrough(new TransformStream<Uint8Array, Uint8Array>({
    transform(chunk, controller): void {
      received += chunk.byteLength;
      if (received > maxBytes) {
        overLimit = true;
        controller.error(new Error('request body exceeds configured limit'));
        return;
      }
      controller.enqueue(chunk);
    },
  }));
  return { body: bounded, exceeded: () => overLimit };
}
