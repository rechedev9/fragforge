import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import path from 'node:path';
import test from 'node:test';
import { isJsonObject, type JsonObject } from './json.ts';
import { listOperations, operationNamed } from './operations.ts';

interface CoverageCase {
  input: JsonObject;
  method: 'DELETE' | 'GET' | 'POST' | 'PUT';
  operation: string;
  route: string;
}

interface PreviewRequest {
  method: string;
  path: string;
}

const JOB_INPUT: JsonObject = { job_id: '11111111-1111-4111-8111-111111111111' };
const RENDER_INPUT: JsonObject = { ...JOB_INPUT, variant: 'viral-60-clean' };
const RENDER_ARTIFACT_INPUT: JsonObject = { ...RENDER_INPUT, name: 'final' };
const STREAM_INPUT: JsonObject = { stream_job_id: '22222222-2222-4222-8222-222222222222' };
const STREAM_RENDER_INPUT: JsonObject = { ...STREAM_INPUT, variant: 'streamer-vertical-stack' };
const STREAM_PLAN: JsonObject = {
  face_crop: { height: 0.3, width: 1, x: 0, y: 0 },
  gameplay_crop: { height: 0.7, width: 1, x: 0, y: 0.3 },
  variant: 'streamer-vertical-stack',
};

const COVERAGE: readonly CoverageCase[] = [
  { input: {}, method: 'GET', operation: 'studio.status', route: '/healthz' },
  { input: {}, method: 'GET', operation: 'studio.metrics', route: '/metrics' },
  { input: {}, method: 'GET', operation: 'studio.status', route: '/api/capabilities' },
  { input: {}, method: 'GET', operation: 'catalog.loadouts', route: '/api/loadouts' },
  { input: {}, method: 'GET', operation: 'catalog.presets', route: '/api/presets' },
  { input: {}, method: 'GET', operation: 'catalog.songs', route: '/api/songs' },
  { input: { song_id: 'song-one' }, method: 'GET', operation: 'artifacts.get_song_url', route: '/api/songs/{id}/audio' },
  { input: {}, method: 'GET', operation: 'catalog.stream_variants', route: '/api/stream-variants' },
  { input: { demo_path: 'C:\\demos\\match.dem' }, method: 'POST', operation: 'jobs.create', route: '/api/jobs' },
  { input: {}, method: 'GET', operation: 'jobs.list', route: '/api/jobs' },
  { input: JOB_INPUT, method: 'GET', operation: 'jobs.get', route: '/api/jobs/{id}' },
  { input: JOB_INPUT, method: 'DELETE', operation: 'jobs.delete', route: '/api/jobs/{id}' },
  { input: JOB_INPUT, method: 'GET', operation: 'jobs.plan', route: '/api/jobs/{id}/plan' },
  { input: JOB_INPUT, method: 'GET', operation: 'jobs.roster', route: '/api/jobs/{id}/roster' },
  { input: { ...JOB_INPUT, target_steamid: '76561198000000000' }, method: 'POST', operation: 'jobs.parse', route: '/api/jobs/{id}/parse' },
  { input: JOB_INPUT, method: 'GET', operation: 'jobs.moments', route: '/api/jobs/{id}/moments' },
  { input: { ...JOB_INPUT, kind: 'final' }, method: 'GET', operation: 'artifacts.get_url', route: '/api/jobs/{id}/final' },
  { input: JOB_INPUT, method: 'POST', operation: 'jobs.record', route: '/api/jobs/{id}/record' },
  { input: { ...JOB_INPUT, preset: 'viral-60-clean' }, method: 'POST', operation: 'jobs.generate', route: '/api/jobs/{id}/generate' },
  { input: JOB_INPUT, method: 'POST', operation: 'jobs.compose', route: '/api/jobs/{id}/compose' },
  { input: RENDER_INPUT, method: 'POST', operation: 'renders.start', route: '/api/jobs/{id}/renders/{variant}' },
  { input: RENDER_INPUT, method: 'GET', operation: 'renders.get', route: '/api/jobs/{id}/renders/{variant}' },
  { input: RENDER_INPUT, method: 'GET', operation: 'renders.publish', route: '/api/jobs/{id}/renders/{variant}/publish' },
  { input: RENDER_INPUT, method: 'GET', operation: 'renders.quality', route: '/api/jobs/{id}/renders/{variant}/quality' },
  { input: RENDER_INPUT, method: 'POST', operation: 'renders.start_caption_agent', route: '/api/jobs/{id}/renders/{variant}/agent/captions' },
  { input: RENDER_INPUT, method: 'GET', operation: 'renders.caption_candidates', route: '/api/jobs/{id}/renders/{variant}/agent/captions' },
  { input: { ...RENDER_INPUT, kind: 'pack' }, method: 'GET', operation: 'artifacts.get_url', route: '/api/jobs/{id}/renders/{variant}/pack' },
  { input: { ...RENDER_INPUT, kind: 'edit_document' }, method: 'GET', operation: 'artifacts.get_url', route: '/api/jobs/{id}/renders/{variant}/edit-document' },
  { input: { ...RENDER_INPUT, kind: 'gallery' }, method: 'GET', operation: 'artifacts.get_url', route: '/api/jobs/{id}/renders/{variant}/gallery' },
  { input: { ...RENDER_ARTIFACT_INPUT, kind: 'video' }, method: 'GET', operation: 'artifacts.get_url', route: '/api/jobs/{id}/renders/{variant}/videos/{name}' },
  { input: RENDER_ARTIFACT_INPUT, method: 'DELETE', operation: 'renders.delete_video', route: '/api/jobs/{id}/renders/{variant}/videos/{name}' },
  { input: RENDER_ARTIFACT_INPUT, method: 'GET', operation: 'renders.publish_assistant', route: '/api/jobs/{id}/renders/{variant}/videos/{name}/publish-assistant' },
  { input: { ...RENDER_ARTIFACT_INPUT, kind: 'cover' }, method: 'GET', operation: 'artifacts.get_url', route: '/api/jobs/{id}/renders/{variant}/covers/{name}' },
  { input: { ...RENDER_ARTIFACT_INPUT, kind: 'caption' }, method: 'GET', operation: 'artifacts.get_url', route: '/api/jobs/{id}/renders/{variant}/captions/{name}' },
  { input: { video_path: 'C:\\captures\\stream.mp4' }, method: 'POST', operation: 'streams.create_from_file', route: '/api/stream-jobs' },
  { input: { source_url: 'https://www.twitch.tv/videos/123' }, method: 'POST', operation: 'streams.create_from_url', route: '/api/stream-jobs' },
  { input: {}, method: 'GET', operation: 'streams.list', route: '/api/stream-jobs' },
  { input: STREAM_INPUT, method: 'GET', operation: 'streams.get', route: '/api/stream-jobs/{id}' },
  { input: { ...STREAM_INPUT, kind: 'source' }, method: 'GET', operation: 'artifacts.get_stream_url', route: '/api/stream-jobs/{id}/source' },
  { input: STREAM_INPUT, method: 'GET', operation: 'streams.get_edit_plan', route: '/api/stream-jobs/{id}/edit-plan' },
  { input: STREAM_INPUT, method: 'PUT', operation: 'streams.resume_initialization', route: '/api/stream-jobs/{id}/edit-plan' },
  { input: { ...STREAM_INPUT, plan: STREAM_PLAN }, method: 'PUT', operation: 'streams.update_edit_plan', route: '/api/stream-jobs/{id}/edit-plan' },
  { input: { ...STREAM_INPUT, enabled: true, language: 'es' }, method: 'PUT', operation: 'streams.configure_captions', route: '/api/stream-jobs/{id}/edit-plan' },
  { input: { ...STREAM_INPUT, clip_id: 'clip-1', speed: 2 }, method: 'PUT', operation: 'streams.edit_clip', route: '/api/stream-jobs/{id}/edit-plan' },
  { input: STREAM_RENDER_INPUT, method: 'POST', operation: 'streams.start_render', route: '/api/stream-jobs/{id}/renders/{variant}' },
  { input: STREAM_RENDER_INPUT, method: 'GET', operation: 'streams.get_render', route: '/api/stream-jobs/{id}/renders/{variant}' },
  { input: {}, method: 'GET', operation: 'streams.list_killfeed_weapons', route: '/api/stream-killfeed/weapons' },
  {
    input: { attacker_name: 'hero', attacker_side: 'CT', victim_name: 'villain', victim_side: 'T', weapon: 'ak47' },
    method: 'POST',
    operation: 'streams.preview_killfeed_notice',
    route: '/api/stream-killfeed/notice-preview',
  },
  { input: { ...STREAM_INPUT, clip_id: 'clip-1', cue_seconds: 2 }, method: 'POST', operation: 'streams.read_killfeed', route: '/api/stream-jobs/{id}/killfeed-read' },
  { input: { ...STREAM_RENDER_INPUT, kind: 'gallery' }, method: 'GET', operation: 'artifacts.get_stream_url', route: '/api/stream-jobs/{id}/renders/{variant}/gallery' },
  { input: { ...STREAM_RENDER_INPUT, clip_id: 'clip-1', kind: 'video' }, method: 'GET', operation: 'artifacts.get_stream_url', route: '/api/stream-jobs/{id}/renders/{variant}/videos/{clip_id}' },
];

