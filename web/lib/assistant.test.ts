import assert from 'node:assert/strict';
import test from 'node:test';
import {
  applyAssistantEvent,
  beginAssistantCommand,
  assistantContextFromPathname,
  ASSISTANT_AVAILABILITY,
  finishAssistantCommand,
  getFragforgeAssistantBridge,
  initialAssistantSnapshot,
  parseAssistantCommandResult,
  parseAssistantSnapshotEvent,
  type FragforgeAssistantBridge,
} from './assistant.ts';

function bridge(): FragforgeAssistantBridge {
  const snapshot = initialAssistantSnapshot(ASSISTANT_AVAILABILITY.ready);
  const command = async () => ({ ok: true as const, snapshot });
  return {
    approve: command,
    cancel: command,
    clearHistory: command,
    login: command,
    logout: command,
    newConversation: command,
    reject: command,
    send: command,
    status: command,
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
    const status = await got.status();
    assert.equal(status.ok, true);
    assert.equal(status.snapshot?.availability, ASSISTANT_AVAILABILITY.ready);
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

test('finishes a rejected command without leaving the composer pending', () => {
  const initial = {
    commandPendingCount: 0,
    snapshot: initialAssistantSnapshot(ASSISTANT_AVAILABILITY.ready),
  };
  const pending = beginAssistantCommand(initial);
  const completed = finishAssistantCommand(pending, {
    error: 'Solicitud del asistente no válida.',
    ok: false,
  });

  assert.equal(pending.commandPendingCount, 1);
  assert.equal(completed.commandPendingCount, 0);
  assert.equal(completed.controlError, 'Solicitud del asistente no válida.');
  assert.equal(completed.snapshot.busy, false);
});

test('ignores a stale command snapshot after a newer streaming event', () => {
  const initial = initialAssistantSnapshot(ASSISTANT_AVAILABILITY.ready);
  const current = applyAssistantEvent(initial, {
    snapshot: { ...initial, busy: true, revision: 4 },
    type: 'snapshot',
  });
  const completed = finishAssistantCommand({ commandPendingCount: 1, snapshot: current }, {
    ok: true,
    snapshot: { ...initial, busy: false, revision: 3 },
  });

  assert.equal(completed.commandPendingCount, 0);
  assert.equal(completed.snapshot.busy, true);
  assert.equal(completed.snapshot.revision, 4);
});

test('keeps a granular event when a command response has the same revision', () => {
  const initial = { ...initialAssistantSnapshot(ASSISTANT_AVAILABILITY.ready), revision: 4 };
  const streamed = applyAssistantEvent(initial, {
    createdAt: '2026-07-18T08:00:00.000Z',
    delta: 'Nuevo',
    messageId: 'message-1',
    type: 'message_delta',
  });
  const completed = finishAssistantCommand({ commandPendingCount: 1, snapshot: streamed }, {
    ok: true,
    snapshot: initial,
  });

  assert.equal(completed.snapshot.messages[0]?.content, 'Nuevo');
  assert.equal(completed.snapshot.revision, 4);
});

test('accepts revision zero as the first remote snapshot', () => {
  const local = initialAssistantSnapshot();
  const remote = {
    ...initialAssistantSnapshot(ASSISTANT_AVAILABILITY.ready),
    revision: 0,
  };

  const applied = applyAssistantEvent(local, { snapshot: remote, type: 'snapshot' });

  assert.equal(applied.availability, ASSISTANT_AVAILABILITY.ready);
  assert.equal(applied.revision, 0);
});

test('rejects malformed command responses and snapshot events at the renderer boundary', () => {
  assert.throws(() => parseAssistantCommandResult(null), /invalid assistant command result/);
  assert.throws(() => parseAssistantCommandResult({ ok: true, snapshot: { revision: 1 } }), /invalid assistant snapshot/);
  assert.throws(() => parseAssistantSnapshotEvent({ type: 'snapshot', snapshot: { revision: 1 } }), /invalid assistant snapshot/);

  const snapshot = { ...initialAssistantSnapshot(ASSISTANT_AVAILABILITY.ready), revision: 0 };
  assert.deepEqual(parseAssistantCommandResult({ ok: true, snapshot }), { ok: true, snapshot });
  assert.deepEqual(parseAssistantSnapshotEvent({ type: 'snapshot', snapshot }), { type: 'snapshot', snapshot });
});
