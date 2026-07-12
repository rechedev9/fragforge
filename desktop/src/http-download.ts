import { createHash } from 'node:crypto';
import * as fs from 'node:fs';
import * as http from 'node:http';
import * as https from 'node:https';

export interface DownloadOptions {
  signal?: AbortSignal;
  onProgress?: (received: number, total: number | undefined) => void;
}

interface FetchStreamOptions {
  redirectsLeft?: number;
  signal?: AbortSignal;
}

// A stalled socket should not consume the caller's larger provisioning budget.
const DOWNLOAD_SOCKET_IDLE_TIMEOUT_MS = 60_000;

/**
 * Downloads a URL through a temporary sibling file, returning its SHA-256.
 * The destination is renamed into place only after the complete response has
 * arrived, so interrupted downloads never look like usable runtime assets.
 */
export async function downloadFile(
  url: string,
  destination: string,
  { signal, onProgress }: DownloadOptions = {},
): Promise<string> {
  const temporary = `${destination}.tmp`;
  fs.rmSync(temporary, { force: true });

  try {
    const response = await fetchStream(url, { signal });
    const total = Number(response.headers['content-length']) || undefined;
    const hash = createHash('sha256');
    let received = 0;
    await new Promise<void>((resolve, reject) => {
      const output = fs.createWriteStream(temporary);
      let settled = false;
      const onAbort = (): void => fail(new Error('download aborted'));
      const finish = (err?: Error): void => {
        if (settled) return;
        settled = true;
        signal?.removeEventListener('abort', onAbort);
        if (err) {
          reject(err);
        } else {
          resolve();
        }
      };
      const fail = (err: Error): void => {
        response.destroy();
        output.destroy();
        finish(err);
      };

      response.on('data', (chunk: Buffer) => {
        hash.update(chunk);
        received += chunk.length;
        onProgress?.(received, total);
      });
      response.pipe(output);
      response.on('error', fail);
      output.on('error', fail);
      output.on('finish', () => finish());

      if (signal) {
        if (signal.aborted) {
          onAbort();
        } else {
          signal.addEventListener('abort', onAbort, { once: true });
        }
      }
    });

    fs.renameSync(temporary, destination);
    return hash.digest('hex');
  } catch (err) {
    fs.rmSync(temporary, { force: true });
    throw err;
  }
}

/** Opens an HTTP(S) response and follows a small, bounded redirect chain. */
function fetchStream(
  url: string,
  { redirectsLeft = 5, signal }: FetchStreamOptions = {},
): Promise<http.IncomingMessage> {
  return new Promise((resolve, reject) => {
    const handleResponse = (response: http.IncomingMessage): void => {
      const code = response.statusCode;
      if (code !== undefined && code >= 300 && code < 400 && response.headers.location && redirectsLeft > 0) {
        response.resume();
        resolve(fetchStream(new URL(response.headers.location, url).toString(), {
          redirectsLeft: redirectsLeft - 1,
          signal,
        }));
        return;
      }
      if (code !== 200) {
        response.resume();
        reject(new Error(`GET ${url}: HTTP ${code}`));
        return;
      }
      resolve(response);
    };

    const request = url.startsWith('https:')
      ? https.get(url, { signal }, handleResponse)
      : http.get(url, { signal }, handleResponse);
    request.on('error', reject);
    request.setTimeout(DOWNLOAD_SOCKET_IDLE_TIMEOUT_MS, () => {
      request.destroy(new Error(`GET ${url}: timed out`));
    });
  });
}
