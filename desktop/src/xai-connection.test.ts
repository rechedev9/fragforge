import assert from 'node:assert/strict';
import test from 'node:test';
import { testXAIConnection } from './xai-connection.ts';

const ACTIVE_STATUS = {
  acls: ['api-key:endpoint:*'],
  api_key_blocked: false,
  api_key_disabled: false,
  redacted_api_key: 'must-never-leave-main',
  team_blocked: false,
};

test('checks the fixed xAI key-status endpoint without exposing account metadata', async () => {
  let authorization = '';
  let requestURL = '';
  const outcome = await testXAIConnection('  test-secret  ', {
    fetchImpl: async (input, init) => {
      requestURL = String(input);
      authorization = new Headers(init?.headers).get('Authorization') ?? '';
      return Response.json(ACTIVE_STATUS);
    },
  });

  assert.equal(requestURL, 'https://api.x.ai/v1/api-key');
  assert.equal(authorization, 'Bearer test-secret');
  assert.deepEqual(outcome, {
    code: 'valid',
    message: 'Clave válida y activa. Asegúrate de concederle acceso a Speech-to-Text.',
    ok: true,
  });
  assert.doesNotMatch(JSON.stringify(outcome), /test-secret|must-never-leave-main/);
});

test('maps rejected, blocked, permissionless, limited, malformed, and oversized responses', async (t) => {
  const cases: Array<{ body?: unknown; code: string; name: string; status?: number }> = [
    { code: 'invalid', name: 'unauthorized', status: 401 },
    { code: 'rate_limited', name: 'limited', status: 429 },
    { body: { ...ACTIVE_STATUS, api_key_disabled: true }, code: 'blocked', name: 'disabled' },
    { body: { ...ACTIVE_STATUS, acls: [] }, code: 'missing_permissions', name: 'no permissions' },
    { body: { unexpected: true }, code: 'service_error', name: 'malformed' },
  ];

  for (const scenario of cases) {
    await t.test(scenario.name, async () => {
      const outcome = await testXAIConnection('secret', {
        fetchImpl: async () => Response.json(scenario.body ?? { error: 'redacted-secret' }, {
          status: scenario.status ?? 200,
        }),
      });
      assert.equal(outcome.ok, false);
      assert.equal(outcome.code, scenario.code);
      assert.doesNotMatch(JSON.stringify(outcome), /secret|redacted/);
    });
  }

  const oversized = await testXAIConnection('secret', {
    fetchImpl: async () => new Response('x'.repeat(65 * 1024), {
      headers: { 'Content-Type': 'application/json' },
    }),
  });
  assert.equal(oversized.code, 'service_error');
});

test('rejects unsafe input and maps network failures without echoing the key', async () => {
  let called = false;
  const invalid = await testXAIConnection('unsafe\nsecret', {
    fetchImpl: async () => {
      called = true;
      return Response.json(ACTIVE_STATUS);
    },
  });
  assert.equal(called, false);
  assert.equal(invalid.code, 'invalid_format');
  assert.doesNotMatch(JSON.stringify(invalid), /unsafe|secret/);

  const network = await testXAIConnection('network-canary', {
    fetchImpl: async () => {
      throw new Error('network failure network-canary');
    },
  });
  assert.equal(network.code, 'network_error');
  assert.doesNotMatch(JSON.stringify(network), /network-canary/);
});

test('bounds a stalled xAI key check', async () => {
  const outcome = await testXAIConnection('timeout-canary', {
    fetchImpl: async (_input, init) => new Promise<Response>((_resolve, reject) => {
      init?.signal?.addEventListener('abort', () => reject(new Error('aborted timeout-canary')), { once: true });
    }),
    timeoutMs: 5,
  });
  assert.equal(outcome.code, 'network_error');
  assert.match(outcome.message, /tiempo de espera/);
  assert.doesNotMatch(JSON.stringify(outcome), /timeout-canary/);
});

test('keeps the timeout active while reading a stalled successful body', async () => {
  const outcome = await testXAIConnection('body-timeout-canary', {
    fetchImpl: async (_input, init) => {
      const body = new ReadableStream<Uint8Array>({
        start: (streamController) => {
          init?.signal?.addEventListener('abort', () => {
            streamController.error(new Error('aborted body-timeout-canary'));
          }, { once: true });
        },
      });
      return new Response(body, { status: 200 });
    },
    timeoutMs: 5,
  });

  assert.equal(outcome.code, 'network_error');
  assert.match(outcome.message, /tiempo de espera/);
  assert.doesNotMatch(JSON.stringify(outcome), /body-timeout-canary/);
});
