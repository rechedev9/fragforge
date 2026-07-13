import type { JsonObject } from './json.ts';

export interface EvalScenario {
  abortGraceMs?: number;
  id: string;
  run: (signal: AbortSignal) => Promise<JsonObject>;
  timeoutMs?: number;
  title: string;
}

export interface EvalScenarioResult {
  duration_ms: number;
  error?: string;
  evidence?: JsonObject;
  id: string;
  status: 'failed' | 'passed';
  title: string;
}

export interface McpEvalReport {
  duration_ms: number;
  environment: JsonObject;
  finished_at: string;
  run_id: string;
  scenarios: EvalScenarioResult[];
  schema_version: 1;
  started_at: string;
  summary: {
    failed: number;
    passed: number;
    score: number;
    total: number;
  };
}

export class EvalAssertionError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'EvalAssertionError';
  }
}

export class EvalTimeoutError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'EvalTimeoutError';
  }
}

export class EvalProcessCleanupError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'EvalProcessCleanupError';
  }
}

export class EvalScenarioQuiescenceError extends EvalTimeoutError {
  readonly report: McpEvalReport | undefined;

  constructor(message: string, report?: McpEvalReport) {
    super(message);
    this.name = 'EvalScenarioQuiescenceError';
    this.report = report;
  }
}

class EvalScenarioTimeoutError extends EvalTimeoutError {
  constructor(message: string) {
    super(message);
    this.name = 'EvalScenarioTimeoutError';
  }
}

const DEFAULT_SCENARIO_TIMEOUT_MS = 30_000;
const DEFAULT_SCENARIO_ABORT_GRACE_MS = 3_000;

export function requireEval(condition: boolean, message: string): asserts condition {
  if (!condition) throw new EvalAssertionError(message);
}

export function requireEvalString(value: unknown, label: string): string {
  requireEval(typeof value === 'string' && value !== '', `${label} must be a non-empty string`);
  return value;
}

export function isConcreteArtifactUnavailableError(message: string): boolean {
  return /(?:\b404\b|not[ -]?found|does not exist|(?:artifact|file|resource|video).{0,40}unavailable|unavailable.{0,40}(?:artifact|file|resource|video))/i.test(message);
}

export async function runEvalScenarios(
  scenarios: readonly EvalScenario[],
  environment: JsonObject,
  now: () => Date = () => new Date(),
): Promise<McpEvalReport> {
  const started = now();
  const results: EvalScenarioResult[] = [];
  for (const [index, scenario] of scenarios.entries()) {
    const scenarioStarted = performance.now();
    const timeoutMs = scenario.timeoutMs ?? DEFAULT_SCENARIO_TIMEOUT_MS;
    const controller = new AbortController();
    try {
      const evidence = await runWithDeadline(
        scenario.run(controller.signal),
        controller,
        timeoutMs,
        scenario.abortGraceMs ?? DEFAULT_SCENARIO_ABORT_GRACE_MS,
        `scenario timed out after ${timeoutMs}ms`,
      );
      results.push({
        duration_ms: elapsedMilliseconds(scenarioStarted),
        evidence,
        id: scenario.id,
        status: 'passed',
        title: scenario.title,
      });
    } catch (error: unknown) {
      results.push({
        duration_ms: elapsedMilliseconds(scenarioStarted),
        error: errorMessage(error),
        id: scenario.id,
        status: 'failed',
        title: scenario.title,
      });
      if (error instanceof EvalScenarioQuiescenceError) {
        for (const remaining of scenarios.slice(index + 1)) {
          results.push({
            duration_ms: 0,
            error: `not run because prior scenario ${scenario.id} did not quiesce`,
            id: remaining.id,
            status: 'failed',
            title: remaining.title,
          });
        }
        throw new EvalScenarioQuiescenceError(
          error.message,
          buildEvalReport(started, now(), results, environment),
        );
      }
      if (error instanceof EvalTimeoutError) {
        for (const remaining of scenarios.slice(index + 1)) {
          results.push({
            duration_ms: 0,
            error: `not run because prior scenario ${scenario.id} timed out`,
            id: remaining.id,
            status: 'failed',
            title: remaining.title,
          });
        }
        break;
      }
    }
  }
  const finished = now();
  return buildEvalReport(started, finished, results, environment);
}

function buildEvalReport(
  started: Date,
  finished: Date,
  results: EvalScenarioResult[],
  environment: JsonObject,
): McpEvalReport {
  const passed = results.filter((result) => result.status === 'passed').length;
  const total = results.length;
  return {
    duration_ms: Math.max(0, finished.getTime() - started.getTime()),
    environment,
    finished_at: finished.toISOString(),
    run_id: evalRunID(started),
    scenarios: results,
    schema_version: 1,
    started_at: started.toISOString(),
    summary: {
      failed: total - passed,
      passed,
      score: total === 0 ? 0 : Math.round((passed / total) * 100),
      total,
    },
  };
}

