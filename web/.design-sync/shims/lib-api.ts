// Design-sync shim for `@/lib/api`. Production always uses RealApiClient, while
// standalone previews have no local orchestrator. Bind MockApiClient so previews
// stay deterministic; feature components only call `api` in event handlers.
import { MockApiClient } from '../../lib/api/mock';

export const api = new MockApiClient();
export type { ApiClient } from '../../lib/api/client';
export * from '../../lib/api/types';
