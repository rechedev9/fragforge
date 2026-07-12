import * as http from 'node:http';

export interface HttpWaitOptions {
  requestTimeoutMs?: number;
  pollIntervalMs?: number;
}

const DEFAULT_REQUEST_TIMEOUT_MS = 2000;
const DEFAULT_POLL_INTERVAL_MS = 400;

/** Polls an HTTP URL until it answers 2xx/3xx, times out, or is cancelled. */
export function waitForHttp(
  url: string,
  timeoutMs: number,
  signal: AbortSignal,
  options: HttpWaitOptions = {},
): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  const requestTimeoutMs = options.requestTimeoutMs ?? DEFAULT_REQUEST_TIMEOUT_MS;
  const pollIntervalMs = options.pollIntervalMs ?? DEFAULT_POLL_INTERVAL_MS;

  return new Promise((resolve, reject) => {
    let settled = false;
    let request: http.ClientRequest | null = null;
    let retryTimer: NodeJS.Timeout | null = null;
    const finish = (err?: Error): void => {
      if (settled) return;
      settled = true;
      if (retryTimer) clearTimeout(retryTimer);
      signal.removeEventListener('abort', abort);
      if (err) {
        reject(err);
      } else {
        resolve();
      }
    };
    const abort = (): void => {
      const activeRequest = request;
      finish(new Error(`cancelled waiting for ${url}`));
      activeRequest?.destroy();
    };
    const retry = (): void => {
      if (settled) return;
      request = null;
      if (Date.now() > deadline) {
        finish(new Error(`timed out waiting for ${url}`));
        return;
      }
      retryTimer = setTimeout(attempt, pollIntervalMs);
    };
    const attempt = (): void => {
      if (settled) return;
      const nextRequest = http.get(url, (response) => {
        response.resume();
        request = null;
        if (response.statusCode && response.statusCode < 400) {
          finish();
          return;
        }
        retry();
      });
      request = nextRequest;
      nextRequest.on('error', retry);
      nextRequest.setTimeout(requestTimeoutMs, () => nextRequest.destroy());
    };

    signal.addEventListener('abort', abort, { once: true });
    if (signal.aborted) {
      abort();
      return;
    }
    attempt();
  });
}
