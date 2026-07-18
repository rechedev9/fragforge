import assert from 'node:assert/strict';
import test from 'node:test';
import { parseAssistantRequest } from './assistant-ipc.ts';

test('parses only bounded assistant IPC requests', () => {
  assert.deepEqual(parseAssistantRequest({ action: 'status' }), { action: 'status' });
  assert.deepEqual(parseAssistantRequest({ action: 'cancel' }), { action: 'cancel' });
  assert.deepEqual(parseAssistantRequest({ action: 'new' }), { action: 'new' });
  assert.deepEqual(parseAssistantRequest({ action: 'clear' }), { action: 'clear' });
  assert.deepEqual(parseAssistantRequest({ action: 'approve', actionId: 'action_123' }), {
    action: 'approve',
    actionId: 'action_123',
  });
  assert.deepEqual(parseAssistantRequest({
    action: 'send',
    context: { jobId: 'job-123', kind: 'demo', label: 'Demo actual', pathname: '/matches/job-123' },
    message: '  Resume las kills  ',
  }), {
    action: 'send',
    context: { jobId: 'job-123', kind: 'demo', label: 'Demo actual', pathname: '/matches/job-123' },
    message: 'Resume las kills',
  });
});

test('rejects injected, oversized, and malformed assistant IPC requests', () => {
  for (const invalid of [
    null,
    {},
    { action: 'shell', command: 'whoami' },
    { action: 'status', extra: true },
    { action: 'approve', actionId: '../escape' },
    { action: 'approve', actionId: 'x'.repeat(129) },
    { action: 'send', context: { pathname: '/matches' } },
    { action: 'send', context: { kind: 'none', label: 'Studio', pathname: 'https://example.com' }, message: 'hola' },
    { action: 'send', context: { kind: 'none', label: 'Studio', pathname: '/matches', ignored: true }, message: 'hola' },
    { action: 'send', context: { kind: 'none', label: 'Studio', pathname: '/matches' }, message: ' ' },
    { action: 'send', context: { kind: 'none', label: 'Studio', pathname: '/matches' }, message: 'x'.repeat(8_001) },
    { action: 'send', context: { kind: 'none', label: 'Studio', pathname: '/matches' }, message: 'abre C:\\Users\\me\\demo.dem' },
    { action: 'send', context: { kind: 'none', label: 'Studio', pathname: '/matches' }, message: 'mira https://example.com/video' },
    { action: 'send', context: { kind: 'none', label: 'Studio', pathname: '/matches' }, message: 'token: secret-value' },
    { action: 'send', context: { kind: 'none', label: 'Studio', pathname: '/C:/Windows' }, message: 'hola' },
  ]) {
    assert.throws(() => parseAssistantRequest(invalid), /invalid assistant request/);
  }
});
