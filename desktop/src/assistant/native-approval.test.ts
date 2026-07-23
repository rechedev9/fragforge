import assert from 'node:assert/strict';
import test from 'node:test';
import {
  NativeApprovalGate,
  nativeApprovalDetail,
  type NativeApprovalDecision,
  type NativeApprovalPrompt,
} from './native-approval.ts';

const prompt: NativeApprovalPrompt = {
  actionId: 'action_known_to_renderer',
  fields: [{ label: 'Destino', value: 'job_123' }],
  operation: 'jobs.delete',
  risk: 'destructive',
  summary: 'Eliminar el trabajo y sus artefactos.',
  title: 'Eliminar trabajo',
};

test('a renderer-known action ID cannot approve without the native affirmative result', async () => {
  let approved = false;
  const gate = new NativeApprovalGate(async () => 'cancel');

  await gate.request(prompt.actionId, {
    approvalPrompt: () => prompt,
    approve: async () => { approved = true; },
  });

  assert.equal(approved, false);
});

test('the native affirmative result consumes approval through the main-owned target', async () => {
  const approvals: string[] = [];
  const gate = new NativeApprovalGate(async () => 'approve');

  await gate.request(prompt.actionId, {
    approvalPrompt: () => prompt,
    approve: async (actionId) => { approvals.push(actionId); },
  });

  assert.deepEqual(approvals, [prompt.actionId]);
});

test('rejects concurrent confirmation requests for the same pending action', async () => {
  let resolveDecision: ((decision: NativeApprovalDecision) => void) | undefined;
  const gate = new NativeApprovalGate(() => new Promise((resolve) => {
    resolveDecision = resolve;
  }));
  const target = {
    approvalPrompt: () => prompt,
    approve: async () => {},
  };
  const first = gate.request(prompt.actionId, target);

  await assert.rejects(
    gate.request(prompt.actionId, target),
    /already open/,
  );
  resolveDecision?.('cancel');
  await first;
});

test('renders the exact risk, operation, and preview fields for native review', () => {
  assert.equal(
    nativeApprovalDetail(prompt),
    [
      'Riesgo: acción destructiva',
      'Operación: jobs.delete',
      '',
      'Eliminar el trabajo y sus artefactos.',
      '',
      'Destino: job_123',
      '',
      'Esta aprobación sólo puede concederse desde este diálogo nativo de FragForge Studio.',
    ].join('\n'),
  );
});
