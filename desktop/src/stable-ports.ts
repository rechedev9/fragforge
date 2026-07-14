import { randomBytes } from 'node:crypto';
import * as fs from 'node:fs';
import * as net from 'node:net';

export interface StableServicePorts {
  orchestrator: number;
  web: number;
}

export interface StablePortOptions {
  host: string;
  portsFile: string;
  logLine: (line: string) => void;
  discoverySecret?: string;
  signal?: AbortSignal;
  isPortFree?: (port: number, host: string) => Promise<boolean>;
  allocateFreePort?: (host: string) => Promise<number>;
}

type ServiceKey = keyof StableServicePorts;

interface SavedPortOptions {
  host: string;
  logLine: (line: string) => void;
  isPortFree: (port: number, host: string) => Promise<boolean>;
}

const MAX_ALLOCATION_ATTEMPTS = 32;
const DISCOVERY_SECRET_BYTES = 32;
const DISCOVERY_SECRET_PATTERN = /^[a-f0-9]{64}$/;

/** Creates a fresh per-boot secret used only to authenticate local discovery. */
export function createDiscoverySecret(): string {
  return randomBytes(DISCOVERY_SECRET_BYTES).toString('hex');
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function isValidPort(value: unknown): value is number {
  return typeof value === 'number'
    && Number.isInteger(value)
    && value >= 1
    && value <= 65_535;
}

function originChangeHint(key: ServiceKey): string {
  return key === 'web'
    ? ' the reel library kept in the browser localStorage is keyed by origin, so it may appear empty on the new port'
    : '';
}

function throwIfAborted(signal: AbortSignal | undefined): void {
  if (signal?.aborted) throw new Error('port allocation aborted');
}

function readSavedPorts(portsFile: string): Record<string, unknown> {
  try {
    const parsed: unknown = JSON.parse(fs.readFileSync(portsFile, 'utf8'));
    return isRecord(parsed) ? parsed : {};
  } catch {
    return {};
  }
}

async function reusableSavedPort(
  key: ServiceKey,
  saved: Record<string, unknown>,
  selected: Set<number>,
  options: SavedPortOptions,
): Promise<number | undefined> {
  const savedPort = saved[key];
  if (!isValidPort(savedPort)) return undefined;

  if (selected.has(savedPort)) {
    options.logLine(
      `[ports] saved ${key} port ${savedPort} conflicts with another service, picking a new one;${originChangeHint(key)}\n`,
    );
    return undefined;
  }
  if (await options.isPortFree(savedPort, options.host)) {
    selected.add(savedPort);
    return savedPort;
  }

  options.logLine(
    `[ports] saved ${key} port ${savedPort} was taken, picking a new one;${originChangeHint(key)}\n`,
  );
  return undefined;
}

async function allocateDistinctPort(
  selected: Set<number>,
  host: string,
  allocateFreePort: (host: string) => Promise<number>,
  signal: AbortSignal | undefined,
): Promise<number> {
  for (let attempt = 0; attempt < MAX_ALLOCATION_ATTEMPTS; attempt += 1) {
    throwIfAborted(signal);
    const port = await allocateFreePort(host);
    throwIfAborted(signal);
    if (!isValidPort(port)) {
      throw new Error(`free port allocator returned invalid port ${String(port)}`);
    }
    if (!selected.has(port)) {
      selected.add(port);
      return port;
    }
  }
  throw new Error('could not allocate distinct desktop service ports');
}

function persistPorts(
  saved: Record<string, unknown>,
  portsFile: string,
  logLine: (line: string) => void,
): void {
  const temporary = `${portsFile}.tmp`;
  let descriptor: number | undefined;
  try {
    fs.rmSync(temporary, { force: true });
    descriptor = fs.openSync(temporary, 'w', 0o600);
    fs.writeFileSync(descriptor, JSON.stringify(saved));
    fs.fsyncSync(descriptor);
    fs.closeSync(descriptor);
    descriptor = undefined;
    fs.renameSync(temporary, portsFile);
  } catch (err) {
    if (descriptor !== undefined) {
      try {
        fs.closeSync(descriptor);
      } catch {
        // The original publication error below is the useful diagnostic.
      }
    }
    try {
      fs.rmSync(temporary, { force: true });
    } catch {
      // Best-effort cleanup must not replace the original publication error.
    }
    logLine(`[ports] could not persist service ports: ${String(err)}\n`);
  }
}

/**
 * Chooses stable, distinct loopback ports for both desktop services.
 *
 * Saved ports are evaluated in the same orchestrator-then-web order as the
 * original main-process implementation, while the selected set prevents a
 * duplicate/corrupt file from assigning one port to both children.
 */
export async function allocateStableServicePorts({
  host,
  portsFile,
  logLine,
  discoverySecret,
  signal,
  isPortFree = loopbackPortFree,
  allocateFreePort = allocateLoopbackPort,
}: StablePortOptions): Promise<StableServicePorts> {
  throwIfAborted(signal);
  if (discoverySecret !== undefined && !DISCOVERY_SECRET_PATTERN.test(discoverySecret)) {
    throw new Error('discovery secret must be 32 random bytes encoded as lowercase hex');
  }
  const saved = readSavedPorts(portsFile);
  const selected = new Set<number>();
  let orchestrator = await reusableSavedPort(
    'orchestrator',
    saved,
    selected,
    { host, logLine, isPortFree },
  );
  throwIfAborted(signal);
  let web = await reusableSavedPort('web', saved, selected, { host, logLine, isPortFree });
  throwIfAborted(signal);
  let changed = false;

  if (orchestrator === undefined) {
    orchestrator = await allocateDistinctPort(selected, host, allocateFreePort, signal);
    throwIfAborted(signal);
    saved.orchestrator = orchestrator;
    changed = true;
  }
  if (web === undefined) {
    web = await allocateDistinctPort(selected, host, allocateFreePort, signal);
    throwIfAborted(signal);
    saved.web = web;
    changed = true;
  }

  // The secret deliberately rotates on every desktop boot, even when both
  // stable ports are reusable. Persist it atomically in the same discovery
  // document before the orchestrator starts, without ever writing it to logs.
  if (discoverySecret !== undefined) {
    saved.discovery_secret = discoverySecret;
    changed = true;
  }

  if (changed) persistPorts(saved, portsFile, logLine);

  return { orchestrator, web };
}

/** Grabs an OS-assigned free loopback port, then releases it for the child. */
function allocateLoopbackPort(host: string): Promise<number> {
  return new Promise((resolve, reject) => {
    const server = net.createServer();
    server.unref();
    server.once('error', reject);
    server.listen(0, host, () => {
      const address = server.address();
      if (address === null || typeof address === 'string') {
        server.close(() => reject(new Error('free port server has no assigned address')));
        return;
      }
      const { port } = address;
      server.close(() => resolve(port));
    });
  });
}

/** Reports whether a specific loopback port is currently free. */
function loopbackPortFree(port: number, host: string): Promise<boolean> {
  return new Promise((resolve) => {
    const server = net.createServer();
    server.unref();
    server.once('error', () => resolve(false));
    server.listen(port, host, () => server.close(() => resolve(true)));
  });
}
