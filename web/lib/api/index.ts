import { RealApiClient } from './real';
import type { ApiClient } from './client';

// The RealApiClient drives the real upload→parse→record→render path through
// the same-origin /api/demos/* route handlers, which proxy the whole pipeline
// to the local orchestrator bundled with the desktop app.
export const api: ApiClient = new RealApiClient();
