import test from 'node:test';
import assert from 'node:assert/strict';
import {
  getDesktopSettingsBridge,
  XAI_KEY_SOURCES,
  type DesktopSettingsBridge,
  type XAISettingsStatus,
} from './desktop-settings.ts';

const STATUS: XAISettingsStatus = {
  storageAvailable: true,
  stored: false,
  active: false,
  activeSource: XAI_KEY_SOURCES.none,
  pendingSource: XAI_KEY_SOURCES.none,
  restartRequired: false,
};

function bridge(): DesktopSettingsBridge {
  return {
    getAppInfo: async () => ({ version: '2.2.9', build: 'production', electronVersion: '37.0.0', chromiumVersion: '138.0.0' }),
    getXAIStatus: async () => STATUS,
    saveXAIKey: async () => ({ ok: true, status: STATUS }),
    removeXAIKey: async () => ({ ok: true, status: STATUS }),
    testXAIKey: async () => ({ ok: true, code: 'ok', message: 'Conexión correcta.' }),
    restartStudio: async () => ({ ok: true }),
    getMCPConfig: async () => ({
      launcherPath: 'C:\\Programs\\FragForge Studio\\fragforge-mcp.cmd',
      launcherInstalled: true,
      claudeCommand: 'claude mcp add …',
      mcpServersJSON: '{}',
    }),
  };
}

test('returns null outside Electron instead of falling back to HTTP', () => {
  assert.equal(getDesktopSettingsBridge({}), null);
  assert.equal(getDesktopSettingsBridge(null), null);
  assert.equal(getDesktopSettingsBridge({ fragforgeSettings: {} }), null);
});

test('rejects a partial preload surface', () => {
  const partial = bridge();
  const incomplete = {
    getXAIStatus: partial.getXAIStatus,
    saveXAIKey: partial.saveXAIKey,
    removeXAIKey: partial.removeXAIKey,
    testXAIKey: partial.testXAIKey,
  };

  assert.equal(getDesktopSettingsBridge({ fragforgeSettings: incomplete }), null);
});

test('returns the complete narrow preload bridge', async () => {
  const expected = bridge();
  const got = getDesktopSettingsBridge({ fragforgeSettings: expected });

  assert.equal(got, expected);
  if (got === null) throw new Error('expected the desktop settings bridge');
  assert.deepEqual(await got.getXAIStatus(), STATUS);
  assert.equal((await got.getAppInfo()).version, '2.2.9');
  assert.deepEqual(await got.testXAIKey('candidate'), {
    ok: true,
    code: 'ok',
    message: 'Conexión correcta.',
  });
});
