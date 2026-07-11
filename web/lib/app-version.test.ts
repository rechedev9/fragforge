import test from 'node:test';
import assert from 'node:assert/strict';
import { formatAppVersion } from './app-version.ts';

test('formats the desktop package version for the sidebar', () => {
  assert.equal(formatAppVersion('1.0.5'), 'v1.0.5');
});

test('does not duplicate an existing version prefix', () => {
  assert.equal(formatAppVersion('v1.0.5'), 'v1.0.5');
});

test('hides the version outside a versioned desktop build', () => {
  assert.equal(formatAppVersion(undefined), null);
  assert.equal(formatAppVersion('  '), null);
});
