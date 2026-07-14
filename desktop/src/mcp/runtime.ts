import { existsSync } from 'node:fs';
import * as path from 'node:path';
import type { Readable, Writable } from 'node:stream';
import { OrchestratorClient } from './orchestrator-client.ts';
import { FragForgeMcpServer } from './server.ts';

const ORCHESTRATOR_URL_ENV = 'FRAGFORGE_ORCHESTRATOR_URL';
const MUTATION_TOKEN_ENV = 'FRAGFORGE_MUTATION_TOKEN';
const PORTS_FILE_ENV = 'FRAGFORGE_PORTS_FILE';
const REQUEST_TIMEOUT_ENV = 'FRAGFORGE_MCP_TIMEOUT_MS';

export interface RunFragForgeMcpOptions {
  diagnostics?: Writable;
  input: Readable;
  onClose?: (reason: Error) => void;
  output: Writable;
  serverVersion: string;
  userDataDir?: string;
}

export function runFragForgeMcp(options: RunFragForgeMcpOptions): FragForgeMcpServer {
  const timeout = requestTimeout();
  const server = new FragForgeMcpServer({
    client: new OrchestratorClient({
      baseUrl: nonEmptyEnvironment(ORCHESTRATOR_URL_ENV),
      mutationToken: nonEmptyEnvironment(MUTATION_TOKEN_ENV),
      portsFile: resolvePortsFile(options.userDataDir),
      requestTimeoutMs: timeout,
    }),
    diagnostics: options.diagnostics,
    elicitationTimeoutMs: timeout,
    input: options.input,
    onClose: options.onClose,
    output: options.output,
    serverVersion: options.serverVersion,
  });
  server.start();
  return server;
}

export function resolvePortsFile(userDataDir?: string): string | undefined {
  const explicit = nonEmptyEnvironment(PORTS_FILE_ENV);
  if (explicit !== undefined) return path.resolve(explicit);
  if (userDataDir !== undefined && userDataDir !== '') return path.join(userDataDir, 'ports.json');
  const appData = nonEmptyEnvironment('APPDATA');
  if (appData === undefined) return undefined;
  const candidates = [
    path.join(appData, 'FragForge Studio', 'ports.json'),
    path.join(appData, 'fragforge-studio', 'ports.json'),
  ];
  return candidates.find((candidate) => existsSync(candidate)) ?? candidates[0];
}

function requestTimeout(): number | undefined {
  const raw = nonEmptyEnvironment(REQUEST_TIMEOUT_ENV);
  if (raw === undefined) return undefined;
  const timeout = Number(raw);
  if (!Number.isInteger(timeout) || timeout < 100 || timeout > 300_000) {
    throw new Error(`${REQUEST_TIMEOUT_ENV} must be an integer from 100 to 300000`);
  }
  return timeout;
}

function nonEmptyEnvironment(name: string): string | undefined {
  const value = process.env[name];
  return value === undefined || value.trim() === '' ? undefined : value.trim();
}
