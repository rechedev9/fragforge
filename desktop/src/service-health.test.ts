import test from 'node:test';
import assert from 'node:assert/strict';
import { waitForDesktopServices, type HttpWaiter } from './service-health.ts';

test('fails immediately when web exits during orchestrator health polling', async () => {
  const child = deferred<never>();
  const neverHealthy = new Promise<void>(() => {});
  const waiting = waitForDesktopServices({
    orchestratorUrl: 'http://127.0.0.1:1001',
    webUrl: 'http://127.0.0.1:1002',
    timeoutMs: 60_000,
    signal: new AbortController().signal,
    childExited: child.promise,
    wait: () => neverHealthy,
  });

  child.reject(new Error('web exited'));
  await assert.rejects(waiting, /web exited/);
});

test('keeps observing orchestrator exit during web health polling', async () => {
  const child = deferred<never>();
  const webStarted = deferred<void>();
  const webHealth = new Promise<void>(() => {});
  const wait: HttpWaiter = (url) => {
    if (url.endsWith('/healthz')) return Promise.resolve();
    webStarted.resolve();
    return webHealth;
  };
  const waiting = waitForDesktopServices({
    orchestratorUrl: 'http://127.0.0.1:1001',
    webUrl: 'http://127.0.0.1:1002',
    timeoutMs: 60_000,
    signal: new AbortController().signal,
    childExited: child.promise,
    wait,
  });
  await webStarted.promise;

  child.reject(new Error('orchestrator exited'));
  await assert.rejects(waiting, /orchestrator exited/);
});

interface Deferred<T> {
  promise: Promise<T>;
  resolve(value: T): void;
  reject(err: Error): void;
}

function deferred<T>(): Deferred<T> {
  let resolvePromise: ((value: T) => void) | undefined;
  let rejectPromise: ((err: Error) => void) | undefined;
  const promise = new Promise<T>((resolve, reject) => {
    resolvePromise = resolve;
    rejectPromise = reject;
  });
  return {
    promise,
    resolve(value): void {
      if (!resolvePromise) throw new Error('deferred resolve is unavailable');
      resolvePromise(value);
    },
    reject(err): void {
      if (!rejectPromise) throw new Error('deferred reject is unavailable');
      rejectPromise(err);
    },
  };
}
