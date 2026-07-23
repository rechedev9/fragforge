import test from 'node:test';
import assert from 'node:assert/strict';
import { localAPIBootstrapError, localAPIRequestError } from './local-request-guard.ts';

function requestHeaders(values: Record<string, string>): Headers {
  return new Headers(values);
}

test('rejects a non-loopback Host even when the browser reports same-origin', async () => {
  const error = await localAPIRequestError(requestHeaders({
    host: 'attacker.example:3000',
    'sec-fetch-site': 'same-origin',
  }));

  assert.match(error ?? '', /host rejected/);
});

test('rejects cross-site browser requests to a loopback Host', async () => {
  const error = await localAPIRequestError(requestHeaders({
    host: '127.0.0.1:3000',
    'sec-fetch-site': 'cross-site',
  }));

  assert.equal(error, 'cross-site request blocked');
});

test('rejects an Origin that does not match Host', async () => {
  const error = await localAPIRequestError(requestHeaders({
    host: 'localhost:3000',
    origin: 'http://evil.example:3000',
    'sec-fetch-site': 'same-origin',
  }));

  assert.equal(error, 'cross-site request blocked');
});

test('allows same-origin reads for supported loopback hosts', async () => {
  for (const host of ['localhost:3000', '127.42.0.9:43120', '[::1]:8080']) {
    const error = await localAPIRequestError(requestHeaders({
      host,
      origin: `http://${host}`,
      'sec-fetch-site': 'same-origin',
    }));

    assert.equal(error, undefined, host);
  }
});

test('allows origin-less reads only when Host is loopback with a port', async () => {
  assert.equal(await localAPIRequestError(requestHeaders({ host: '127.0.0.1:8080' })), undefined);
  assert.equal(await localAPIRequestError(requestHeaders({
    host: 'localhost:3000',
    'sec-fetch-site': 'none',
  })), undefined);
  assert.match(await localAPIRequestError(requestHeaders({ host: 'localhost' })) ?? '', /host rejected/);
  assert.match(await localAPIRequestError(requestHeaders({ host: '127.0.0.1:0' })) ?? '', /host rejected/);
});

test('fails closed for mutations without a seeded capability', async () => {
  const previous = process.env.FRAGFORGE_PROXY_MUTATION_CAPABILITY;
  delete process.env.FRAGFORGE_PROXY_MUTATION_CAPABILITY;
  try {
    const error = await localAPIRequestError(requestHeaders({
      host: '127.0.0.1:3000',
      origin: 'http://127.0.0.1:3000',
      'sec-fetch-site': 'same-origin',
    }), 'POST');
    assert.equal(error, 'local API mutation capability required');
  } finally {
    if (previous === undefined) delete process.env.FRAGFORGE_PROXY_MUTATION_CAPABILITY;
    else process.env.FRAGFORGE_PROXY_MUTATION_CAPABILITY = previous;
  }
});

test('requires a capability for origin-less local mutations too', async () => {
  const previous = process.env.FRAGFORGE_PROXY_MUTATION_CAPABILITY;
  delete process.env.FRAGFORGE_PROXY_MUTATION_CAPABILITY;
  try {
    const error = await localAPIRequestError(requestHeaders({ host: '127.0.0.1:3000' }), 'POST');
    assert.equal(error, 'local API mutation capability required');
  } finally {
    if (previous === undefined) delete process.env.FRAGFORGE_PROXY_MUTATION_CAPABILITY;
    else process.env.FRAGFORGE_PROXY_MUTATION_CAPABILITY = previous;
  }
});

test('requires the one capability cookie for mutations', async () => {
  const previous = process.env.FRAGFORGE_PROXY_MUTATION_CAPABILITY;
  process.env.FRAGFORGE_PROXY_MUTATION_CAPABILITY = 'one-launch-secret';
  try {
    const base = {
      host: '127.0.0.1:3000',
      origin: 'http://127.0.0.1:3000',
      'sec-fetch-site': 'same-origin',
    };
    assert.equal(await localAPIRequestError(requestHeaders(base), 'DELETE'), 'local API mutation capability required');
    assert.equal(await localAPIRequestError(requestHeaders({
      ...base,
      cookie: 'fragforge_proxy_capability=wrong-secret',
    }), 'DELETE'), 'local API mutation capability required');
    assert.equal(await localAPIRequestError(requestHeaders({
      ...base,
      cookie: 'fragforge_proxy_capability=one-launch-secret',
    }), 'DELETE'), undefined);
  } finally {
    if (previous === undefined) delete process.env.FRAGFORGE_PROXY_MUTATION_CAPABILITY;
    else process.env.FRAGFORGE_PROXY_MUTATION_CAPABILITY = previous;
  }
});

test('rejects an ambiguous duplicated mutation capability cookie', async () => {
  const previous = process.env.FRAGFORGE_PROXY_MUTATION_CAPABILITY;
  process.env.FRAGFORGE_PROXY_MUTATION_CAPABILITY = 'one-launch-secret';
  try {
    const error = await localAPIRequestError(requestHeaders({
      host: '127.0.0.1:3000',
      origin: 'http://127.0.0.1:3000',
      'sec-fetch-site': 'same-origin',
      cookie: 'fragforge_proxy_capability=one-launch-secret; fragforge_proxy_capability=wrong-secret',
    }), 'PATCH');
    assert.equal(error, 'local API mutation capability required');
  } finally {
    if (previous === undefined) delete process.env.FRAGFORGE_PROXY_MUTATION_CAPABILITY;
    else process.env.FRAGFORGE_PROXY_MUTATION_CAPABILITY = previous;
  }
});

test('bootstrap needs its separate server-only capability and preserves the origin guard', async () => {
  const previous = process.env.FRAGFORGE_PROXY_BOOTSTRAP_CAPABILITY;
  process.env.FRAGFORGE_PROXY_BOOTSTRAP_CAPABILITY = 'standalone-bootstrap-secret';
  try {
    const headers = requestHeaders({
      host: '127.0.0.1:3000',
      origin: 'http://127.0.0.1:3000',
      'sec-fetch-site': 'same-origin',
    });
    assert.equal(await localAPIBootstrapError(headers, null), 'local API bootstrap capability required');
    assert.equal(await localAPIBootstrapError(headers, 'wrong-secret'), 'local API bootstrap capability required');
    assert.equal(await localAPIBootstrapError(headers, 'standalone-bootstrap-secret'), undefined);
    assert.equal(await localAPIBootstrapError(requestHeaders({
      host: '127.0.0.1:3000',
      origin: 'http://evil.example:3000',
      'sec-fetch-site': 'same-origin',
    }), 'standalone-bootstrap-secret'), 'cross-site request blocked');
  } finally {
    if (previous === undefined) delete process.env.FRAGFORGE_PROXY_BOOTSTRAP_CAPABILITY;
    else process.env.FRAGFORGE_PROXY_BOOTSTRAP_CAPABILITY = previous;
  }
});
