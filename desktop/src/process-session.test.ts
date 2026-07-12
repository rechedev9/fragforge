import test from 'node:test';
import assert from 'node:assert/strict';
import {
  ProcessSession,
  terminateProcessTree,
  type ProcessHandle,
  type ProcessLauncher,
  type ProcessTerminator,
} from './process-session.ts';

test('prefixes child output and exposes the first exit as a failure', async () => {
  const harness = sessionHarness();
  const launched = harness.session.launch('web', 'node.exe', ['server.js'], { PORT: '3000' });
  const child = harness.children[0];
  if (!child) throw new Error('expected a launched child');
  child.emitStdout('ready\n');
  child.emitStderr('warning\n');

  const rejected = assert.rejects(launched.exited, /web terminó inesperadamente \(código 7\)/);
  child.emitExit(7);
  await rejected;

  assert.deepEqual(harness.logs, ['[web] ready\n', '[web] warning\n', '[web] exited (7)\n']);
});

test('reports a spawn failure once even if an exit event follows', async () => {
  const harness = sessionHarness();
  const launched = harness.session.launch('orchestrator', 'missing.exe', [], {});
  const child = harness.children[0];
  if (!child) throw new Error('expected a launched child');

  const rejected = assert.rejects(launched.exited, /orchestrator no pudo iniciarse: blocked/);
  child.emitError(new Error('blocked'));
  child.emitExit(1);
  await rejected;

  assert.equal(harness.logs.filter((line) => line.includes('failed to start')).length, 1);
  assert.equal(harness.logs.some((line) => line.includes('exited (1)')), false);
});

test('notifies unexpected exits while the session is active', async () => {
  const harness = sessionHarness();
  const launched = harness.session.launch('web', 'node.exe', [], {});
  const child = harness.children[0];
  if (!child) throw new Error('expected a launched child');
  const failures: unknown[] = [];
  harness.session.watchUnexpectedExit(launched, (err) => failures.push(err));

  child.emitExit(9);
  await flushPromises();

  assert.equal(failures.length, 1);
  assert.match(String(failures[0]), /código 9/);
});

test('does not lose an exit that happened before the watcher was attached', async () => {
  const harness = sessionHarness();
  const launched = harness.session.launch('web', 'node.exe', [], {});
  const child = harness.children[0];
  if (!child) throw new Error('expected a launched child');
  child.emitExit(11);
  const failures: unknown[] = [];

  harness.session.watchUnexpectedExit(launched, (err) => failures.push(err));
  await flushPromises();

  assert.equal(failures.length, 1);
  assert.match(String(failures[0]), /código 11/);
});

test('suppresses an earlier exit when the session stopped before late watcher attachment', async () => {
  const harness = sessionHarness();
  const launched = harness.session.launch('web', 'node.exe', [], {});
  const child = harness.children[0];
  if (!child) throw new Error('expected a launched child');
  child.emitExit(12);
  assert.equal(harness.session.stop(), true);
  const failures: unknown[] = [];

  harness.session.watchUnexpectedExit(launched, (err) => failures.push(err));
  await flushPromises();

  assert.deepEqual(failures, []);
});

test('marks retry shutdowns expected before terminating children', async () => {
  const harness = sessionHarness({ emitExitDuringTermination: true });
  const first = harness.session.launch('orchestrator', 'zv-orchestrator.exe', [], {});
  const second = harness.session.launch('web', 'node.exe', [], {});
  const failures: unknown[] = [];
  harness.session.watchUnexpectedExit(first, (err) => failures.push(err));
  harness.session.watchUnexpectedExit(second, (err) => failures.push(err));

  assert.equal(harness.session.stop(), true);
  assert.equal(harness.session.stop(), true);
  await flushPromises();

  assert.deepEqual(harness.terminatedPids, [1000, 1001]);
  assert.deepEqual(failures, []);
});

test('continues stopping siblings when one terminator fails', () => {
  const harness = sessionHarness({ failingPid: 1000 });
  harness.session.launch('orchestrator', 'zv-orchestrator.exe', [], {});
  harness.session.launch('web', 'node.exe', [], {});

  const stopped = harness.session.stop();

  assert.equal(stopped, false);
  assert.deepEqual(harness.terminatedPids, [1000, 1001]);
  assert.match(harness.logs.join(''), /orchestrator.*could not stop process/);
});