export function runEvalFailureReport(
  id: string,
  title: string,
  error: unknown,
  environment: JsonObject,
  now: () => Date = () => new Date(),
): Promise<McpEvalReport> {
  const message = errorMessage(error);
  return runEvalScenarios([{
    id,
    run: async () => {
      throw new Error(message);
    },
    title,
  }], environment, now);
}

export async function runWithEvalTimeout<T>(
  parentSignal: AbortSignal,
  timeoutMs: number,
  label: string,
  operation: (signal: AbortSignal) => Promise<T>,
): Promise<T> {
  parentSignal.throwIfAborted();
  const timeoutSignal = AbortSignal.timeout(timeoutMs);
  try {
    return await operation(AbortSignal.any([parentSignal, timeoutSignal]));
  } catch (error: unknown) {
    if (timeoutSignal.aborted && !parentSignal.aborted) {
      throw new EvalTimeoutError(`${label} timed out after ${timeoutMs}ms`);
    }
    throw error;
  }
}

export async function publishEvalReportBeforeFatalExit(
  fatal: boolean,
  publish: () => Promise<void>,
  exit: (code: number) => void,
): Promise<void> {
  try {
    await publish();
  } finally {
    if (fatal) exit(1);
  }
}

export function evalFailureRequiresForcedExit(error: unknown): boolean {
  return error instanceof EvalScenarioQuiescenceError || error instanceof EvalProcessCleanupError;
}

export function releaseStoppedEvalResource<T>(
  resources: Set<T>,
  resource: T,
  stopped: boolean,
): void {
  if (stopped) resources.delete(resource);
}

function runWithDeadline<T>(
  promise: Promise<T>,
  controller: AbortController,
  timeoutMs: number,
  abortGraceMs: number,
  message: string,
): Promise<T> {
  return new Promise<T>((resolve, reject) => {
    let abortGrace: ReturnType<typeof setTimeout> | undefined;
    let settled = false;
    let timeoutError: EvalScenarioTimeoutError | undefined;
    const timeout = setTimeout(() => {
      timeoutError = new EvalScenarioTimeoutError(message);
      controller.abort(timeoutError);
      abortGrace = setTimeout(() => {
        settled = true;
        reject(new EvalScenarioQuiescenceError(
          `${message}; scenario did not quiesce within ${abortGraceMs}ms after abort`,
        ));
      }, abortGraceMs);
    }, timeoutMs);
    void promise.then(
      (value) => {
        if (settled) return;
        settled = true;
        clearTimeout(timeout);
        if (abortGrace !== undefined) clearTimeout(abortGrace);
        if (timeoutError === undefined) resolve(value);
        else reject(timeoutError);
      },
      (error: unknown) => {
        if (settled) return;
        settled = true;
        clearTimeout(timeout);
        if (abortGrace !== undefined) clearTimeout(abortGrace);
        reject(timeoutError ?? error);
      },
    );
  });
}

export function renderEvalMarkdown(report: McpEvalReport): string {
  const lines = [
    '# FragForge MCP evaluation',
    '',
    `- Run: \`${report.run_id}\``,
    `- Started: ${report.started_at}`,
    `- Score: **${report.summary.score}/100**`,
    `- Passed: **${report.summary.passed}/${report.summary.total}**`,
    `- Failed: **${report.summary.failed}**`,
    `- Duration: ${report.duration_ms} ms`,
    '',
    '| Status | Scenario | Duration | Evidence / failure |',
    '|---|---|---:|---|',
  ];
  for (const scenario of report.scenarios) {
    const status = scenario.status === 'passed' ? 'PASS' : 'FAIL';
    const detail = scenario.status === 'passed'
      ? compactEvidence(scenario.evidence)
      : scenario.error ?? 'unknown failure';
    lines.push(`| ${status} | \`${escapeTable(scenario.id)}\` — ${escapeTable(scenario.title)} | ${scenario.duration_ms} ms | ${escapeTable(detail)} |`);
  }
  lines.push('', '## Environment', '', '```json', JSON.stringify(report.environment, null, 2), '```', '');
  if (report.summary.failed > 0) {
    lines.push('## Feedback queue', '');
    for (const scenario of report.scenarios) {
      if (scenario.status === 'failed') lines.push(`- \`${scenario.id}\`: ${scenario.error ?? 'unknown failure'}`);
    }
    lines.push('');
  }
  return `${lines.join('\n')}\n`;
}

function elapsedMilliseconds(started: number): number {
  return Math.max(0, Math.round((performance.now() - started) * 100) / 100);
}

function evalRunID(date: Date): string {
  return date.toISOString().replace(/[:.]/g, '-');
}

function compactEvidence(evidence: JsonObject | undefined): string {
  if (evidence === undefined || Object.keys(evidence).length === 0) return 'completed';
  const serialized = JSON.stringify(evidence);
  return serialized.length <= 240 ? serialized : `${serialized.slice(0, 237)}...`;
}

function escapeTable(value: string): string {
  return value.replace(/\|/g, '\\|').replace(/[\r\n]+/g, ' ');
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}
