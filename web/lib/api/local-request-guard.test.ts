import test from 'node:test';
import assert from 'node:assert/strict';
import { localAPIRequestError } from './local-request-guard.ts';

function requestHeaders(values: Record<string, string>): Headers {
  return new Headers(values);
}

test('rejects a non-loopback Host even when the browser reports same-origin', () => {
  const error = localAPIRequestError(requestHeaders({
    host: 'attacker.example:3000',
    'sec-fetch-site': 'same-origin',
  }));

  assert.match(error ?? '', /host rejected/);
});

test('rejects cross-site browser requests to a loopback Host', () => {
  const error = localAPIRequestError(requestHeaders({
    host: '127.0.0.1:3000',
    'sec-fetch-site': 'cross-site',
  }));

  assert.equal(error, 'cross-site request blocked');
});

test('rejects an Origin that does not match Host', () => {
  const error = localAPIRequestError(requestHeaders({
    host: 'localhost:3000',
    origin: 'http://evil.example:3000',
    'sec-fetch-site': 'same-origin',
  }));

  assert.equal(error, 'cross-site request blocked');
});

test('allows same-origin requests for supported loopback hosts', () => {
  for (const host of ['localhost:3000', '127.42.0.9:43120', '[::1]:8080']) {
    const error = localAPIRequestError(requestHeaders({
      host,
      origin: `http://${host}`,
      'sec-fetch-site': 'same-origin',
    }));

    assert.equal(error, undefined, host);
  }
});

test('allows origin-less server requests only when Host is loopback with a port', () => {
  assert.equal(localAPIRequestError(requestHeaders({ host: '127.0.0.1:8080' })), undefined);
  assert.equal(localAPIRequestError(requestHeaders({
    host: 'localhost:3000',
    'sec-fetch-site': 'none',
  })), undefined);
  assert.match(localAPIRequestError(requestHeaders({ host: 'localhost' })) ?? '', /host rejected/);
  assert.match(localAPIRequestError(requestHeaders({ host: '127.0.0.1:0' })) ?? '', /host rejected/);
});
