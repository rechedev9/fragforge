import { openAsBlob } from 'node:fs';
import { readFile, stat } from 'node:fs/promises';
import { createHmac, randomBytes, timingSafeEqual } from 'node:crypto';
import { Buffer } from 'node:buffer';
import * as path from 'node:path';
import { isJsonObject, isJsonValue, parseJsonObject, type JsonObject, type JsonValue } from './json.ts';

const DEFAULT_ORCHESTRATOR_URL = 'http://127.0.0.1:8080';
const DEFAULT_REQUEST_TIMEOUT_MS = 30_000;
const DEFAULT_UPLOAD_TIMEOUT_MS = 10 * 60_000;
const MAX_HEALTH_RESPONSE_BYTES = 64 << 10;
const MAX_JSON_RESPONSE_BYTES = 10 << 20;
const MAX_TEXT_RESPONSE_BYTES = 2 << 20;
const MAX_DEMO_BYTES = 500 << 20;
const MAX_STREAM_VIDEO_BYTES = 8 * 2 ** 30;
const MIN_SECRET_LENGTH = 8;
const LOOPBACK_HOSTS = new Set(['127.0.0.1', '::1', 'localhost']);

export interface OrchestratorClientOptions {
  baseUrl?: string;
  mutationToken?: string;
  portsFile?: string;
  requestTimeoutMs?: number;
}

export interface OrchestratorRequest {
  body?: JsonValue;
  method?: 'DELETE' | 'GET' | 'POST' | 'PUT';
  path: string;
  signal?: AbortSignal;
}

interface ResolvedOrchestrator {
  baseUrl: string;
  discovered: boolean;
  verificationSecret?: string;
}

interface PortsFileEndpoint {
  baseUrl: string;
  discoverySecret?: string;
}

export class OrchestratorHttpError extends Error {
  readonly status: number;

  constructor(status: number, message: string) {
    super(message);
    this.name = 'OrchestratorHttpError';
    this.status = status;
  }
}

class ArtifactReadAuthError extends Error {
  constructor() {
    super('artifact URLs are unavailable while orchestrator read authentication is enabled; use the loopback-only Studio configuration');
    this.name = 'ArtifactReadAuthError';
  }
}

export class OrchestratorClient {
  readonly #explicitBaseUrl: string | undefined;
  readonly #mutationToken: string | undefined;
  readonly #portsFile: string | undefined;
  readonly #requestTimeoutMs: number;

  constructor(options: OrchestratorClientOptions = {}) {
    this.#explicitBaseUrl = options.baseUrl;
    if (options.mutationToken !== undefined && options.mutationToken !== ''
      && options.mutationToken.length < MIN_SECRET_LENGTH) {
      throw new Error(`mutationToken must contain at least ${MIN_SECRET_LENGTH} characters`);
    }
    this.#mutationToken = options.mutationToken;
    this.#portsFile = options.portsFile;
    this.#requestTimeoutMs = options.requestTimeoutMs ?? DEFAULT_REQUEST_TIMEOUT_MS;
  }

