// Unit tests for the /api/pc/status response shape.
// Run: node --test pcStatus.test.ts
import test from 'node:test';
import assert from 'node:assert/strict';
import { pcStatus } from './pcStatus.ts';

const NOW = 1_700_000_000_000;
const recent = new Date(NOW - 10_000).toISOString();
const stale = new Date(NOW - 120_000).toISOString();

test('no agent row → unpaired, offline, no loopback', () => {
  assert.deepEqual(pcStatus(null, NOW), { paired: false, online: false, loopback: null });
  assert.deepEqual(pcStatus(undefined, NOW), { paired: false, online: false, loopback: null });
});

test('paired agent with a recent heartbeat and a token is online with a loopback', () => {
  assert.deepEqual(
    pcStatus({ last_heartbeat_at: recent, loopback_token: 'tok', loopback_port: 8090 }, NOW),
    { paired: true, online: true, loopback: { port: 8090, token: 'tok' } },
  );
});

test('a stale heartbeat is paired-but-offline, still exposing the loopback', () => {
  assert.deepEqual(
    pcStatus({ last_heartbeat_at: stale, loopback_token: 'tok', loopback_port: 8091 }, NOW),
    { paired: true, online: false, loopback: { port: 8091, token: 'tok' } },
  );
});

test('a paired agent that has not reported a token yet has a null loopback', () => {
  assert.deepEqual(
    pcStatus({ last_heartbeat_at: recent, loopback_token: '', loopback_port: 8090 }, NOW),
    { paired: true, online: true, loopback: null },
  );
});

test('a missing loopback_port defaults to 8090', () => {
  assert.deepEqual(
    pcStatus({ last_heartbeat_at: recent, loopback_token: 'tok', loopback_port: null }, NOW),
    { paired: true, online: true, loopback: { port: 8090, token: 'tok' } },
  );
});
