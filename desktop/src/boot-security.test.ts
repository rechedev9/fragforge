import assert from 'node:assert/strict';
import test from 'node:test';
import {
  createBootSecurityCapabilities,
  installProxyCapabilityCookie,
  orchestratorSecurityEnvironment,
  PROXY_CAPABILITY_COOKIE,
  webSecurityEnvironment,
  type CookieStore,
} from './boot-security.ts';

test('creates distinct per-boot capabilities and keeps each child environment minimal', () => {
  const values = ['1'.repeat(64), '2'.repeat(64), '3'.repeat(64)];
  const capabilities = createBootSecurityCapabilities(() => {
    const value = values.shift();
    if (value === undefined) throw new Error('unexpected capability request');
    return value;
  });

  assert.deepEqual(orchestratorSecurityEnvironment(capabilities), {
    FRAGFORGE_PROXY_BOOTSTRAP_CAPABILITY: undefined,
    FRAGFORGE_PROXY_MUTATION_CAPABILITY: undefined,
    ORCHESTRATOR_TOKEN: undefined,
    ZV_DISCOVERY_SECRET: '1'.repeat(64),
    ZV_MUTATION_TOKEN: '2'.repeat(64),
  });
  assert.deepEqual(webSecurityEnvironment(capabilities), {
    FRAGFORGE_PROXY_BOOTSTRAP_CAPABILITY: undefined,
    FRAGFORGE_PROXY_MUTATION_CAPABILITY: '3'.repeat(64),
    ORCHESTRATOR_TOKEN: '2'.repeat(64),
    ZV_DISCOVERY_SECRET: undefined,
    ZV_MUTATION_TOKEN: undefined,
  });
});

test('rejects duplicate or malformed boot capabilities', () => {
  assert.throws(
    () => createBootSecurityCapabilities(() => 'a'.repeat(64)),
    /must be distinct/,
  );
  assert.throws(
    () => createBootSecurityCapabilities(() => 'not-a-capability'),
    /invalid value/,
  );
});

test('installs the proxy capability as an HttpOnly strict loopback cookie', async () => {
  const writes: Parameters<CookieStore['set']>[0][] = [];
  await installProxyCapabilityCookie(
    { set: async (details) => { writes.push(details); } },
    'http://127.0.0.1:43123',
    'c'.repeat(64),
  );

  assert.deepEqual(writes, [{
    httpOnly: true,
    name: PROXY_CAPABILITY_COOKIE,
    path: '/',
    sameSite: 'strict',
    secure: false,
    url: 'http://127.0.0.1:43123',
    value: 'c'.repeat(64),
  }]);
});

test('refuses to seed the proxy capability for an ambiguous local authority', async () => {
  const cookies: CookieStore = { set: async () => {} };
  await assert.rejects(
    installProxyCapabilityCookie(cookies, 'http://localhost:43123', 'd'.repeat(64)),
    /explicit HTTP loopback origin/,
  );
  await assert.rejects(
    installProxyCapabilityCookie(cookies, 'https://127.0.0.1:43123', 'd'.repeat(64)),
    /explicit HTTP loopback origin/,
  );
});
