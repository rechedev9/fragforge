import { MockApiClient } from './mock';
import { RealApiClient } from './real';
import type { ApiClient } from './client';
import { isLocalMode } from '@/lib/mode';

// The RealApiClient (same ApiClient interface) drives the real upload→parse→
// record→render path through the same-origin /api/demos/* route handlers. It is
// selected when NEXT_PUBLIC_API_BASE is set (cloud) or in local-studio mode,
// where the routes proxy the whole pipeline to a local orchestrator. Otherwise
// the in-memory mock runs (the design/preview default).
export const api: ApiClient =
  process.env.NEXT_PUBLIC_API_BASE || isLocalMode() ? new RealApiClient() : new MockApiClient();

export type { ApiClient } from './client';
export * from './types';
