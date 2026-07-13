import assert from 'node:assert/strict';
import test from 'node:test';
import { XAISettingsController, type XAISettingsKeyStore } from './xai-settings-controller.ts';
import type { XAISettingsStatus } from './xai-settings-ipc.ts';

const BASE_STATUS: XAISettingsStatus = {
  active: false,
  activeSource: 'none',
  pendingSource: 'none',
  restartRequired: false,
  storageAvailable: true,
  stored: false,
};

interface ControllerDouble {
  controller: XAISettingsController;
  state: { removed: number; restartScheduled: number; saved: string[] };
}

function controllerDouble(options: {
  available?: boolean;
  environmentOverride?: boolean;
  statusError?: boolean;
} = {}): ControllerDouble {
  const state = { removed: 0, restartScheduled: 0, saved: [] as string[] };
  let stored = false;
  const keyStore: XAISettingsKeyStore = {
    isAvailable: async () => options.available ?? true,
    remove: async () => {
      state.removed += 1;
      const existed = stored;
      stored = false;
      return existed;
    },
    save: async (value) => {
      state.saved.push(value);
      stored = true;
    },
  };
  const controller = new XAISettingsController({
    environmentOverride: options.environmentOverride ?? false,
    keyStore,
    readStatus: async (restartRequired) => {
      if (options.statusError) throw new Error('status unavailable');
      return {
        ...BASE_STATUS,
        pendingSource: stored ? 'stored' : 'none',
        restartRequired,
        stored,
      };
    },
    scheduleRestart: () => {
      state.restartScheduled += 1;
      return true;
    },
    testConnection: async () => ({ code: 'valid', message: 'valid', ok: true }),
  });
  return { controller, state };
}

test('saves a normalized key without returning it and marks restart pending', async () => {
  const { controller, state } = controllerDouble();
  const canary = 'xai-controller-canary';
  const response = await controller.handle({ action: 'save', apiKey: `  ${canary}  ` });

  assert.deepEqual(state.saved, [canary]);
  assert.equal(controller.restartRequired, true);
  assert.doesNotMatch(JSON.stringify(response), new RegExp(canary));
  assert.match(JSON.stringify(response), /"restartRequired":true/);
});

test('fails closed for unavailable storage, invalid input, and environment overrides', async () => {
  for (const scenario of [
    { input: 'valid-key', options: { available: false } },
    { input: 'bad\nkey', options: {} },
    { input: 'valid-key', options: { environmentOverride: true } },
  ]) {
    const { controller, state } = controllerDouble(scenario.options);
    const response = await controller.handle({ action: 'save', apiKey: scenario.input });
    assert.match(JSON.stringify(response), /"ok":false/);
    assert.deepEqual(state.saved, []);
    assert.doesNotMatch(JSON.stringify(response), /bad\nkey|valid-key/);
  }
});

test('removal and restart are explicit and markApplied resets the transition', async () => {
  const { controller, state } = controllerDouble();
  assert.deepEqual(await controller.handle({ action: 'restart' }), {
    error: 'No hay cambios de xAI pendientes de aplicar.',
    ok: false,
  });
  await controller.handle({ action: 'save', apiKey: 'saved-key' });
  const removed = await controller.handle({ action: 'remove' });
  assert.equal(state.removed, 1);
  assert.match(JSON.stringify(removed), /"restartRequired":true/);

  assert.deepEqual(await controller.handle({ action: 'restart' }), { ok: true });
  assert.equal(state.restartScheduled, 1);
  controller.markApplied();
  assert.equal(controller.restartRequired, false);
});

test('status and connection test never require a stored key read', async () => {
  const { controller, state } = controllerDouble();
  const status = await controller.handle({ action: 'status' });
  const tested = await controller.handle({ action: 'test', apiKey: 'candidate' });
  assert.match(JSON.stringify(status), /"active":false/);
  assert.deepEqual(tested, { code: 'valid', message: 'valid', ok: true });
  assert.deepEqual(state.saved, []);
  assert.equal(state.removed, 0);
});

test('does not report a durable save or removal as failed when only status refresh fails', async () => {
  const { controller, state } = controllerDouble({ statusError: true });

  assert.deepEqual(await controller.handle({ action: 'save', apiKey: 'durable-key' }), { ok: true });
  assert.deepEqual(state.saved, ['durable-key']);
  assert.equal(controller.restartRequired, true);

  assert.deepEqual(await controller.handle({ action: 'remove' }), { ok: true });
  assert.equal(state.removed, 1);
  assert.equal(controller.restartRequired, true);
});
