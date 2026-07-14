import test from 'node:test';
import assert from 'node:assert/strict';
import * as fs from 'node:fs';
import * as os from 'node:os';
import * as path from 'node:path';
import {
  normalizeXAIAPIKey,
  resolveXAIAPIKey,
  resolveXAIAPIKeyDetails,
  takeXAIAPIKeyFromEnvironment,
} from './xai-api-key.ts';

test('prefers an explicit local xAI API key without reading the bundle', () => {
  const key = resolveXAIAPIKey({
    environmentValue: '  local-team-key  ',
    bundledPath: 'unused',
    readFile: () => {
      throw new Error('bundled key should not be read');
    },
  });

  assert.equal(key, 'local-team-key');
});

test('loads the packaged team xAI API key when no local override exists', (t) => {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'fragforge-team-key-'));
  t.after(() => fs.rmSync(directory, { recursive: true, force: true }));
  const bundledPath = path.join(directory, 'xai-api-key');
  fs.writeFileSync(bundledPath, '  packaged-team-key  ');

  assert.equal(resolveXAIAPIKey({ bundledPath }), 'packaged-team-key');
});

test('reports source details and applies environment, stored, team precedence', () => {
  let reads = 0;
  const readFile = (): string => {
    reads += 1;
    return 'team-key';
  };

  assert.deepEqual(
    resolveXAIAPIKeyDetails({
      environmentValue: ' environment-key ',
      storedValue: 'stored-key',
      bundledPath: 'unused',
      readFile,
    }),
    { apiKey: 'environment-key', source: 'environment' },
  );
  assert.deepEqual(
    resolveXAIAPIKeyDetails({
      storedValue: ' stored-key ',
      bundledPath: 'unused',
      readFile,
    }),
    { apiKey: 'stored-key', source: 'stored' },
  );
  assert.deepEqual(
    resolveXAIAPIKeyDetails({ bundledPath: 'unused', readFile }),
    { apiKey: 'team-key', source: 'team' },
  );
  assert.equal(reads, 1);
});

test('treats a missing or empty packaged key as an unconfigured normal build', (t) => {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'fragforge-team-key-'));
  t.after(() => fs.rmSync(directory, { recursive: true, force: true }));
  const bundledPath = path.join(directory, 'xai-api-key');

  assert.equal(resolveXAIAPIKey({ bundledPath }), undefined);
  fs.writeFileSync(bundledPath, '  ');
  assert.equal(resolveXAIAPIKey({ bundledPath }), undefined);
  assert.deepEqual(resolveXAIAPIKeyDetails({ bundledPath }), { source: 'none' });
});

test('rejects unsafe key contents without echoing the value', () => {
  const unsafeValues = [
    'first-line\nsecond-line',
    'prefix\0suffix',
    'x'.repeat(4097),
  ];

  for (const value of unsafeValues) {
    assert.throws(
      () => resolveXAIAPIKey({ environmentValue: value, bundledPath: 'unused' }),
      (err: unknown) => err instanceof Error
        && /single non-empty line no longer than 4096 bytes/.test(err.message)
        && !err.message.includes(value),
    );
  }
});

test('normalizes a pasted key and rejects an empty value', () => {
  assert.equal(normalizeXAIAPIKey('  xai-local-key  '), 'xai-local-key');
  assert.throws(() => normalizeXAIAPIKey('   '), /single non-empty line/);
});

test('captures the canonical xAI key and clears every casing variant', () => {
  const environment: NodeJS.ProcessEnv = {
    KEEP_ME: 'yes',
    xai_api_key: 'lowercase-key',
    XAI_API_KEY: 'canonical-key',
    Xai_Api_Key: 'mixed-key',
  };

  assert.equal(takeXAIAPIKeyFromEnvironment(environment), 'canonical-key');
  assert.deepEqual(environment, { KEEP_ME: 'yes' });
  assert.equal(takeXAIAPIKeyFromEnvironment(environment), undefined);
});

test('reports packaged key read failures without exposing file contents', () => {
  assert.throws(
    () => resolveXAIAPIKey({
      bundledPath: 'team/xai-api-key',
      readFile: () => {
        throw new Error('access denied');
      },
    }),
    /could not read the packaged team xAI API key: access denied/,
  );
});