  async baseUrl(): Promise<string> {
    return (await this.#resolveOrchestrator()).baseUrl;
  }

  async #resolveOrchestrator(): Promise<ResolvedOrchestrator> {
    const portsEndpoint = this.#explicitBaseUrl === undefined
      ? await this.#endpointFromPortsFile()
      : undefined;
    const candidate = this.#explicitBaseUrl ?? portsEndpoint?.baseUrl ?? DEFAULT_ORCHESTRATOR_URL;
    const url = new URL(candidate);
    const hostname = url.hostname.toLowerCase().replace(/^\[(.*)\]$/, '$1');
    if (url.protocol !== 'http:' || !LOOPBACK_HOSTS.has(hostname)) {
      throw new Error('FragForge orchestrator URL must use HTTP on loopback');
    }
    if (url.username !== '' || url.password !== '' || url.search !== '' || url.hash !== '') {
      throw new Error('FragForge orchestrator URL must not contain credentials, query, or fragment');
    }
    url.pathname = url.pathname.replace(/\/$/, '');
    const baseUrl = url.toString().replace(/\/$/, '');
    if (this.#explicitBaseUrl !== undefined) return { baseUrl, discovered: false };

    const verificationSecret = portsEndpoint?.discoverySecret ?? this.#mutationToken;
    if (verificationSecret === undefined || verificationSecret === '') {
      throw new Error('automatic FragForge discovery requires discovery_secret in ports.json or FRAGFORGE_MUTATION_TOKEN; set FRAGFORGE_ORCHESTRATOR_URL only for an explicitly trusted loopback service');
    }
    return { baseUrl, discovered: true, verificationSecret };
  }

  async request(request: OrchestratorRequest): Promise<JsonValue> {
    const orchestrator = await this.#resolveOrchestrator();
    const url = new URL(request.path, `${orchestrator.baseUrl}/`);
    if (url.origin !== new URL(orchestrator.baseUrl).origin) {
      throw new Error('orchestrator request path escaped the loopback origin');
    }
    await this.#ensureVerified(orchestrator, request.signal);
    const headers = new Headers({ Accept: 'application/json' });
    if (this.#mutationToken !== undefined && this.#mutationToken !== '') {
      headers.set('X-FragForge-Token', this.#mutationToken);
    }
    let body: string | undefined;
    if (request.body !== undefined) {
      headers.set('Content-Type', 'application/json');
      body = JSON.stringify(request.body);
    }
    return this.#fetchJson(url, {
      body,
      headers,
      method: request.method ?? 'GET',
    }, this.#requestTimeoutMs, request.signal);
  }

  async requestText(apiPath: string, signal?: AbortSignal): Promise<string> {
    const orchestrator = await this.#resolveOrchestrator();
    const url = new URL(apiPath, `${orchestrator.baseUrl}/`);
    if (url.origin !== new URL(orchestrator.baseUrl).origin) {
      throw new Error('orchestrator request path escaped the loopback origin');
    }
    await this.#ensureVerified(orchestrator, signal);
    const headers = new Headers({ Accept: 'text/plain' });
    if (this.#mutationToken !== undefined && this.#mutationToken !== '') {
      headers.set('X-FragForge-Token', this.#mutationToken);
    }
    return this.#fetchText(url, { headers, method: 'GET' }, this.#requestTimeoutMs, signal);
  }

  async uploadDemo(filePath: string, config: JsonObject, signal?: AbortSignal): Promise<JsonValue> {
    await validateUpload(filePath, new Set(['.dem']), 'demo', MAX_DEMO_BYTES);
    const form = new FormData();
    form.append('demo', await openAsBlob(filePath), path.basename(filePath));
    form.append('config', JSON.stringify(config));
    return this.#upload('/api/jobs', form, signal);
  }

  async uploadStreamVideo(filePath: string, config: JsonObject, signal?: AbortSignal): Promise<JsonValue> {
    await validateUpload(filePath, new Set(['.avi', '.flv', '.m2ts', '.m4v', '.mkv', '.mov', '.mp4', '.mpeg', '.mpg', '.ts', '.webm']), 'stream video', MAX_STREAM_VIDEO_BYTES);
    const form = new FormData();
    form.append('video', await openAsBlob(filePath), path.basename(filePath));
    form.append('config', JSON.stringify(config));
    return this.#upload('/api/stream-jobs', form, signal);
  }

  async artifactUrl(apiPath: string, signal?: AbortSignal): Promise<string> {
    const orchestrator = await this.#resolveOrchestrator();
    const url = new URL(apiPath, `${orchestrator.baseUrl}/`);
    if (url.origin !== new URL(orchestrator.baseUrl).origin) throw new Error('artifact path escaped the loopback origin');
    await this.#ensureVerified(orchestrator, signal);
    const headers = new Headers({ Accept: 'application/json' });
    if (this.#mutationToken !== undefined && this.#mutationToken !== '') {
      headers.set('X-FragForge-Token', this.#mutationToken);
    }
    let capabilities: JsonValue;
    try {
      capabilities = await this.#fetchJson(
        new URL('/api/capabilities', `${orchestrator.baseUrl}/`),
        { headers, method: 'GET' },
        this.#requestTimeoutMs,
        signal,
      );
    } catch (error: unknown) {
      if (error instanceof OrchestratorHttpError && error.status === 401) throw artifactReadAuthError();
      throw error;
    }
    const auth = isJsonObject(capabilities) && isJsonObject(capabilities.auth) ? capabilities.auth : undefined;
    if (auth?.read_requires_token === true) {
      throw artifactReadAuthError();
    }
    await this.#assertArtifactAvailable(url, headers, signal);
    return url.toString();
  }

  async #assertArtifactAvailable(url: URL, headers: Headers, signal?: AbortSignal): Promise<void> {
    const controller = new AbortController();
    const abortFromCaller = (): void => controller.abort();
    if (signal?.aborted) controller.abort();
    else signal?.addEventListener('abort', abortFromCaller, { once: true });
    const timeout = setTimeout(() => controller.abort(), this.#requestTimeoutMs);
    try {
      const probeHeaders = new Headers(headers);
      probeHeaders.set('Range', 'bytes=0-0');
      const response = await fetch(url, {
        headers: probeHeaders,
        method: 'GET',
        redirect: 'error',
        signal: controller.signal,
      });
      await response.body?.cancel();
      if (response.status === 401) throw artifactReadAuthError();
      if (!response.ok) {
        throw new OrchestratorHttpError(
          response.status,
          `FragForge artifact is unavailable (HTTP ${response.status})`,
        );
      }
    } catch (error: unknown) {
      if (error instanceof OrchestratorHttpError || error instanceof ArtifactReadAuthError) throw error;
      if (signal?.aborted) throw new Error('FragForge operation was cancelled');
      if (controller.signal.aborted) {
        throw new Error(`FragForge orchestrator timed out after ${this.#requestTimeoutMs}ms`);
      }
      const message = error instanceof Error ? error.message : String(error);
      throw new Error(`FragForge Studio is offline or unreachable at ${url.origin}: ${redactText(message, this.#mutationToken)}`);
    } finally {
      clearTimeout(timeout);
      signal?.removeEventListener('abort', abortFromCaller);
    }
  }

  async #upload(apiPath: string, form: FormData, signal?: AbortSignal): Promise<JsonValue> {
    const orchestrator = await this.#resolveOrchestrator();
    await this.#ensureVerified(orchestrator, signal);
    const headers = new Headers({ Accept: 'application/json' });
    if (this.#mutationToken !== undefined && this.#mutationToken !== '') {
      headers.set('X-FragForge-Token', this.#mutationToken);
    }
    return this.#fetchJson(new URL(apiPath, `${orchestrator.baseUrl}/`), {
      body: form,
      headers,
      method: 'POST',
    }, Math.max(this.#requestTimeoutMs, DEFAULT_UPLOAD_TIMEOUT_MS), signal);
  }

  async #ensureVerified(orchestrator: ResolvedOrchestrator, signal?: AbortSignal): Promise<void> {
    // Explicit URLs are an intentional trust decision. Automatically discovered
    // ports must prove they are the currently running FragForge instance before
    // this process sends a token or local media.
    if (!orchestrator.discovered) return;
    const verificationSecret = orchestrator.verificationSecret;
    if (verificationSecret === undefined || verificationSecret === '') {
      throw new Error('automatic FragForge discovery has no verification secret');
    }
    await this.#verifyDiscoveredOrigin(orchestrator.baseUrl, verificationSecret, signal);
  }

  async #verifyDiscoveredOrigin(baseUrl: string, verificationSecret: string, signal?: AbortSignal): Promise<void> {
    const challenge = randomBytes(32).toString('hex');
    const url = new URL(`/healthz?challenge=${challenge}`, `${baseUrl}/`);
    const controller = new AbortController();
    const abortFromCaller = (): void => controller.abort();
    if (signal?.aborted) controller.abort();
    else signal?.addEventListener('abort', abortFromCaller, { once: true });
    const timeout = setTimeout(() => controller.abort(), this.#requestTimeoutMs);
    try {
      const response = await fetch(url, { headers: { Accept: 'application/json' }, redirect: 'error', signal: controller.signal });
      if (!response.ok) throw new Error(`health check returned HTTP ${response.status}`);
      const value: unknown = JSON.parse(await readBoundedResponseText(response, MAX_HEALTH_RESPONSE_BYTES));
      if (!isJsonObject(value) || value.status !== 'ok' || value.service !== 'fragforge') {
        throw new Error('health check did not identify FragForge');
      }
      if (value.endpoint !== url.host) throw new Error('health check endpoint did not match the discovered port');
      const expected = createHmac('sha256', verificationSecret).update(`${challenge}\n${url.host}`).digest();
      const proof = typeof value.proof === 'string' && /^[a-f0-9]{64}$/.test(value.proof)
        ? Buffer.from(value.proof, 'hex')
        : Buffer.alloc(0);
      if (proof.length !== expected.length || !timingSafeEqual(proof, expected)) {
        throw new Error('health check could not authenticate FragForge');
      }
    } catch (error: unknown) {
      if (signal?.aborted) throw new Error('FragForge operation was cancelled');
      const message = error instanceof Error ? error.message : String(error);
      throw new Error(`refusing discovered orchestrator at ${new URL(baseUrl).origin}: ${redactText(message, verificationSecret, this.#mutationToken)}`);
    } finally {
      clearTimeout(timeout);
      signal?.removeEventListener('abort', abortFromCaller);
    }
  }

  async #fetchJson(url: URL, init: RequestInit, timeoutMs = this.#requestTimeoutMs, signal?: AbortSignal): Promise<JsonValue> {
    const controller = new AbortController();
    const abortFromCaller = (): void => controller.abort();
    if (signal?.aborted) controller.abort();
    else signal?.addEventListener('abort', abortFromCaller, { once: true });
    const timeout = setTimeout(() => controller.abort(), timeoutMs);
    try {
      // Never follow redirects: a compromised/stale loopback service must not
      // forward the mutation token or uploaded local media to another origin.
      const response = await fetch(url, { ...init, redirect: 'error', signal: controller.signal });
      const text = response.status === 204 ? '' : await readBoundedResponseText(response, MAX_JSON_RESPONSE_BYTES);
      if (!response.ok) {
        throw new OrchestratorHttpError(response.status, orchestratorErrorMessage(response.status, text, this.#mutationToken));
      }
      if (text.trim() === '') return null;
      const value: unknown = JSON.parse(text);
      if (!isJsonValue(value)) throw new Error(`orchestrator returned non-JSON data for ${url.pathname}`);
      return redactSecret(value, this.#mutationToken);
    } catch (error: unknown) {
      if (error instanceof OrchestratorHttpError) throw error;
      if (signal?.aborted) throw new Error('FragForge operation was cancelled');
      if (controller.signal.aborted) {
        throw new Error(`FragForge orchestrator timed out after ${timeoutMs}ms`);
      }
      const message = error instanceof Error ? error.message : String(error);
      throw new Error(`FragForge Studio is offline or unreachable at ${url.origin}: ${message}`);
    } finally {
      clearTimeout(timeout);
      signal?.removeEventListener('abort', abortFromCaller);
    }
  }

  async #fetchText(url: URL, init: RequestInit, timeoutMs: number, signal?: AbortSignal): Promise<string> {
    const controller = new AbortController();
    const abortFromCaller = (): void => controller.abort();
    if (signal?.aborted) controller.abort();
    else signal?.addEventListener('abort', abortFromCaller, { once: true });
    const timeout = setTimeout(() => controller.abort(), timeoutMs);
    try {
      const response = await fetch(url, { ...init, redirect: 'error', signal: controller.signal });
      const value = response.status === 204 ? '' : await readBoundedResponseText(response, MAX_TEXT_RESPONSE_BYTES);
      if (!response.ok) {
        throw new OrchestratorHttpError(response.status, orchestratorErrorMessage(response.status, value, this.#mutationToken));
      }
      return redactText(value, this.#mutationToken);
    } catch (error: unknown) {
      if (error instanceof OrchestratorHttpError) throw error;
      if (signal?.aborted) throw new Error('FragForge operation was cancelled');
      if (controller.signal.aborted) throw new Error(`FragForge orchestrator timed out after ${timeoutMs}ms`);
      const message = error instanceof Error ? error.message : String(error);
      throw new Error(`FragForge Studio is offline or unreachable at ${url.origin}: ${message}`);
    } finally {
      clearTimeout(timeout);
      signal?.removeEventListener('abort', abortFromCaller);
    }
  }

  async #endpointFromPortsFile(): Promise<PortsFileEndpoint | undefined> {
    if (this.#portsFile === undefined || this.#portsFile === '') return undefined;
    let raw: string;
    try {
      raw = await readFile(this.#portsFile, 'utf8');
    } catch (error: unknown) {
      const code = errorCode(error);
      if (code === 'ENOENT') return undefined;
      throw new Error(`read FragForge ports file: ${errorMessage(error)}`);
    }
    const ports = parseJsonObject(raw, 'FragForge ports file');
    const port = ports.orchestrator;
    if (typeof port !== 'number' || !Number.isInteger(port) || port < 1 || port > 65_535) {
      throw new Error('FragForge ports file has an invalid orchestrator port');
    }
    const secret = ports.discovery_secret;
    if (secret !== undefined && (typeof secret !== 'string' || !/^[a-f0-9]{64}$/.test(secret))) {
      throw new Error('FragForge ports file has an invalid discovery_secret');
    }
    return {
      baseUrl: `http://127.0.0.1:${port}`,
      discoverySecret: typeof secret === 'string' ? secret : undefined,
    };
  }
}

async function validateUpload(filePath: string, extensions: ReadonlySet<string>, label: string, maxBytes: number): Promise<void> {
  if (!path.isAbsolute(filePath)) throw new Error(`${label}_path must be absolute`);
  if (!extensions.has(path.extname(filePath).toLowerCase())) {
    throw new Error(`${label}_path must use one of: ${[...extensions].sort().join(', ')}`);
  }
  const info = await stat(filePath);
  if (!info.isFile()) throw new Error(`${label}_path must point to a file`);
  if (info.size === 0) throw new Error(`${label}_path must not be empty`);
  if (info.size > maxBytes) throw new Error(`${label}_path exceeds the ${formatByteLimit(maxBytes)} limit`);
}

async function readBoundedResponseText(response: Response, maxBytes: number): Promise<string> {
  if (response.body === null) return '';
  const reader = response.body.getReader();
  const chunks: Buffer[] = [];
  let total = 0;
  while (true) {
    const next = await reader.read();
    if (next.done) break;
    const chunk = Buffer.from(next.value);
    total += chunk.length;
    if (total > maxBytes) {
      await reader.cancel();
      throw new Error(`orchestrator response exceeded the ${formatByteLimit(maxBytes)} limit`);
    }
    chunks.push(chunk);
  }
  return Buffer.concat(chunks, total).toString('utf8');
}

function formatByteLimit(bytes: number): string {
  if (bytes % 2 ** 30 === 0) return `${bytes / 2 ** 30} GiB`;
  if (bytes % 2 ** 20 !== 0 && bytes % 2 ** 10 === 0) return `${bytes / 2 ** 10} KiB`;
  return `${bytes / 2 ** 20} MiB`;
}

function orchestratorErrorMessage(status: number, text: string, secret: string | undefined): string {
  let detail = text.trim();
  if (detail !== '') {
    try {
      const body = parseJsonObject(detail, 'orchestrator error');
      const error = body.error;
      if (typeof error === 'string') detail = error;
    } catch {
      // Keep the bounded text response when the server did not return JSON.
    }
  }
  detail = redactText(detail, secret);
  if (detail.length > 2_000) detail = `${detail.slice(0, 2_000)}…`;
  return detail === '' ? `FragForge orchestrator returned HTTP ${status}` : `FragForge orchestrator returned HTTP ${status}: ${detail}`;
}

function redactSecret(value: JsonValue, secret: string | undefined): JsonValue {
  if (typeof value === 'string') return redactText(value, secret);
  if (Array.isArray(value)) return value.map((item) => redactSecret(item, secret));
  if (!isJsonObject(value)) return value;
  return Object.fromEntries(Object.entries(value).map(([key, item]) => [key, redactSecret(item, secret)]));
}

function redactText(value: string, ...secrets: Array<string | undefined>): string {
  let redacted = value;
  for (const secret of secrets) {
    if (secret !== undefined && secret !== '') redacted = redacted.split(secret).join('[redacted]');
  }
  return redacted;
}

function artifactReadAuthError(): Error {
  return new ArtifactReadAuthError();
}

function errorCode(error: unknown): string | undefined {
  if (typeof error !== 'object' || error === null || !('code' in error)) return undefined;
  return typeof error.code === 'string' ? error.code : undefined;
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}
