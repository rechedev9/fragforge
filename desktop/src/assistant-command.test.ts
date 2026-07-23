import assert from 'node:assert/strict';
import test from 'node:test';
import type { AssistantSnapshot } from './assistant-ipc.ts';
import { dispatchAssistantRequest } from './assistant-command.ts';

const snapshot: AssistantSnapshot = {
  account: { planType: 'plus', status: 'signed-in' },
  availability: 'ready',
  busy: false,
  messages: [],
  pendingActions: [],
  revision: 7,
};

test('returns a terminal command failure when a send request is rejected before the controller', async () => {
  let controllerRequested = false;
  const result = await dispatchAssistantRequest({
    action: 'send',
    context: { kind: 'none', label: 'Studio', pathname: '/settings' },
    message: 'mira https://example.com/video',
  }, () => {
    controllerRequested = true;
    throw new Error('controller must not be created for invalid input');
  });

  assert.equal(controllerRequested, false);
  assert.deepEqual(result, {
    error: 'Solicitud del asistente no válida.',
    ok: false,
  });
});

test('returns the current snapshot when a controller command fails', async () => {
  const result = await dispatchAssistantRequest({
    action: 'send',
    context: { kind: 'none', label: 'Studio', pathname: '/settings' },
    message: 'continúa',
  }, () => ({
    approve: async () => {},
    cancel: async () => {},
    clearHistory: async () => {},
    login: async () => {},
    logout: async () => {},
    newConversation: async () => {},
    reject: () => {},
    send: async () => {
      throw new Error('turn failed');
    },
    snapshot: () => snapshot,
    status: async () => snapshot,
    wake: async () => {},
  }));

  assert.deepEqual(result, {
    error: 'No se pudo completar la operación del asistente.',
    ok: false,
    snapshot,
  });
});

test('wraps status in the same terminal command result as every mutation', async () => {
  const result = await dispatchAssistantRequest({ action: 'status' }, () => ({
    approve: async () => {},
    cancel: async () => {},
    clearHistory: async () => {},
    login: async () => {},
    logout: async () => {},
    newConversation: async () => {},
    reject: () => {},
    send: async () => {},
    snapshot: () => snapshot,
    status: async () => snapshot,
    wake: async () => {},
  }));

  assert.deepEqual(result, { ok: true, snapshot });
});

test('still returns a terminal failure when taking the fallback snapshot also fails', async () => {
  const result = await dispatchAssistantRequest({
    action: 'send',
    context: { kind: 'none', label: 'Studio', pathname: '/settings' },
    message: 'continúa',
  }, () => ({
    approve: async () => {},
    cancel: async () => {},
    clearHistory: async () => {},
    login: async () => {},
    logout: async () => {},
    newConversation: async () => {},
    reject: () => {},
    send: async () => {
      throw new Error('turn failed');
    },
    snapshot: () => {
      throw new Error('snapshot failed');
    },
    status: async () => snapshot,
    wake: async () => {},
  }));

  assert.deepEqual(result, {
    error: 'No se pudo completar la operación del asistente.',
    ok: false,
  });
});
