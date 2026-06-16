import { MockApiClient } from './mock';
import type { ApiClient } from './client';

// Fase 1 runs entirely against the in-memory mock. In Fase 2, when
// NEXT_PUBLIC_API_BASE is set, swap this for a RealApiClient that implements the
// same ApiClient interface so no screen code has to change.
export const api: ApiClient = new MockApiClient();

export type { ApiClient } from './client';
export * from './types';
