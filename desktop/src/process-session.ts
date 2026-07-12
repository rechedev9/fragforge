import { spawn, spawnSync, type ChildProcess } from 'node:child_process';

/** The child-process surface the session owns; deliberately smaller than Node's ChildProcess. */
export interface ProcessHandle {
  readonly pid: number | undefined;
  readonly killed: boolean;
  readonly hasExited: boolean;
  onStdout(listener: (chunk: Buffer) => void): void;
  onStderr(listener: (chunk: Buffer) => void): void;
  onError(listener: (err: Error) => void): void;
  onExit(listener: (code: number | null) => void): void;
  kill(): void;
}

export interface LaunchedProcess {
  // Rejects when the child fails to start or exits; it intentionally never resolves.
  exited: Promise<never>;
}

export type ProcessLauncher = (
  executable: string,
  args: string[],
  env: Record<string, string>,
) => ProcessHandle;

export type ProcessTerminator = (process: ProcessHandle) => void;

export interface TaskkillResult {
  status: number | null;
  error?: Error;
  stderr?: string;
}

export type TaskkillRunner = (pid: number) => TaskkillResult;

export interface ProcessSessionOptions {
  logLine: (text: string) => void;
  launchProcess?: ProcessLauncher;
  terminateProcess?: ProcessTerminator;
}

interface OwnedProcess {
  label: string;
  handle: ProcessHandle;
}

/**
 * Owns every child started by one desktop boot attempt. Stopping the session
 * marks all subsequent exits as expected before terminating any process, so a
 * retry or application quit cannot be mistaken for a backend crash.
 */
export class ProcessSession {
  private readonly logLine: (text: string) => void;
  private readonly launchProcess: ProcessLauncher;
  private readonly terminateProcess: ProcessTerminator;
  private readonly processes: OwnedProcess[] = [];
  private pendingTermination: OwnedProcess[] | null = null;
  private stopping = false;

  constructor(options: ProcessSessionOptions) {
    this.logLine = options.logLine;
    this.launchProcess = options.launchProcess ?? launchNodeProcess;
    this.terminateProcess = options.terminateProcess ?? terminateProcessTree;
  }

  launch(label: string, executable: string, args: string[], env: Record<string, string>): LaunchedProcess {
    if (this.stopping) throw new Error('cannot launch a process in a stopped session');

    const handle = this.launchProcess(executable, args, env);
    this.processes.push({ label, handle });
    const tag = (chunk: Buffer): void => this.logLine(`[${label}] ${String(chunk)}`);
    handle.onStdout(tag);
    handle.onStderr(tag);

    let settled = false;
    const exited = new Promise<never>((_resolve, reject) => {
      handle.onError((err) => {
        if (settled) return;
        settled = true;
        this.logLine(`[${label}] failed to start: ${String(err)}\n`);
        // Desktop-facing process failures are Spanish to match the rest of the
        // app chrome and preserve the messages shown before this extraction.
        reject(new Error(`${label} no pudo iniciarse: ${err.message}`));
      });
      handle.onExit((code) => {
        if (settled) return;
        settled = true;
        this.logLine(`[${label}] exited (${code})\n`);
        reject(new Error(`${label} terminó inesperadamente (código ${code})`));
      });
    });
    exited.catch(() => {}); // Consumers observe it selectively; never leave an unhandled rejection.
    return { exited };
  }

  /** Runs a callback only when a child exits while this boot session is still active. */
  watchUnexpectedExit(process: LaunchedProcess, onUnexpectedExit: (err: unknown) => void): void {
    process.exited.catch((err: unknown) => {
      if (this.stopping) return;
      onUnexpectedExit(err);
    });
  }

  /**
   * Stops the owned process trees, returning false when any termination must be
   * retried. The session remains permanently closed to new launches as soon as
   * the first stop begins, so every resulting exit is still expected.
   */
  stop(): boolean {
    if (!this.stopping) {
      this.stopping = true;
      this.pendingTermination = [...this.processes];
    }
    const pending = this.pendingTermination ?? [];
    const failed: OwnedProcess[] = [];
    for (const process of pending) {
      try {
        this.terminateProcess(process.handle);
      } catch (err) {
        failed.push(process);
        this.logLine(`[${process.label}] could not stop process: ${String(err)}\n`);
      }
    }
    this.pendingTermination = failed;
    return failed.length === 0;
  }
}

function launchNodeProcess(
  executable: string,
  args: string[],
  env: Record<string, string>,
): ProcessHandle {
  const child = spawn(executable, args, {
    env: { ...process.env, ...env },
    windowsHide: true,
  });
  return nodeProcessHandle(child);
}

function nodeProcessHandle(child: ChildProcess): ProcessHandle {
  let hasExited = child.exitCode !== null || child.signalCode !== null;
  return {
    get pid(): number | undefined {
      return child.pid;
    },
    get killed(): boolean {
      return child.killed;
    },
    get hasExited(): boolean {
      return hasExited;
    },
    onStdout(listener): void {
      child.stdout?.on('data', listener);
    },
    onStderr(listener): void {
      child.stderr?.on('data', listener);
    },
    onError(listener): void {
      child.once('error', listener);
    },
    onExit(listener): void {
      child.once('exit', (code) => {
        hasExited = true;
        listener(code);
      });
    },
    kill(): void {
      child.kill();
    },
  };
}

/** Terminates descendants too: the orchestrator can own recorder -> HLAE -> CS2. */
export function terminateProcessTree(
  handle: ProcessHandle,
  platform: NodeJS.Platform = process.platform,
  runTaskkill: TaskkillRunner = windowsTaskkill,
): void {
  // exitCode stays null for signal-driven exits on Node/Windows. The adapter's
  // explicit event-backed state prevents retrying taskkill against a stale,
  // potentially reused PID after the direct child has definitely exited.
  if (!handle.pid || handle.hasExited) return;
  if (platform === 'win32') {
    const result = runTaskkill(handle.pid);
    const detail = result.stderr?.trim();
    const failure = result.error
      ?? (result.status === 0 ? undefined : new Error(`taskkill exited ${result.status}${detail ? `: ${detail}` : ''}`));
    if (!failure) return;
    // taskkill owns descendant cleanup; direct kill is the last-resort attempt
    // to avoid leaving the orchestrator itself alive when Windows rejects it.
    try {
      handle.kill();
    } catch (fallbackErr) {
      throw new Error(`${failure.message}; direct kill also failed: ${String(fallbackErr)}`);
    }
    throw failure;
  }
  if (handle.killed) return;
  handle.kill();
}

function windowsTaskkill(pid: number): TaskkillResult {
  const result = spawnSync('taskkill', ['/pid', String(pid), '/T', '/F'], {
    encoding: 'utf8',
    windowsHide: true,
  });
  return {
    status: result.status,
    error: result.error,
    stderr: result.stderr,
  };
}
