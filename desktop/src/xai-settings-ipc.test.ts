import assert from 'node:assert/strict';
import test from 'node:test';
import {
  isTrustedSettingsSender,
  parseXAISettingsRequest,
  XAI_SETTINGS_ACTION,
} from './xai-settings-ipc.ts';

test('parses only the narrow xAI settings action shapes', () => {
  assert.deepEqual(parseXAISettingsRequest({ action: 'app-info' }), { action: 'app-info' });
  assert.deepEqual(parseXAISettingsRequest({ action: 'status' }), { action: 'status' });
  assert.deepEqual(parseXAISettingsRequest({ action: 'save', apiKey: 'secret' }), {
    action: 'save',
    apiKey: 'secret',
  });
  assert.deepEqual(parseXAISettingsRequest({ action: 'test', apiKey: 'secret' }), {
    action: 'test',
    apiKey: 'secret',
  });
  const maximum = parseXAISettingsRequest({ action: 'test', apiKey: 'x'.repeat(4096) });
  assert.equal(maximum.action, XAI_SETTINGS_ACTION.test);
  if (maximum.action !== XAI_SETTINGS_ACTION.test) throw new Error('expected a test request');
  assert.equal(maximum.apiKey.length, 4096);
  assert.deepEqual(parseXAISettingsRequest({ action: 'remove' }), { action: 'remove' });
  assert.deepEqual(parseXAISettingsRequest({ action: 'restart' }), { action: 'restart' });

  for (const invalid of [
    null,
    {},
    { action: 'read-key' },
		{ action: 'status', extra: true },
    { action: 'app-info', extra: true },
    { action: 'save' },
    { action: 'save', apiKey: 42 },
    { action: 'save', apiKey: 'x'.repeat(4097) },
    Object.create({ action: XAI_SETTINGS_ACTION.status }),
  ]) {
    assert.throws(() => parseXAISettingsRequest(invalid), /invalid xAI settings request/);
  }
});

test('trusts only the active top-level web frame and exact origin', () => {
  const trusted = {
    expectedOrigin: 'http://127.0.0.1:3010',
    expectedWebContentsID: 7,
    isMainFrame: true,
    senderURL: 'http://127.0.0.1:3010/settings',
    senderWebContentsID: 7,
  };
  assert.equal(isTrustedSettingsSender(trusted), true);
  assert.equal(isTrustedSettingsSender({ ...trusted, isMainFrame: false }), false);
  assert.equal(isTrustedSettingsSender({ ...trusted, senderWebContentsID: 8 }), false);
  assert.equal(isTrustedSettingsSender({ ...trusted, senderURL: 'http://127.0.0.1:8080/settings' }), false);
  assert.equal(isTrustedSettingsSender({ ...trusted, senderURL: 'https://example.com/settings' }), false);
  assert.equal(isTrustedSettingsSender({ ...trusted, senderURL: 'not a URL' }), false);
  assert.equal(isTrustedSettingsSender({ ...trusted, expectedOrigin: null }), false);
});
