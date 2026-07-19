import test from 'node:test';
import assert from 'node:assert/strict';
import * as fs from 'node:fs';
import { PINNED_HLAE_TOOL } from './hlae-tool.ts';

test('pins the official HLAE release', () => {
  assert.deepEqual(PINNED_HLAE_TOOL, {
    version: '2.191.1',
    archiveName: 'hlae_2_191_1.zip',
    url: 'https://github.com/advancedfx/advancedfx/releases/download/v2.191.1/hlae_2_191_1.zip',
    sha256: '307ba9170b151a7df9b7e5604b335c2d8b8df5bf5cb8d6700ae3fd01069da514',
    kind: 'zip',
    exeRel: 'HLAE.exe',
    timeoutMs: 90_000,
  });
});

test('runtime and packaging use the same HLAE pin', () => {
  const manifest = JSON.parse(
    fs.readFileSync(new URL('./hlae-tool.json', import.meta.url), 'utf8'),
  );
  const { kind, ...runtimeManifest } = PINNED_HLAE_TOOL;

  assert.equal(kind, 'zip');
  assert.deepEqual(runtimeManifest, manifest);
});