function routeKey(method: string, route: string): string {
  return `${method} ${route}`;
}

function productRoutes(source: string): PreviewRequest[] {
  const routes: PreviewRequest[] = [];
  const routePattern = /r\.(Get|Post|Put|Delete)\(\s*"([^"]+)"/g;
  for (const match of source.matchAll(routePattern)) {
    const goMethod = match[1];
    const route = match[2];
    if (goMethod === undefined || route === undefined) continue;
    if (route !== '/healthz' && route !== '/metrics' && route !== '/api' && !route.startsWith('/api/')) continue;
    routes.push({ method: goMethod.toUpperCase(), path: route });
  }
  return routes;
}

function previewRequests(preview: JsonObject): PreviewRequest[] {
  const requests: PreviewRequest[] = [];
  if (typeof preview.method === 'string' && typeof preview.path === 'string') {
    requests.push({ method: preview.method, path: preview.path });
  }
  if (typeof preview.method === 'string' && Array.isArray(preview.paths)) {
    for (const previewPath of preview.paths) {
      if (typeof previewPath === 'string') requests.push({ method: preview.method, path: previewPath });
    }
  }
  if (Array.isArray(preview.steps)) {
    for (const step of preview.steps) {
      if (!isJsonObject(step) || typeof step.method !== 'string' || typeof step.path !== 'string') continue;
      requests.push({ method: step.method, path: step.path });
    }
  }
  return requests;
}

