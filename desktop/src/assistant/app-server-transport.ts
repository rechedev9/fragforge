import { spawn, spawnSync, type ChildProcessWithoutNullStreams } from 'node:child_process';

/** The small, mockable transport surface used by the Codex app-server client. */
export interface AppServerTransport {
  onData(listener: (chunk: Buffer | string) => void): void;
  onDiagnostic(listener: (chunk: Buffer | string) => void): void;
  onError(listener: (error: Error) => void): void;
  onClose(listener: (reason: Error) => void): void;
  write(frame: string): Promise<void>;
  close(): void;
}

export interface AppServerLaunchOptions {
  /** Defaults to the locally installed Codex CLI discovered from PATH. */
  command?: string;
  /** Defaults to `['app-server', '--stdio']` for Codex's JSONL stdio transport. */
  args?: readonly string[];
  cwd?: string;
  env?: NodeJS.ProcessEnv;
}

export type AppServerTransportLauncher = (options: AppServerLaunchOptions) => AppServerTransport;

export interface NodeAppServerSpawnOptions {
  cwd?: string;
  env?: NodeJS.ProcessEnv;
  stdio: ['pipe', 'pipe', 'pipe'];
  windowsHide: true;
}

export type NodeAppServerSpawner = (
  command: string,
  args: string[],
  options: NodeAppServerSpawnOptions,
) => ChildProcessWithoutNullStreams;

export interface NodeAppServerProcess {
  readonly pid?: number;
  kill(): boolean;
}

export interface WindowsTaskkillResult {
  error?: Error;
  status: number | null;
  stderr?: string;
}

export type WindowsTaskkillRunner = (pid: number) => WindowsTaskkillResult;
export type NodeAppServerTerminator = (process: NodeAppServerProcess) => void;

/**
 * Starts Codex over its documented JSONL stdio transport. The adapter keeps
 * Node's ChildProcess details outside the protocol client so tests and future
 * non-stdio transports only need to implement AppServerTransport.
 */
export function launchCodexAppServer(options: AppServerLaunchOptions = {}): AppServerTransport {
  return createNodeAppServerLauncher()(options);
}

/** Wraps a ChildProcessWithoutNullStreams for production use and focused tests. */
export class NodeAppServerTransport implements AppServerTransport {
  readonly #child: ChildProcessWithoutNullStreams;
  readonly #dataListeners = new Set<(chunk: Buffer | string) => void>();
  readonly #diagnosticListeners = new Set<(chunk: Buffer | string) => void>();
  readonly #errorListeners = new Set<(error: Error) => void>();
  readonly #closeListeners = new Set<(reason: Error) => void>();
  readonly #terminate: NodeAppServerTerminator;
  #closed = false;
  #hasExited = false;

