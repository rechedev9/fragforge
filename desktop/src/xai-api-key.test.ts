import test from 'node:test';
import assert from 'node:assert/strict';
import {
  normalizeXAIAPIKey,
  resolveXAIAPIKeyDetails,
  takeXAIAPIKeyFromEnvironment,
} from './xai-api-key.ts';

test('prefers an explicit environment xAI API key over the stored value', () => {
  const resolved = resolveXAIAPIKeyDetails({
    environmentValue: '  environment-key  ',
    storedValue: 'stored-key',
  });

  assert.deepEqual(resolved, { apiKey: 'environment-key', source: 'environment' });
});

test('reports source details for environment, stored, and unconfigured states', () => {
  assert.deepEqual(
    resolveXAIAPIKeyDetails({
      environmentValue: ' environment-key ',
      storedValue: 'stored-key',
    }),
    { apiKey: 'environment-key', source: 'environment' },
  );
  assert.deepEqual(
    resolveXAIAPIKeyDetails({
      storedValue: ' stored-key ',
    }),
    { apiKey: 'stored-key', source: 'stored' },
  );
  assert.deepEqual(resolveXAIAPIKeyDetails({}), { source: 'none' });
});

test('rejects unsafe key contents without echoing the value', () => {
  const unsafeValues = [
    'first-line\nsecond-line',
    'prefix\0suffix',
    'x'.repeat(4097),
  ];

  for (const value of unsafeValues) {
    assert.throws(
      () => resolveXAIAPIKeyDetails({ environmentValue: value }),
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
