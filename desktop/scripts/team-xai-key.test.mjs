import test from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, readFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import {
  assembleUsesTeamXAIKey,
  environmentWithoutXAIAPIKey,
  resolveTeamXAIKey,
  stageTeamXAIKey,
} from './team-xai-key.mjs';

test('team assembly requires a valid key without including it in errors', () => {
  assert.throws(
    () => resolveTeamXAIKey(true, {}),
    /team build requires XAI_API_KEY to contain a single non-empty line/,
  );
  const canary = 'do-not-echo-this\nsecond-line';
  assert.throws(
    () => resolveTeamXAIKey(true, { XAI_API_KEY: canary }),
    (err) => err instanceof Error && !err.message.includes(canary),
  );
});

test('normal assembly never selects an environment key', () => {
  assert.equal(resolveTeamXAIKey(false, { XAI_API_KEY: 'must-not-be-bundled' }), '');
});

test('normal staging overwrites a previously staged team key', (t) => {
  const directory = mkdtempSync(join(tmpdir(), 'fragforge-team-stage-'));
  t.after(() => rmSync(directory, { recursive: true, force: true }));

  stageTeamXAIKey(directory, 'packaged-team-key');
  assert.equal(readFileSync(join(directory, 'xai-api-key'), 'utf8'), 'packaged-team-key');

  stageTeamXAIKey(directory, '');
  assert.equal(readFileSync(join(directory, 'xai-api-key'), 'utf8'), '');
});

test('rejects unsupported build arguments without echoing them', () => {
  const canary = '--team-xai-key=do-not-print-this';
  assert.throws(
    () => assembleUsesTeamXAIKey([canary]),
    (err) => err instanceof Error && /unsupported assemble argument/.test(err.message)
      && !err.message.includes(canary),
  );
  assert.equal(assembleUsesTeamXAIKey([]), false);
  assert.equal(assembleUsesTeamXAIKey(['--team-xai-key']), true);
});

test('removes every casing of XAI_API_KEY from a copied environment', () => {
  const original = {
    Path: 'C:\\tools',
    XAI_API_KEY: 'upper',
    xai_api_key: 'lower',
    XaI_ApI_KeY: 'mixed',
  };

  const sanitized = environmentWithoutXAIAPIKey(original);

  assert.deepEqual(sanitized, { Path: 'C:\\tools' });
  assert.equal(original.XAI_API_KEY, 'upper');
});
