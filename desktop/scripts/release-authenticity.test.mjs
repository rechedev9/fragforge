import assert from 'node:assert/strict';
import { mkdtempSync, readFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import test from 'node:test';
import {
  environmentWithoutAuthenticodeCredentials,
  requireAuthenticodeSigningConfiguration,
  verifyAuthenticodeSignature,
  verifyPublisherIdentity,
  writePublisherIdentity,
} from './release-authenticity.mjs';

const validSignature = {
  signatureType: 'Authenticode',
  signerSubject: 'CN=FragForge Test Publisher, O=FragForge Test Publisher',
  signerThumbprint: 'A'.repeat(40),
  status: 'Valid',
  timestampSubject: 'CN=Trusted Timestamp Authority',
};

test('requires signing material and an independently configured expected publisher', () => {
  assert.deepEqual(requireAuthenticodeSigningConfiguration({
    CSC_KEY_PASSWORD: 'protected-secret',
    CSC_LINK: 'certificate-from-secret-storage',
    FRAGFORGE_AUTHENTICODE_SUBJECT: validSignature.signerSubject,
  }), {
    expectedSubject: validSignature.signerSubject,
  });

  assert.throws(
    () => requireAuthenticodeSigningConfiguration({
      CSC_KEY_PASSWORD: 'protected-secret',
      FRAGFORGE_AUTHENTICODE_SUBJECT: validSignature.signerSubject,
    }),
    /CSC_LINK/,
  );
  assert.throws(
    () => requireAuthenticodeSigningConfiguration({
      CSC_KEY_PASSWORD: 'protected-secret',
      CSC_LINK: 'certificate-from-secret-storage',
    }),
    /FRAGFORGE_AUTHENTICODE_SUBJECT/,
  );
});

test('keeps signing credentials out of non-signing build children', () => {
  assert.deepEqual(environmentWithoutAuthenticodeCredentials({
    CSC_KEY_PASSWORD: 'secret',
    KEEP_ME: 'yes',
    win_csc_link: 'protected.pfx',
  }), {
    KEEP_ME: 'yes',
  });
});

test('accepts only a valid timestamped Authenticode signature from the expected publisher', async () => {
  const signature = await verifyAuthenticodeSignature(
    'installer.exe',
    validSignature.signerSubject,
    async () => JSON.stringify(validSignature),
  );
  assert.deepEqual(signature, validSignature);
});

for (const [label, change, expected] of [
  ['unsigned', { status: 'NotSigned' }, /status is NotSigned/],
  ['wrong publisher', { signerSubject: 'CN=Attacker' }, /signer does not match/],
  ['missing timestamp', { timestampSubject: '' }, /not timestamped/],
  ['catalog signature', { signatureType: 'Catalog' }, /direct Authenticode/],
]) {
  test(`rejects a ${label} installer`, async () => {
    await assert.rejects(
      verifyAuthenticodeSignature(
        'installer.exe',
        validSignature.signerSubject,
        async () => JSON.stringify({ ...validSignature, ...change }),
      ),
      expected,
    );
  });
}

test('persists and re-verifies the public signer identity beside release artifacts', () => {
  const directory = mkdtempSync(join(tmpdir(), 'fragforge-publisher-'));
  const publisherPath = join(directory, 'AUTHENTICODE_PUBLISHER.json');
  try {
    writePublisherIdentity(publisherPath, validSignature);
    assert.match(readFileSync(publisherPath, 'utf8'), /FragForge Test Publisher/);
    verifyPublisherIdentity(publisherPath, validSignature);
    assert.throws(
      () => verifyPublisherIdentity(publisherPath, { ...validSignature, signerThumbprint: 'B'.repeat(40) }),
      /does not match/,
    );
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
});
