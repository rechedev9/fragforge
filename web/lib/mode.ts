/**
 * FragForge web runs in one of three data-plane modes, selected by
 * NEXT_PUBLIC_FRAGFORGE_MODE (a public var, so it is readable identically on the
 * client and inside the server route handlers):
 *
 * - 'local' - the "local studio". The web talks only to a local orchestrator
 *   (`zv serve`) running on the same machine, which parses, records with
 *   HLAE/CS2, and renders. Everything is on the user's PC: no Supabase, no
 *   paired agent. The web is a thin same-origin proxy (`/api/demos/*`) to that
 *   orchestrator.
 * - 'cloud' - the hosted control-plane (default). Uploads and scan go to
 *   Supabase and a paired desktop agent does the work.
 * - 'hosted' - Topology A. The SPA is served from OUR HTTPS domain but the
 *   browser runs on the END USER's PC and talks DIRECTLY to a LOCAL agent (the
 *   Go orchestrator) at a configurable base URL (default http://127.0.0.1:8787),
 *   sending X-FragForge-Token on every request. Nothing heavy and no video ever
 *   transits our server: all job traffic bypasses the Next.js `/api/*` proxy and
 *   hits the agent's NATIVE routes. Our server serves only the SPA plus a small
 *   accounts DB. Capture-wise the agent runs on the same machine as the browser,
 *   so hosted behaves like local for the pipeline (see usesLocalAgent).
 *
 * The modes differ only in the data plane: which backend serves job traffic, and
 * how the client waits for a scan. Everything downstream (parse, record, render)
 * targets the orchestrator/agent in every mode.
 */
export type FragForgeMode = 'local' | 'cloud' | 'hosted';

/** The active data-plane mode; 'cloud' is the default when unset. */
export function getMode(): FragForgeMode {
  const raw = process.env.NEXT_PUBLIC_FRAGFORGE_MODE;
  if (raw === 'local' || raw === 'hosted') return raw;
  return 'cloud';
}

export function isLocalMode(): boolean {
  return getMode() === 'local';
}

export function isHostedMode(): boolean {
  return getMode() === 'hosted';
}

/**
 * True when the orchestrator/agent runs on the SAME machine as the browser
 * (local studio) or is reached directly by the browser as a local agent
 * (hosted). Both cases scan synchronously, show only the user's own reels, and
 * back capabilities off the agent - unlike cloud, which hands work to a remote
 * paired agent. Use this (not isLocalMode) for "the agent is local" branches.
 */
export function usesLocalAgent(): boolean {
  const mode = getMode();
  return mode === 'local' || mode === 'hosted';
}
