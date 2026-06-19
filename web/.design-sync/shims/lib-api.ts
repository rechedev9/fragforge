// design-sync shim for `@/lib/api` — the app picks Real vs Mock client at module
// load via process.env.NEXT_PUBLIC_API_BASE, which is undefined in the standalone
// DS bundle (no `process`). Always bind the in-memory MockApiClient so the bundle
// evaluates cleanly; feature components only call `api` in event handlers, so
// previews render identically. Re-exports the same surface as the real index.
import { MockApiClient } from '../../lib/api/mock';

export const api = new MockApiClient();
export type { ApiClient } from '../../lib/api/client';
export * from '../../lib/api/types';
