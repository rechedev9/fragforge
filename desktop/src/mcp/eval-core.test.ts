import assert from 'node:assert/strict';
import test from 'node:test';
import {
  EvalAssertionError,
  EvalProcessCleanupError,
  EvalScenarioQuiescenceError,
  EvalTimeoutError,
  evalFailureRequiresForcedExit,
  isConcreteArtifactUnavailableError,
  publishEvalReportBeforeFatalExit,
  releaseStoppedEvalResource,
  renderEvalMarkdown,
  requireEval,
  requireEvalString,
  runEvalFailureReport,
  runEvalScenarios,
  runWithEvalTimeout,
} from './eval-core.ts';

test('runs every eval scenario, scores failures, and preserves actionable evidence', async () => {
  const dates = [new Date('2026-07-13T10:00:00.000Z'), new Date('2026-07-13T10:00:01.250Z')];
  const report = await runEvalScenarios([
    { id: 'green', run: async () => ({ operation: 'studio.status' }), title: 'Green scenario' },
    { id: 'red', run: async () => { throw new Error('expected confirmation barrier'); }, title: 'Red scenario' },
  ], { target: 'test' }, () => dates.shift() ?? new Date('2026-07-13T10:00:01.250Z'));

  assert.deepEqual(report.summary, { failed: 1, passed: 1, score: 50, total: 2 });
  assert.equal(report.duration_ms, 1_250);
  assert.equal(report.scenarios[0]?.status, 'passed');
  assert.deepEqual(report.scenarios[0]?.evidence, { operation: 'studio.status' });
  assert.equal(report.scenarios[1]?.error, 'expected confirmation barrier');
  assert.match(renderEvalMarkdown(report), /Feedback queue[\s\S]*`red`: expected confirmation barrier/);
});

test('renders a fully green report without a feedback queue', async () => {
  const report = await runEvalScenarios([
    { id: 'all-green', run: async () => ({}), title: 'Everything works' },
  ], {});

  const markdown = renderEvalMarkdown(report);
  assert.match(markdown, /Score: \*\*100\/100\*\*/);
  assert.match(markdown, /Passed: \*\*1\/1\*\*/);
  assert.doesNotMatch(markdown, /Feedback queue/);
});

test('aborts a stuck scenario and stops the remaining shared-context evaluation queue', async () => {
  const abortStarted = deferred();
  const cleanupPhaseOneGate = deferred();
  const cleanupPhaseOneCompleted = deferred();
  const cleanupPhaseTwoGate = deferred();
  let abortObserved = false;
  let cleanupCompleted = false;
  let nextScenarioRan = false;
  let reportResolved = false;
  const reportPromise = runEvalScenarios([
    {
      id: 'stuck',
      run: async (signal) => {
        try {
          await new Promise<never>((_resolve, reject) => {
            signal.addEventListener('abort', () => {
              abortObserved = signal.aborted;
              abortStarted.resolve();
              reject(signal.reason);
            }, { once: true });
          });
          return { completed: true };
        } finally {
          await cleanupPhaseOneGate.promise;
          cleanupPhaseOneCompleted.resolve();
          await cleanupPhaseTwoGate.promise;
          cleanupCompleted = true;
        }
      },
      timeoutMs: 10,
      title: 'Stuck scenario',
    },
    {
      id: 'next',
      run: async () => {
        nextScenarioRan = true;
        return { completed: true };
      },
      title: 'Next scenario',
    },
  ], {});
  void reportPromise.then(() => {
    reportResolved = true;
  });

  await abortStarted.promise;
  assert.equal(reportResolved, false, 'report returned before abort cleanup started');
  cleanupPhaseOneGate.resolve();
  await cleanupPhaseOneCompleted.promise;
  assert.equal(reportResolved, false, 'report returned between cleanup phases');
  cleanupPhaseTwoGate.resolve();
  const report = await reportPromise;

  assert.equal(abortObserved, true);
  assert.equal(cleanupCompleted, true);
  assert.equal(nextScenarioRan, false);
  assert.deepEqual(report.summary, { failed: 2, passed: 0, score: 0, total: 2 });
  assert.equal(report.scenarios[0]?.error, 'scenario timed out after 10ms');
  assert.equal(report.scenarios[1]?.error, 'not run because prior scenario stuck timed out');
});

test('treats an operation timeout as terminal for the shared-context queue', async () => {
  let nextScenarioRan = false;
  const report = await runEvalScenarios([
    {
      id: 'io-timeout',
      run: async () => {
        throw new EvalTimeoutError('MCP request timed out');
      },
      title: 'I/O timeout',
    },
    {
      id: 'next',
      run: async () => {
        nextScenarioRan = true;
        return { completed: true };
      },
      title: 'Next scenario',
    },
  ], {});

  assert.equal(nextScenarioRan, false);
  assert.equal(report.scenarios[0]?.error, 'MCP request timed out');
  assert.equal(report.scenarios[1]?.error, 'not run because prior scenario io-timeout timed out');
});

