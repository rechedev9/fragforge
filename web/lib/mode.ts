/**
 * FragForge web runs in one of two data-plane modes, selected by
 * NEXT_PUBLIC_FRAGFORGE_MODE (a public var, so it is readable identically on the
 * client and inside the server route handlers):
 *
 * - 'local' - the "local studio". The web talks only to a local orchestrator
 *   (`zv serve`) running on the same machine, which parses, records with
 *   HLAE/CS2, and renders. Everything is on the user's PC: no Supabase, no
 *   paired agent. This is the mode that lets an end user drive local HLAE+CS2
 *   capture straight from the web UI.
 * - 'cloud' - the hosted control-plane (default). Uploads and scan go to
 *   Supabase and a paired desktop agent does the work.
 *
 * The two modes differ only in the data plane: which backend the `/api/demos/*`
 * routes proxy to, and how the client waits for a scan. Everything downstream
 * (parse, record, render) already proxies to the orchestrator in both modes.
 */
export function isLocalMode(): boolean {
  return process.env.NEXT_PUBLIC_FRAGFORGE_MODE === 'local';
}
