import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import {
  environmentWithoutCodeSigningCredentials,
  environmentWithoutXAIAPIKey,
} from './build-environment.mjs';

const desktop = join(dirname(fileURLToPath(import.meta.url)), '..');

test('removes every casing of XAI_API_KEY without mutating the build environment', () => {
  const credentialName = ['XAI', 'API', 'KEY'].join('_');
  const original = {
    KEEP_ME: 'yes',
    [credentialName.toLowerCase()]: 'lowercase',
    [credentialName]: 'uppercase',
    Xai_Api_Key: 'mixed',
  };

  const sanitized = environmentWithoutXAIAPIKey(original);

  assert.deepEqual(sanitized, { KEEP_ME: 'yes' });
  assert.equal(original[credentialName], 'uppercase');
});

test('disables code-signing discovery and strips every signing credential casing', () => {
  const certificateName = ['CSC', 'LINK'].join('_');
  const passwordName = ['CSC', 'KEY', 'PASSWORD'].join('_');
  const windowsCertificateName = ['WIN', 'CSC', 'LINK'].join('_');
  const windowsPasswordName = ['WIN', 'CSC', 'KEY', 'PASSWORD'].join('_');
  const original = {
    [certificateName]: 'certificate-input',
    KEEP_ME: 'yes',
    [passwordName.toLowerCase()]: 'password-input',
    [windowsCertificateName]: 'windows-certificate-input',
    [windowsPasswordName.toLowerCase()]: 'windows-password-input',
  };

  assert.deepEqual(environmentWithoutCodeSigningCredentials(original), {
    CSC_IDENTITY_AUTO_DISCOVERY: 'false',
    KEEP_ME: 'yes',
  });
  assert.equal(original[certificateName], 'certificate-input');
});

test('desktop manifest exposes one credential-free distribution path', () => {
  const manifest = JSON.parse(readFileSync(join(desktop, 'package.json'), 'utf8'));
  const scripts = manifest.scripts;
  const resources = manifest.build.extraResources;

  assert.equal(scripts.assemble, 'node scripts/assemble.mjs');
  assert.equal(scripts.dist, 'node scripts/dist.mjs');
  assert.equal(Object.keys(scripts).some((name) => name.includes('team')), false);
  assert.equal(
    resources.some((resource) => /(?:credential|xai-api-key)/i.test(`${resource.from} ${resource.to}`)),
    false,
  );
  assert.equal(
    resources.some((resource) => resource.from === 'build-resources/hlae' && resource.to === 'hlae'),
    true,
  );
});
