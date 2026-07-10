import test from 'node:test';
import assert from 'node:assert/strict';
import { PINNED_HLAE_TOOL } from './hlae-tool.ts';

test('pins the official HLAE release', () => {
  assert.deepEqual(PINNED_HLAE_TOOL, {
    version: '2.191.0',
    url: 'https://github.com/advancedfx/advancedfx/releases/download/v2.191.0/hlae_2_191_0.zip',
    sha256: '78efa377a2bac9522c3771a79c2503fec57e106432fc11d32244fe25b7c5b6cc',
    kind: 'zip',
    exeRel: 'HLAE.exe',
    timeoutMs: 90_000,
  });
});
