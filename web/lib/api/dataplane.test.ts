// Unit tests for the client data-plane addressing (local proxy vs cloud loopback).
// Run: node --test dataplane.test.ts
import test from 'node:test';
import assert from 'node:assert/strict';
import { dataPlane, loopbackOrigin, offlineReason, type Loopback } from './dataplane.ts';

const LB: Loopback = { port: 8090, token: 'tok-abc' };
const JOB = '11111111-1111-4111-8111-111111111111';

test('local mode targets the same-origin /api/demos proxy with no auth header', () => {
  const dp = dataPlane(null);
  assert.deepEqual(dp.headers, {});
  assert.equal(dp.scanUrl, '/api/demos/scan');
  assert.equal(dp.scanField, 'file');
  assert.equal(dp.jobStatusUrl(JOB), `/api/demos/${JOB}/status`);
  assert.equal(dp.rosterUrl(JOB), `/api/demos/${JOB}/roster`);
  assert.equal(dp.parseUrl(JOB), `/api/demos/${JOB}/parse`);
  assert.equal(dp.planUrl(JOB), `/api/demos/${JOB}/plan`);
  assert.equal(dp.recordUrl(JOB), `/api/demos/${JOB}/record`);
  assert.equal(dp.renderUrl(JOB, 'viral-60-clean'), `/api/demos/${JOB}/renders/viral-60-clean`);
  assert.equal(dp.videoUrl(JOB, 'viral-60-clean', 'a_b'), `/api/demos/${JOB}/renders/viral-60-clean/videos/a_b`);
  assert.equal(dp.coverUrl(JOB, 'viral-60-clean', 'a_b'), `/api/demos/${JOB}/renders/viral-60-clean/covers/a_b`);
  assert.equal(dp.capabilitiesUrl, '/api/capabilities');
  assert.equal(dp.healthzUrl, null);
});

test('local mode reads the proxy scan response key and posts { steamId }', () => {
  const dp = dataPlane(null);
  assert.equal(dp.scanJobId({ jobId: JOB }), JOB);
  assert.equal(dp.scanJobId({ id: 'ignored' }), '');
  assert.deepEqual(dp.parseBody('76561198000000001'), { steamId: '76561198000000001' });
});

test('cloud mode targets the loopback /api/jobs API with a Bearer header', () => {
  const dp = dataPlane(LB);
  assert.deepEqual(dp.headers, { Authorization: 'Bearer tok-abc' });
  assert.equal(dp.scanUrl, 'http://127.0.0.1:8090/api/jobs');
  assert.equal(dp.scanField, 'demo');
  // The orchestrator's native job status is GET /api/jobs/{id} (no /status suffix).
  assert.equal(dp.jobStatusUrl(JOB), `http://127.0.0.1:8090/api/jobs/${JOB}`);
  assert.equal(dp.rosterUrl(JOB), `http://127.0.0.1:8090/api/jobs/${JOB}/roster`);
  assert.equal(dp.parseUrl(JOB), `http://127.0.0.1:8090/api/jobs/${JOB}/parse`);
  assert.equal(dp.planUrl(JOB), `http://127.0.0.1:8090/api/jobs/${JOB}/plan`);
  assert.equal(dp.recordUrl(JOB), `http://127.0.0.1:8090/api/jobs/${JOB}/record`);
  assert.equal(dp.renderUrl(JOB, 'v'), `http://127.0.0.1:8090/api/jobs/${JOB}/renders/v`);
  assert.equal(dp.videoUrl(JOB, 'v', 'a_b'), `http://127.0.0.1:8090/api/jobs/${JOB}/renders/v/videos/a_b`);
  assert.equal(dp.coverUrl(JOB, 'v', 'a_b'), `http://127.0.0.1:8090/api/jobs/${JOB}/renders/v/covers/a_b`);
  assert.equal(dp.capabilitiesUrl, 'http://127.0.0.1:8090/api/capabilities');
  assert.equal(dp.healthzUrl, 'http://127.0.0.1:8090/healthz');
});

test('cloud mode reads the orchestrator scan response key and posts { target_steamid }', () => {
  const dp = dataPlane(LB);
  assert.equal(dp.scanJobId({ id: JOB }), JOB);
  assert.equal(dp.scanJobId({ jobId: 'ignored' }), '');
  assert.deepEqual(dp.parseBody('76561198000000001'), { target_steamid: '76561198000000001' });
});

test('loopbackOrigin pins the loopback host', () => {
  assert.equal(loopbackOrigin({ port: 9001, token: 't' }), 'http://127.0.0.1:9001');
});

test('offlineReason distinguishes PC off from agent not running via heartbeat', () => {
  assert.equal(offlineReason({ paired: true, online: false, loopback: LB }), 'pc-off');
  assert.equal(offlineReason({ paired: true, online: true, loopback: LB }), 'agent-not-running');
});
