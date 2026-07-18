import assert from 'node:assert/strict';
import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import * as os from 'node:os';
import * as path from 'node:path';
import test from 'node:test';
import { AssistantHistoryStore } from './history.ts';

test('persists a bounded local assistant transcript and thread id', async (t) => {
  const directory = await mkdtemp(path.join(os.tmpdir(), 'fragforge-assistant-history-'));
  t.after(async () => rm(directory, { force: true, recursive: true }));
  const filePath = path.join(directory, 'history.json');
  const store = new AssistantHistoryStore(filePath);

  await store.save({
    messages: [{
      createdAt: '2026-01-01T00:00:00.000Z',
      id: 'message-1',
      role: 'assistant',
      content: 'respuesta parcial',
      streaming: true,
    }],
    threadId: 'thread-1',
  });

  assert.deepEqual(await store.load(), {
    messages: [{
      createdAt: '2026-01-01T00:00:00.000Z',
      id: 'message-1',
      role: 'assistant',
      content: 'respuesta parcial',
    }],
    threadId: 'thread-1',
  });
  const stored = JSON.parse(await readFile(filePath, 'utf8')) as { version: number };
  assert.equal(stored.version, 1);
});

test('ignores corrupted and invalid persisted assistant history', async (t) => {
  const directory = await mkdtemp(path.join(os.tmpdir(), 'fragforge-assistant-history-'));
  t.after(async () => rm(directory, { force: true, recursive: true }));
  const filePath = path.join(directory, 'history.json');
  await writeFile(filePath, JSON.stringify({ version: 1, messages: [{ text: 'missing fields' }] }));

  const store = new AssistantHistoryStore(filePath);

  assert.deepEqual(await store.load(), { messages: [] });
  await store.clear();
  assert.deepEqual(await store.load(), { messages: [] });
});
