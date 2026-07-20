import assert from 'node:assert/strict';
import test from 'node:test';
import { loadStreamDraft, reconcileStreamDraftAfterSave, recoverableStreamJobs, saveStreamDraft, selectStreamDraftPlan, streamEditPlanFingerprint } from './stream-draft.ts';
import type { StreamEditPlan } from './api/streams.ts';

test('stream drafts round-trip through durable browser storage', () => {
  const values = new Map<string, string>();
  const storage = {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => { values.set(key, value); },
  };
  const plan: StreamEditPlan = { schema_version: '1.1', variant: 'streamer-fullframe-nocam', clips: [] };
  saveStreamDraft(storage, 'job-1', plan, '2026-04-01T12:00:00Z');
  assert.deepEqual(loadStreamDraft(storage, 'job-1'), {
    savedAt: '2026-04-01T12:00:00Z',
    basePlanFingerprint: streamEditPlanFingerprint(plan),
    plan,
  });
});

test('a confirmed save clears only the exact matching browser draft', () => {
  const values = new Map<string, string>();
  const storage = {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => { values.set(key, value); },
    removeItem: (key: string) => { values.delete(key); },
  };
  const first: StreamEditPlan = { schema_version: '1.1', variant: 'streamer-fullframe-nocam', clips: [] };
  const latest: StreamEditPlan = { ...first, clips: [{ id: 'latest', start_seconds: 1, end_seconds: 2 }] };
  const initial: StreamEditPlan = { ...first, clips: [{ id: 'initial', start_seconds: 0, end_seconds: 1 }] };
  saveStreamDraft(storage, 'job-1', latest, '2026-04-01T12:00:00Z', streamEditPlanFingerprint(initial), { editorSessionId: 'editor-1', revision: 2 });
  reconcileStreamDraftAfterSave(storage, 'job-1', initial, first, { editorSessionId: 'editor-1', revision: 1 });
  const pending = loadStreamDraft(storage, 'job-1');
  assert.deepEqual(pending?.plan, latest);
  assert.deepEqual(selectStreamDraftPlan(pending, first), latest);
  assert.equal(selectStreamDraftPlan(pending, initial), null);
  reconcileStreamDraftAfterSave(storage, 'job-1', latest, latest, { editorSessionId: 'editor-1', revision: 2 });
  assert.equal(loadStreamDraft(storage, 'job-1'), null);
});

test('a canonical server response rebases a newer draft across omitted defaults', () => {
  const values = new Map<string, string>();
  const storage = {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => { values.set(key, value); },
    removeItem: (key: string) => { values.delete(key); },
  };
  const submitted: StreamEditPlan = {
    schema_version: '1.1',
    variant: 'streamer-vertical-stack-40-60',
    face_crop_reviewed: false,
    clips: [{ id: 'clip-1', start_seconds: 0, end_seconds: 2 }],
  };
  const serverSaved: StreamEditPlan = {
    schema_version: '1.1',
    variant: 'streamer-vertical-stack-40-60',
    clips: [{ id: 'clip-1', start_seconds: 0, end_seconds: 2 }],
  };
  const newer = { ...submitted, clips: [{ id: 'clip-1', start_seconds: 0, end_seconds: 3 }] };
  saveStreamDraft(storage, 'job-1', newer, '2026-04-01T12:00:00Z', streamEditPlanFingerprint(submitted), { editorSessionId: 'editor-1', revision: 2 });

  reconcileStreamDraftAfterSave(storage, 'job-1', submitted, serverSaved, { editorSessionId: 'editor-1', revision: 1 });

  const pending = loadStreamDraft(storage, 'job-1');
  assert.deepEqual(pending?.plan, newer);
  assert.deepEqual(selectStreamDraftPlan(pending, serverSaved), newer);
});

test('a canonical server response clears the exact submitted draft', () => {
  const values = new Map<string, string>();
  const storage = {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => { values.set(key, value); },
    removeItem: (key: string) => { values.delete(key); },
  };
  const submitted: StreamEditPlan = {
    schema_version: '1.1',
    variant: 'streamer-vertical-stack-40-60',
    face_crop_reviewed: false,
    clips: [],
  };
  const serverSaved: StreamEditPlan = {
    schema_version: '1.1',
    variant: 'streamer-vertical-stack-40-60',
    clips: [],
  };
  saveStreamDraft(storage, 'job-1', submitted, undefined, undefined, { editorSessionId: 'editor-1', revision: 1 });

  reconcileStreamDraftAfterSave(storage, 'job-1', submitted, serverSaved, { editorSessionId: 'editor-1', revision: 1 });

  assert.equal(loadStreamDraft(storage, 'job-1'), null);
});

test('a conflicting draft from another editor session is not rebased or restored', () => {
  const values = new Map<string, string>();
  const storage = {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => { values.set(key, value); },
    removeItem: (key: string) => { values.delete(key); },
  };
  const previousServer: StreamEditPlan = { schema_version: '1.1', variant: 'streamer-fullframe-nocam', clips: [] };
  const submitted: StreamEditPlan = { ...previousServer, clips: [{ id: 'submitted', start_seconds: 0, end_seconds: 2 }] };
  const saved: StreamEditPlan = { ...submitted, face_crop_reviewed: false };
  const conflicting: StreamEditPlan = { ...previousServer, clips: [{ id: 'other-tab', start_seconds: 5, end_seconds: 8 }] };
  saveStreamDraft(storage, 'job-1', conflicting, undefined, streamEditPlanFingerprint(previousServer), { editorSessionId: 'other-editor', revision: 9 });

  reconcileStreamDraftAfterSave(storage, 'job-1', submitted, saved, { editorSessionId: 'editor-1', revision: 1 });

  const pending = loadStreamDraft(storage, 'job-1');
  assert.deepEqual(pending?.plan, conflicting);
  assert.equal(pending?.basePlanFingerprint, streamEditPlanFingerprint(previousServer));
  assert.equal(selectStreamDraftPlan(pending, saved), null);
});

test('recoverable stream jobs exclude failures and put newest first', () => {
  const jobs = recoverableStreamJobs([
    { id: 'old', status: 'ready', created_at: '2026-01-01T00:00:00Z', updated_at: '2026-04-01T00:00:00Z' },
    { id: 'failed', status: 'failed', created_at: '2026-03-01T00:00:00Z' },
    { id: 'new', status: 'rendered', created_at: '2026-02-01T00:00:00Z' },
  ]);
  assert.deepEqual(jobs.map((job) => job.id), ['old', 'new']);
});

test('stream draft storage failures do not break the editor', () => {
  const plan: StreamEditPlan = { schema_version: '1.1', variant: 'streamer-fullframe-nocam', clips: [] };
  assert.doesNotThrow(() => saveStreamDraft({ setItem: () => { throw new Error('quota'); } }, 'job-1', plan));
  assert.equal(loadStreamDraft({ getItem: () => { throw new Error('blocked'); } }, 'job-1'), null);
});