  constructor(child: ChildProcessWithoutNullStreams, terminate: NodeAppServerTerminator = terminateNodeAppServerProcess) {
    this.#child = child;
    this.#terminate = terminate;
    child.stdout.on('data', (chunk: Buffer | string) => this.#emitData(chunk));
    child.stderr.on('data', (chunk: Buffer | string) => this.#emitDiagnostic(chunk));
    child.once('error', (error: Error) => {
      this.#emitError(error);
      this.#emitClose(error);
    });
    child.once('exit', (code, signal) => {
      this.#hasExited = true;
      const detail = signal === null ? `code ${String(code)}` : `signal ${signal}`;
      this.#emitClose(new Error(`codex app-server exited with ${detail}`));
    });
  }

  onData(listener: (chunk: Buffer | string) => void): void {
    this.#dataListeners.add(listener);
  }

  onDiagnostic(listener: (chunk: Buffer | string) => void): void {
    this.#diagnosticListeners.add(listener);
  }

  onError(listener: (error: Error) => void): void {
    this.#errorListeners.add(listener);
  }

  onClose(listener: (reason: Error) => void): void {
    this.#closeListeners.add(listener);
  }

  write(frame: string): Promise<void> {
    if (this.#closed) return Promise.reject(new Error('codex app-server transport is closed'));
    return new Promise((resolve, reject) => {
      this.#child.stdin.write(frame, 'utf8', (error: Error | null | undefined) => {
        if (error !== null && error !== undefined) {
          reject(error);
          return;
        }
        resolve();
      });
    });
  }

  close(): void {
    if (this.#closed) return;
    this.#closed = true;
    try {
      this.#child.stdin.end();
    } catch {
      // The process may already have closed stdin; kill below still releases it.
    }
    try {
      if (!this.#hasExited) this.#terminate(this.#child);
    } catch (error) {
      this.#emitError(toError(error));
      this.#emitClose(toError(error));
    }
  }

  #emitData(chunk: Buffer | string): void {
    for (const listener of this.#dataListeners) safelyInvoke(() => listener(chunk), (error) => this.#emitError(error));
  }

  #emitDiagnostic(chunk: Buffer | string): void {
    for (const listener of this.#diagnosticListeners) safelyInvoke(() => listener(chunk), (error) => this.#emitError(error));
  }

  #emitError(error: Error): void {
    for (const listener of this.#errorListeners) {
      try {
        listener(error);
      } catch {
        // A host diagnostic callback must never break the transport.
      }
    }
  }

  #emitClose(reason: Error): void {
    if (this.#closed) return;
    this.#closed = true;
    for (const listener of this.#closeListeners) safelyInvoke(() => listener(reason), (error) => this.#emitError(error));
  }
}

/**
 * Kills the whole Windows command tree. This matters for the npm-provided
 * `codex.cmd` shim: killing cmd.exe alone can otherwise leave Codex running.
 */
export function terminateNodeAppServerProcess(
  child: NodeAppServerProcess,
  platform: NodeJS.Platform = process.platform,
  runTaskkill: WindowsTaskkillRunner = windowsTaskkill,
): void {
  if (child.pid === undefined) return;
  if (platform !== 'win32') {
    child.kill();
    return;
  }
  const result = runTaskkill(child.pid);
  if (result.error === undefined && result.status === 0) return;
  let directError: unknown;
  try {
    child.kill();
  } catch (error) {
    directError = error;
  }
  if (result.error !== undefined) {
    if (directError === undefined) throw result.error;
    throw new Error(`${result.error.message}; direct kill also failed: ${String(directError)}`);
  }
  const detail = result.stderr?.trim();
  const taskkillError = new Error(`taskkill exited ${String(result.status)}${detail ? `: ${detail}` : ''}`);
  if (directError === undefined) throw taskkillError;
  throw new Error(`${taskkillError.message}; direct kill also failed: ${String(directError)}`);
}

/** Allows a caller to keep the real launcher shape while replacing spawn in a focused test. */
export function createNodeAppServerLauncher(
  spawnProcess: NodeAppServerSpawner = spawnNodeAppServer,
): AppServerTransportLauncher {
  return (options: AppServerLaunchOptions): AppServerTransport => {
    const command = options.command ?? 'codex';
    const args = [...(options.args ?? ['app-server', '--stdio'])];
    const child = launchNodeProcess(spawnProcess, command, args, {
      cwd: options.cwd,
      env: options.env,
      stdio: ['pipe', 'pipe', 'pipe'],
      windowsHide: true,
    });
    return new NodeAppServerTransport(child);
  };
}

function spawnNodeAppServer(
  command: string,
  args: string[],
  options: NodeAppServerSpawnOptions,
): ChildProcessWithoutNullStreams {
  return spawn(command, args, options);
}

/**
 * Windows installs of the npm CLI commonly expose `codex.cmd`, which cannot
 * be started directly with shell:false. The default path goes through cmd.exe
 * with AutoRun disabled and a command line made only from validated argv
 * tokens. Callers that need an arbitrary executable can inject a transport.
 */
function launchNodeProcess(
  spawnProcess: NodeAppServerSpawner,
  command: string,
  args: string[],
  options: NodeAppServerSpawnOptions,
): ChildProcessWithoutNullStreams {
  if (process.platform !== 'win32' || !usesWindowsCommandProcessor(command)) {
    return spawnProcess(command, args, options);
  }
  return spawnProcess('cmd.exe', ['/d', '/s', '/c', windowsCommandLine(command, args)], options);
}

function usesWindowsCommandProcessor(command: string): boolean {
  const lower = command.toLowerCase();
  return command === 'codex' || lower.endsWith('.cmd') || lower.endsWith('.bat');
}

/** Exposed for focused Windows cmd-shim regression tests. */
export function windowsCommandLine(command: string, args: string[]): string {
  return [command, ...args].map(quoteWindowsCommandToken).join(' ');
}

function quoteWindowsCommandToken(token: string): string {
  // This client has no need for shell syntax. Rejecting it makes cmd.exe a
  // compatibility shim for codex.cmd rather than a second command API.
  if (token === '' || /[\r\n&|<>()^%!"]/u.test(token)) {
    throw new Error('codex app-server command contains unsupported Windows shell syntax');
  }
  return /\s/u.test(token) ? `"${token}"` : token;
}

function windowsTaskkill(pid: number): WindowsTaskkillResult {
  const result = spawnSync('taskkill', ['/pid', String(pid), '/T', '/F'], {
    encoding: 'utf8',
    windowsHide: true,
  });
  return {
    error: result.error,
    status: result.status,
    stderr: result.stderr,
  };
}

function safelyInvoke(run: () => void, onError: (error: Error) => void): void {
  try {
    run();
  } catch (error) {
    onError(toError(error));
  }
}

function toError(value: unknown): Error {
  return value instanceof Error ? value : new Error(String(value));
}