test('retries only process trees whose first termination failed', () => {
  const children: FakeProcess[] = [];
  const attempts: number[] = [];
  const session = new ProcessSession({
    logLine: () => {},
    launchProcess: () => {
      const child = new FakeProcess(1000 + children.length);
      children.push(child);
      return child;
    },
    terminateProcess: (child) => {
      if (child.pid !== undefined) attempts.push(child.pid);
      if (child.pid === 1000 && attempts.filter((pid) => pid === 1000).length === 1) {
        throw new Error('transient taskkill failure');
      }
    },
  });
  session.launch('orchestrator', 'zv-orchestrator.exe', [], {});
  session.launch('web', 'node.exe', [], {});

  assert.equal(session.stop(), false);
  assert.equal(session.stop(), true);
  assert.deepEqual(attempts, [1000, 1001, 1000]);
});

test('refuses to launch new children after stop', () => {
  const harness = sessionHarness();
  assert.equal(harness.session.stop(), true);
  assert.throws(() => harness.session.launch('web', 'node.exe', [], {}), /stopped session/);
});

test('surfaces a nonzero Windows taskkill result', () => {
  const child = new FakeProcess(4242);
  assert.throws(
    () => terminateProcessTree(child, 'win32', () => ({ status: 5, stderr: 'Access is denied.' })),
    /taskkill exited 5: Access is denied/,
  );
  assert.equal(child.killed, true);
});

test('uses direct child termination away from Windows', () => {
  const child = new FakeProcess(4242);
  terminateProcessTree(child, 'linux');
  assert.equal(child.killed, true);
});

test('never taskkills a stale PID after a signal-style exit', () => {
  const child = new FakeProcess(4242);
  child.emitExit(null);
  let taskkillCalls = 0;

  terminateProcessTree(child, 'win32', () => {
    taskkillCalls += 1;
    return { status: 0 };
  });

  assert.equal(taskkillCalls, 0);
});

interface HarnessOptions {
  emitExitDuringTermination?: boolean;
  failingPid?: number;
}

interface Harness {
  session: ProcessSession;
  children: FakeProcess[];
  logs: string[];
  terminatedPids: number[];
}

function sessionHarness(options: HarnessOptions = {}): Harness {
  const children: FakeProcess[] = [];
  const logs: string[] = [];
  const terminatedPids: number[] = [];
  const launchProcess: ProcessLauncher = () => {
    const child = new FakeProcess(1000 + children.length);
    children.push(child);
    return child;
  };
  const terminateProcess: ProcessTerminator = (child) => {
    if (child.pid !== undefined) terminatedPids.push(child.pid);
    if (options.emitExitDuringTermination && child instanceof FakeProcess) child.emitExit(1);
    if (child.pid === options.failingPid) throw new Error('taskkill failed');
  };
  const session = new ProcessSession({
    logLine: (line) => logs.push(line),
    launchProcess,
    terminateProcess,
  });
  return { session, children, logs, terminatedPids };
}

class FakeProcess implements ProcessHandle {
  readonly pid: number;
  killed = false;
  hasExited = false;
  private readonly stdoutListeners: Array<(chunk: Buffer) => void> = [];
  private readonly stderrListeners: Array<(chunk: Buffer) => void> = [];
  private readonly errorListeners: Array<(err: Error) => void> = [];
  private readonly exitListeners: Array<(code: number | null) => void> = [];

  constructor(pid: number) {
    this.pid = pid;
  }

  onStdout(listener: (chunk: Buffer) => void): void {
    this.stdoutListeners.push(listener);
  }

  onStderr(listener: (chunk: Buffer) => void): void {
    this.stderrListeners.push(listener);
  }

  onError(listener: (err: Error) => void): void {
    this.errorListeners.push(listener);
  }

  onExit(listener: (code: number | null) => void): void {
    this.exitListeners.push(listener);
  }

  kill(): void {
    this.killed = true;
  }

  emitStdout(text: string): void {
    for (const listener of this.stdoutListeners) listener(Buffer.from(text));
  }

  emitStderr(text: string): void {
    for (const listener of this.stderrListeners) listener(Buffer.from(text));
  }

  emitError(err: Error): void {
    for (const listener of this.errorListeners) listener(err);
  }

  emitExit(code: number | null): void {
    this.hasExited = true;
    for (const listener of this.exitListeners) listener(code);
  }
}

async function flushPromises(): Promise<void> {
  await Promise.resolve();
  await Promise.resolve();
}
