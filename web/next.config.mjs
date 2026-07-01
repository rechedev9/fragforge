import { fileURLToPath } from 'node:url';
import { dirname } from 'node:path';

const here = dirname(fileURLToPath(import.meta.url));

/** @type {import('next').NextConfig} */
export default {
  // Emit a self-contained server bundle (.next/standalone) so the Docker runtime
  // image needs only node + the bundle, not the full node_modules tree.
  output: 'standalone',
  // Pin the file-tracing root to this app. Without it, Next walks up and can pick
  // a parent package.json (e.g. a stray ~/package.json) as the workspace root,
  // which nests the standalone output under the build machine's absolute path
  // (…/standalone/<abs/path>/web/server.js) instead of a flat …/standalone/
  // server.js. The desktop packaging and the Docker image both rely on the flat
  // layout, so make it deterministic regardless of where the repo is checked out.
  outputFileTracingRoot: here,
};