test('fails fatally instead of returning a report while abort cleanup is still active', async () => {
  const releaseCleanup = deferred();
  const cleanupCompleted = deferred();
  let nextScenarioRan = false;
  let fatalError: EvalScenarioQuiescenceError | undefined;
  const reportPromise = runEvalScenarios([
    {
      id: 'passed-before',
      run: async () => ({ completed: true }),
      title: 'Passed before',
    },
    {
      abortGraceMs: 10,
      id: 'uncooperative-cleanup',
      run: async (signal) => {
        try {
          await new Promise<never>((_resolve, reject) => {
            signal.addEventListener('abort', () => reject(signal.reason), { once: true });
          });
          return { completed: true };
        } finally {
          await releaseCleanup.promise;
          cleanupCompleted.resolve();
        }
      },
      timeoutMs: 5,
      title: 'Uncooperative cleanup',
    },
    {
      id: 'next',
      run: async () => {
        nextScenarioRan = true;
        return { completed: true };
      },
      title: 'Next scenario',
    },
  ], {});

  await assert.rejects(reportPromise, (error: unknown) => {
    if (!(error instanceof EvalScenarioQuiescenceError)) return false;
    fatalError = error;
    return true;
  });
  assert.equal(nextScenarioRan, false);
  assert.ok(fatalError?.report);
  assert.equal(fatalError.report.summary.passed, 1);
  assert.deepEqual(
    fatalError.report.scenarios.map((scenario) => scenario.id),
    ['passed-before', 'uncooperative-cleanup', 'next'],
  );
  assert.equal(
    fatalError.report.scenarios[2]?.error,
    'not run because prior scenario uncooperative-cleanup did not quiesce',
  );
  releaseCleanup.resolve();
  await cleanupCompleted.promise;
});

test('turns a fatal quiescence error into a writable failure report', async () => {
  const error = new EvalScenarioQuiescenceError('scenario cleanup remained active');

  const report = await runEvalFailureReport(
    'bootstrap.real-processes',
    'Start isolated real processes',
    error,
    {},
  );

  assert.equal(report.summary.failed, 1);
  assert.equal(report.scenarios[0]?.id, 'bootstrap.real-processes');
  assert.equal(report.scenarios[0]?.error, error.message);
});

test('maps a timeout after an operation has started to the terminal eval timeout type', async () => {
  // AbortSignal.timeout()'s internal timer is unref'd, so on an otherwise idle
  // event loop node:test can lose track of the pending abort/rejection before
  // it settles, cancelling every later test in the file (nodejs/node#49952).
  // Keep the loop alive for the duration so the rejection is observed here.
  const keepAlive = setInterval(() => {}, 1_000);
  try {
    const parent = new AbortController();

    const operation = runWithEvalTimeout(parent.signal, 5, 'response body', async (signal) => {
      await Promise.resolve();
      return await new Promise<never>((_resolve, reject) => {
        signal.addEventListener('abort', () => reject(signal.reason), { once: true });
      });
    });

    await assert.rejects(
      operation,
      (error: unknown) => error instanceof EvalTimeoutError && error.message === 'response body timed out after 5ms',
    );
  } finally {
    clearInterval(keepAlive);
  }
});

test('requests fatal exit even when the best-effort report write fails', async () => {
  let exitCode: number | undefined;

  await assert.rejects(
    publishEvalReportBeforeFatalExit(
      true,
      async () => { throw new Error('report disk unavailable'); },
      (code) => { exitCode = code; },
    ),
    /report disk unavailable/,
  );

  assert.equal(exitCode, 1);
});

test('process cleanup failures require the same forced-exit report boundary', async () => {
  const cleanupError = new EvalProcessCleanupError('tracked MCP process remains alive');
  let exitCode: number | undefined;

  await publishEvalReportBeforeFatalExit(
    evalFailureRequiresForcedExit(cleanupError),
    async () => {},
    (code) => { exitCode = code; },
  );

  assert.equal(exitCode, 1);
  assert.equal(evalFailureRequiresForcedExit(new Error('ordinary assertion')), false);
});

test('retains a tracked eval resource until process death is verified', () => {
  const resource = { id: 'secondary-mcp' };
  const resources = new Set([resource]);

  releaseStoppedEvalResource(resources, resource, false);
  assert.equal(resources.has(resource), true);

  releaseStoppedEvalResource(resources, resource, true);
  assert.equal(resources.has(resource), false);
});

test('evaluation assertions provide stable failures and string narrowing', () => {
  assert.throws(() => requireEval(false, 'broken invariant'), EvalAssertionError);
  assert.equal(requireEvalString('job-id', 'job_id'), 'job-id');
  assert.throws(() => requireEvalString('', 'job_id'), /job_id must be a non-empty string/);
});

test('artifact availability classification rejects unrelated tool failures', () => {
  assert.equal(isConcreteArtifactUnavailableError('GET artifact returned HTTP 404'), true);
  assert.equal(isConcreteArtifactUnavailableError('final video not found'), true);
  assert.equal(isConcreteArtifactUnavailableError('artifact is unavailable'), true);
  assert.equal(isConcreteArtifactUnavailableError('orchestrator unavailable'), false);
  assert.equal(isConcreteArtifactUnavailableError('request timed out'), false);
});

function deferred(): { promise: Promise<void>; resolve: () => void } {
  let resolve = (): void => {};
  const promise = new Promise<void>((complete) => {
    resolve = complete;
  });
  return { promise, resolve };
}
