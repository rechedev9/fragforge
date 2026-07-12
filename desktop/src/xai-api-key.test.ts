import test from 'node:test';
import assert from 'node:assert/strict';
import * as fs from 'node:fs';
import * as os from 'node:os';
import * as path from 'node:path';
import { resolveXAIAPIKey } from './xai-api-key.ts';

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

test('treats a missing or empty packaged key as an unconfigured normal build', (t) => {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'fragforge-team-key-'));
  t.after(() => fs.rmSync(directory, { recursive: true, force: true }));
  const bundledPath = path.join(directory, 'xai-api-key');

  assert.equal(resolveXAIAPIKey({ bundledPath }), undefined);
  fs.writeFileSync(bundledPath, '  ');
  assert.equal(resolveXAIAPIKey({ bundledPath }), undefined);
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
