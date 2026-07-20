import assert from 'node:assert/strict';
import test from 'node:test';
import { isCurrentStreamEditorLoad, nextStreamEditorLoad, type StreamEditorLoad } from './stream-editor-load.ts';

test('only the latest selected stream job may finish loading the editor', () => {
  const idle: StreamEditorLoad = { generation: 0, jobId: '' };
  const first = nextStreamEditorLoad(idle, 'job-a');
  const second = nextStreamEditorLoad(first, 'job-b');

  assert.equal(isCurrentStreamEditorLoad(first, second), false);
  assert.equal(isCurrentStreamEditorLoad(second, second), true);
  assert.equal(isCurrentStreamEditorLoad({ ...second, jobId: 'job-a' }, second), false);
});

test('a late autosave response from the previous job is stale', () => {
  const autosaveStarted = { generation: 4, jobId: 'job-a' };
  const selectedNow = { generation: 5, jobId: 'job-b' };

  assert.equal(isCurrentStreamEditorLoad(autosaveStarted, selectedNow), false);
});
