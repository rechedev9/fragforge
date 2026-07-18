import assert from 'node:assert/strict';
import test from 'node:test';
import {
  applyAssistantEvent,
  assistantContextFromPathname,
  ASSISTANT_AVAILABILITY,
  getFragforgeAssistantBridge,
  initialAssistantSnapshot,
  type FragforgeAssistantBridge,
} from './assistant.ts';

function bridge(): FragforgeAssistantBridge {
  return {
    approve: async () => {},
    cancel: async () => {},
    clearHistory: async () => {},
    newConversation: async () => {},
    reject: async () => {},
    send: async () => {},
    status: async () => initialAssistantSnapshot(ASSISTANT_AVAILABILITY.ready),
    subscribe: () => () => {},
  };
}

test('returns null outside Electron and rejects a partial preload surface', () => {
  assert.equal(getFragforgeAssistantBridge({}), null);
  assert.equal(getFragforgeAssistantBridge({ fragforgeAssistant: { status: async () => {} } }), null);
});

test('returns the full narrow assistant bridge', async () => {
  const expected = bridge();
  const got = getFragforgeAssistantBridge({ fragforgeAssistant: expected });

  assert.equal(got, expected);
  if (got === null) throw new Error('expected assistant bridge');
  assert.equal((await got.status()).availability, ASSISTANT_AVAILABILITY.ready);
});

test('derives opaque render and demo context from Studio routes', () => {
  assert.deepEqual(assistantContextFromPathname('/matches/job-12'), {
    kind: 'demo',
    jobId: 'job-12',
    label: 'Demo actual',
    pathname: '/matches/job-12',
  });
  assert.deepEqual(assistantContextFromPathname('/streams/stream-3/renders/short-9x16'), {
    kind: 'render',
    label: 'Render de stream',
    pathname: '/streams/stream-3/renders/short-9x16',
    streamJobId: 'stream-3',
    variant: 'short-9x16',
  });
  assert.deepEqual(assistantContextFromPathname('/settings'), {
    kind: 'none',
    label: 'Ajustes',
    pathname: '/settings',
  });
});

test('merges streaming deltas and action updates without mutating the prior snapshot', () => {
  const before = initialAssistantSnapshot(ASSISTANT_AVAILABILITY.ready);
  const streamed = applyAssistantEvent(before, {
    createdAt: '2026-07-18T08:00:00.000Z',
    delta: 'Hola',
    messageId: 'message-1',
    type: 'message_delta',
  });
  const completed = applyAssistantEvent(streamed, { messageId: 'message-1', type: 'message_complete' });
  const withAction = applyAssistantEvent(completed, {
    action: {
      id: 'action-1',
      operation: 'renders.start',
      risk: 'costly',
      title: 'Renderizar reel',
    },
    type: 'action',
  });

  assert.equal(before.messages.length, 0);
  assert.equal(streamed.messages[0]?.content, 'Hola');
  assert.equal(streamed.messages[0]?.streaming, true);
  assert.equal(completed.messages[0]?.streaming, false);
  assert.equal(withAction.pendingActions[0]?.id, 'action-1');
});
