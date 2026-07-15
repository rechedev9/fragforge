import test from 'node:test';
import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { existsSync, mkdtempSync, readFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { stageBundledHLAE, verifyBundledHLAE } from './hlae-bundle.mjs';

function fixtureSpec(bytes) {
  return {
    version: 'test',
    archiveName: 'hlae_test.zip',
    url: 'https://example.invalid/hlae_test.zip',
    sha256: createHash('sha256').update(bytes).digest('hex'),
    exeRel: 'HLAE.exe',
  };
}

test('stages and verifies the pinned HLAE archive', async (t) => {
  const directory = mkdtempSync(join(tmpdir(), 'fragforge-hlae-bundle-'));
  t.after(() => rmSync(directory, { recursive: true, force: true }));
  const bytes = Buffer.from('official release fixture');
  const spec = fixtureSpec(bytes);

  const archive = await stageBundledHLAE({
    destinationDirectory: directory,
    spec,
    fetchImpl: async () => new Response(bytes, { status: 200 }),
  });

  assert.equal(archive, join(directory, spec.archiveName));
  assert.deepEqual(readFileSync(archive), bytes);
  assert.equal(verifyBundledHLAE(archive, spec), archive);
});

test('rejects a corrupt archive without publishing it', async (t) => {
  const directory = mkdtempSync(join(tmpdir(), 'fragforge-hlae-bundle-'));
  t.after(() => rmSync(directory, { recursive: true, force: true }));
  const spec = fixtureSpec(Buffer.from('expected'));

  await assert.rejects(
    stageBundledHLAE({
      destinationDirectory: directory,
      spec,
      fetchImpl: async () => new Response('corrupt', { status: 200 }),
    }),
    /sha256 mismatch/,
  );

  assert.equal(existsSync(join(directory, spec.archiveName)), false);
  assert.equal(existsSync(join(directory, `${spec.archiveName}.tmp`)), false);
});
