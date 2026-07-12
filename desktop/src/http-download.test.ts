import test from 'node:test';
import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { getEventListeners } from 'node:events';
import * as fs from 'node:fs';
import * as http from 'node:http';
import * as os from 'node:os';
import * as path from 'node:path';
import { downloadFile } from './http-download.ts';

test('downloads through a redirect, reports progress, and returns the SHA-256', async (t) => {
  const content = Buffer.from('fragforge runtime asset');
  const server = http.createServer((request, response) => {
    if (request.url === '/redirect') {
      response.writeHead(302, { location: '/asset' });
      response.end();
      return;
    }
    response.writeHead(200, { 'content-length': String(content.length) });
    response.end(content);
  });
  await listen(server);
  t.after(() => server.close());

  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'fragforge-download-'));
  t.after(() => fs.rmSync(directory, { recursive: true, force: true }));
  const destination = path.join(directory, 'asset.bin');
  const progress: Array<[number, number | undefined]> = [];
  const controller = new AbortController();

  const digest = await downloadFile(`${serverUrl(server)}/redirect`, destination, {
    signal: controller.signal,
    onProgress: (received, total) => progress.push([received, total]),
  });

  assert.equal(fs.readFileSync(destination, 'utf8'), content.toString());
  assert.equal(digest, createHash('sha256').update(content).digest('hex'));
  assert.deepEqual(progress.at(-1), [content.length, content.length]);
  assert.equal(fs.existsSync(`${destination}.tmp`), false);
  assert.equal(getEventListeners(controller.signal, 'abort').length, 0);
});

test('rejects an unsuccessful response without publishing a destination', async (t) => {
  const server = http.createServer((_request, response) => {
    response.writeHead(503);
    response.end('offline');
  });
  await listen(server);
  t.after(() => server.close());

  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'fragforge-download-'));
  t.after(() => fs.rmSync(directory, { recursive: true, force: true }));
  const destination = path.join(directory, 'asset.bin');
  fs.writeFileSync(`${destination}.tmp`, 'stale partial download');

  await assert.rejects(downloadFile(`${serverUrl(server)}/asset`, destination), /HTTP 503/);
  assert.equal(fs.existsSync(destination), false);
  assert.equal(fs.existsSync(`${destination}.tmp`), false);
});

function listen(server: http.Server): Promise<void> {
  return new Promise((resolve, reject) => {
    server.once('error', reject);
    server.listen(0, '127.0.0.1', resolve);
  });
}

function serverUrl(server: http.Server): string {
  const address = server.address();
  if (address === null || typeof address === 'string') {
    throw new Error('test server has no TCP address');
  }
  return `http://127.0.0.1:${address.port}`;
}