function pathMatchesRoute(previewPath: string, route: string): boolean {
  const pathSegments = previewPath.split('?', 1)[0]?.split('/') ?? [];
  const routeSegments = route.split('/');
  if (pathSegments.length !== routeSegments.length) return false;
  return routeSegments.every((segment, index) => {
    if (segment.startsWith('{') && segment.endsWith('}')) return pathSegments[index] !== '';
    return pathSegments[index] === segment;
  });
}

test('every product HTTP route has an allowlisted MCP operation with a matching preview', () => {
  const routesPath = path.resolve(process.cwd(), '../internal/httpapi/routes.go');
  const goRoutes = productRoutes(readFileSync(routesPath, 'utf8'));
  const routeKeys = new Set(goRoutes.map((route) => routeKey(route.method, route.path)));
  const coverageKeys = new Set(COVERAGE.map((entry) => routeKey(entry.method, entry.route)));

  assert.equal(goRoutes.length, routeKeys.size, 'product HTTP method/path pairs must be unique');
  const coverageCases = new Set(COVERAGE.map((entry) => `${entry.operation} ${routeKey(entry.method, entry.route)}`));
  assert.equal(COVERAGE.length, coverageCases.size, 'the MCP route coverage table must not contain duplicate operation/route cases');
  assert.deepEqual(
    [...coverageKeys].sort(),
    [...routeKeys].sort(),
    'update MCP operations and this coverage table whenever a product HTTP route changes',
  );

  const coveredOperations = new Set(COVERAGE.map((entry) => entry.operation));
  assert.deepEqual(
    [...coveredOperations].sort(),
    listOperations().map((operation) => operation.name).sort(),
    'every allowlisted MCP operation must have at least one route preview coverage case',
  );

  for (const entry of COVERAGE) {
    const operation = operationNamed(entry.operation);
    assert.notEqual(operation, undefined, `${entry.operation} must remain in the MCP operation allowlist`);
    if (operation === undefined) continue;
    const requests = previewRequests(operation.preview(entry.input));
    assert.equal(
      requests.some((request) => request.method === entry.method && pathMatchesRoute(request.path, entry.route)),
      true,
      `${entry.operation} preview must include ${entry.method} ${entry.route}; got ${JSON.stringify(requests)}`,
    );
  }
});
