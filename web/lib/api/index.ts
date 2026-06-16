import { MockApiClient } from './mock';
import { RealApiClient } from './real';
import type { ApiClient } from './client';

// Fase 1 runs entirely against the in-memory mock. In Fase 2, when
// NEXT_PUBLIC_API_BASE is set, the RealApiClient (same ApiClient interface)
// drives the real upload→parse path through the same-origin /api/demos/* route
// handlers; everything else still delegates to the mock. Default stays mock.
export const api: ApiClient = process.env.NEXT_PUBLIC_API_BASE
  ? new RealApiClient()
  : new MockApiClient();

export type { ApiClient } from './client';
export * from './types';
