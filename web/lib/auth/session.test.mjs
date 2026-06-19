// Unit tests for the signed-cookie session. Run: node --test session.test.mjs
import test from 'node:test';
import assert from 'node:assert/strict';
import { signSession, verifySession } from './session.ts';

const payload = {
  steamid64: '76561198000000000',
  persona: 'kekO',
  avatar: 'https://example.com/a.jpg',
  matchHistoryLinked: false,
};

test('round-trips a signed session', () => {
  assert.deepEqual(verifySession(signSession(payload)), payload);
});

test('preserves matchHistoryLinked=true', () => {
  const linked = { ...payload, matchHistoryLinked: true };
  assert.deepEqual(verifySession(signSession(linked)), linked);
});

test('rejects a tampered payload (mac no longer matches)', () => {
  const token = signSession(payload);
  const mac = token.slice(token.lastIndexOf('.') + 1);
  const forgedBody = Buffer.from(
    JSON.stringify({ ...payload, steamid64: '76561190000000000' }),
  ).toString('base64url');
  assert.equal(verifySession(`${forgedBody}.${mac}`), null);
});

test('rejects garbage and empty tokens', () => {
  assert.equal(verifySession(undefined), null);
  assert.equal(verifySession(''), null);
  assert.equal(verifySession('nope'), null);
  assert.equal(verifySession('a.b'), null);
});

test('rejects a non-17-digit steamid even if well-signed', () => {
  assert.equal(verifySession(signSession({ ...payload, steamid64: '123' })), null);
});
