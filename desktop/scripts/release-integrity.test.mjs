import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { basename, join } from 'node:path';
import test from 'node:test';
import { verifyReleaseChecksums, writeReleaseChecksums } from './release-integrity.mjs';

test('writes deterministic checksums and verifies both release artifacts', async () => {
  await withReleaseFiles(async ({ artifacts, checksum }) => {
    await writeReleaseChecksums(artifacts, checksum);

    const want = artifacts
      .map((artifact) => `${sha256(readFileSync(artifact))}  ${basename(artifact)}`)
      .join('\n') + '\n';
    assert.equal(readFileSync(checksum, 'utf8'), want);
    await verifyReleaseChecksums(artifacts, checksum);
  });
});

test('rejects an artifact modified after checksum generation', async () => {
  await withReleaseFiles(async ({ artifacts, checksum }) => {
    await writeReleaseChecksums(artifacts, checksum);
    writeFileSync(artifacts[0], 'tampered installer');

    await assert.rejects(
      verifyReleaseChecksums(artifacts, checksum),
      /sha256 mismatch/,
    );
  });
});

test('rejects missing, duplicate, and unexpected checksum entries', async () => {
  await withReleaseFiles(async ({ artifacts, checksum }) => {
    await writeReleaseChecksums(artifacts, checksum);
    const lines = readFileSync(checksum, 'utf8').trimEnd().split('\n');

    writeFileSync(checksum, `${lines[0]}\n`);
    await assert.rejects(
      verifyReleaseChecksums(artifacts, checksum),
      /missing a release artifact/,
    );

    writeFileSync(checksum, `${lines[0]}\n${lines[0]}\n`);
    await assert.rejects(
      verifyReleaseChecksums(artifacts, checksum),
      /unexpected checksum entry/,
    );

    writeFileSync(checksum, `${'0'.repeat(64)}  unexpected.exe\n`);
    await assert.rejects(
      verifyReleaseChecksums(artifacts, checksum),
      /unexpected checksum entry/,
    );
  });
});

test('refuses to checksum an empty release artifact', async () => {
  await withReleaseFiles(async ({ artifacts, checksum }) => {
    writeFileSync(artifacts[1], '');

    await assert.rejects(
      writeReleaseChecksums(artifacts, checksum),
      /release artifact is empty/,
    );
  });
});

async function withReleaseFiles(run) {
  const directory = mkdtempSync(join(tmpdir(), 'fragforge-release-integrity-'));
  const artifacts = [
    join(directory, 'FragForge Studio Setup 2.2.1.exe'),
    join(directory, 'FragForge Studio Setup 2.2.1.exe.blockmap'),
  ];
  const checksum = join(directory, 'SHA256SUMS.txt');
  writeFileSync(artifacts[0], 'installer bytes');
  writeFileSync(artifacts[1], 'blockmap bytes');
  try {
    await run({ artifacts, checksum });
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
}

function sha256(value) {
  return createHash('sha256').update(value).digest('hex');
}
