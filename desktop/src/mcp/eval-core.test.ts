import assert from 'node:assert/strict';
import test from 'node:test';
import {
  EvalAssertionError,
  isConcreteArtifactUnavailableError,
  renderEvalMarkdown,
  requireEval,
  requireEvalString,
  runEvalScenarios,
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
  let abortObserved = false;
  let cleanupCompleted = false;
  let nextScenarioRan = false;
  const report = await runEvalScenarios([
    {
      id: 'stuck',
      run: async (signal) => {
        try {
          await new Promise<never>((_resolve, reject) => {
            signal.addEventListener('abort', () => {
              abortObserved = signal.aborted;
              reject(signal.reason);
            }, { once: true });
          });
          return { completed: true };
        } finally {
          await new Promise<void>((resolve) => setTimeout(resolve, 5));
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

  assert.equal(abortObserved, true);
  assert.equal(cleanupCompleted, true);
  assert.equal(nextScenarioRan, false);
  assert.deepEqual(report.summary, { failed: 2, passed: 0, score: 0, total: 2 });
  assert.equal(report.scenarios[0]?.error, 'scenario timed out after 10ms');
  assert.equal(report.scenarios[1]?.error, 'not run because prior scenario stuck timed out');
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
