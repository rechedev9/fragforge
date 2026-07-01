import { test } from 'node:test';
import assert from 'node:assert/strict';
import { toJobDto, STATE_FROM_GO, GO_STATUS } from './jobDto.ts';

test('toJobDto uses demo_id as id and demo storage key as demo_path', () => {
  const dto = toJobDto({ demo_id: 'd1', target_steamid: '', rules: null, demos: { storage_key: 'demos/u/d1.dem', sha256: 'ab' } });
  assert.equal(dto.id, 'd1');
  assert.equal(dto.demo_path, 'demos/u/d1.dem');
  assert.equal(dto.demo_sha256, 'ab');
  assert.deepEqual(dto.rules, {});
});

test('status int/text mapping round-trips', () => {
  assert.equal(STATE_FROM_GO[GO_STATUS.scanned], 'scanned');
  assert.equal(STATE_FROM_GO[GO_STATUS.failed], 'failed');
});
