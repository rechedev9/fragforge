/** @type {import('next').NextConfig} */
export default {
  // Emit a self-contained server bundle (.next/standalone) so the Docker runtime
  // image needs only node + the bundle, not the full node_modules tree.
  output: 'standalone',
};
