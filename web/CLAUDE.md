# web/ frontend guidance

This file is loaded when working under `web/`; the repo-wide rules live in the root `CLAUDE.md`.

## Web frontend (web/)

`web/` is a standalone Next.js app (App Router, React 19, Tailwind 4): the no-login `/upload` entry, match/clip/video views, and a typed API client under `web/lib/api`.
It is local-first and stateless: it talks only to the orchestrator (`zv serve`) through same-origin proxy route handlers under `web/app/api/demos/*`, which forward `.dem` uploads and job calls while keeping the orchestrator URL and token server-side.
`web/lib/api` always uses the real typed client; same-origin route handlers keep the orchestrator URL and token server-side.

Finished Library reels expose a manual publication assistant through the per-artifact `/api/demos/*/publish-assistant` proxy. It generates Madrid-time guidance and factual reel-derived metadata, lets the user download the MP4, and opens only `https://studio.youtube.com/` in the system browser. Account, audience, visibility, scheduling, and the official upload flow remain entirely in YouTube Studio; FragForge has no Google account connection or direct publishing path.

Run it locally with the standard npm scripts in `web/package.json` (`dev`, `typecheck`, `lint`, `test:unit`).
The dev server needs the orchestrator on `127.0.0.1:8080`; the desktop/local-studio path uses persistent SQLite plus the inline queue.

Proxy-route contract: every `/api/demos/*` route reaches the orchestrator through `callOrchestrator` (`web/app/api/demos/_lib.ts`).
When the orchestrator is unreachable the route returns `503 {code: "service_unavailable"}` and logs the cause server-side, and the UI tells "service offline" apart from a bad demo via `SERVICE_UNAVAILABLE_CODE`.
Keep that contract when adding `/api/demos/*` routes; do not let a bare `fetch` throw into a code-less 500.

Real `.dem` files are never committed, so the fixture stays local.

## TypeScript style (web/)

Applies to everything under `web/` (Next.js App Router, React 19, Tailwind 4).
Adapted from the jvidalv/berrus agent guidelines.
Same priorities as the Go rules: clarity, simplicity, concision, maintainability, and repo consistency, in that order.

Full TypeScript, strict:

- The project is full TypeScript: no `.js`/`.jsx` source files, `strict: true` and `allowJs: false` stay on in `web/tsconfig.json`.
- `npm run typecheck` (`tsc --noEmit`) and `npm run lint` (oxlint) must pass before any change is considered done.
- Lint config lives in `web/.oxlintrc.json` (adapted from berrus).

Type safety:

- No `any`, ever: not explicit, not `as any`, not `<any>`, not `any[]`.
  If a type is genuinely unknown, use `unknown` and narrow it.
- No `!` non-null assertions.
  Handle the null case, or restructure so the type proves it.
- No `as <Type>` to silence the checker.
  A cast is acceptable only at a trust boundary where TypeScript cannot know the shape: `JSON.parse`, `await res.json()`, storage reads, `process.env`.
  Even there, cast to a named type (never `any`) and validate or narrow untrusted input before acting on it (see `lib/api/reel-store.ts` for the pattern).
- No `@ts-ignore`.
  `@ts-expect-error` only with a comment explaining the upstream cause.
- Exported functions declare explicit return types; local variables rely on inference.
- Keep exported APIs small; do not export a symbol only tests use.

Modules and imports:

- No re-exports: a module never re-exports a symbol it does not define.
  When moving code, update every import to the new location; do not leave "backwards compat" shims.
- Prefer direct file imports over barrels for heavy libraries.
  Exception: `lucide-react` uses barrel imports only (its direct paths lack type declarations).
- Shared API types live in `web/lib/api`; do not duplicate response shapes in components.

No magic strings:

- A string literal that crosses a boundary or repeats (an error code, a status value, a query param, a storage key) must be a named `const`, ideally an `as const` map with a derived union type, imported at every use site.
  `SERVICE_UNAVAILABLE_CODE` is the house example; inline duplicates of such strings are a review finding.

Async:

- Sequential `await` of independent operations is a performance bug; use `Promise.all`.
- Every `fetch` to the orchestrator goes through `callOrchestrator` (`web/app/api/demos/_lib.ts`) so failures map to `503 {code: "service_unavailable"}` instead of a code-less 500.

Server/client boundary:

- Secrets (orchestrator URL, tokens) stay server-side: route handlers and `server-only` modules.
  Never read them in a client component or ship them via `NEXT_PUBLIC_*`.
- Keep components server components unless they need state, effects, or browser APIs; add `"use client"` at the leaf, not the layout.

React:

- Derive types from data (`typeof x`, `as const` unions) instead of maintaining parallel enums.
- No `React.FC`; type props with an explicit interface or inline object type.
- Handle loading, error, and empty states explicitly in UI that fetches; keep response parsing and error mapping inside the typed `web/lib/api` boundary.

Testing:

- Unit tests are `lib/**/*.test.ts` on `node:test`, run with `npm run test:unit` (Node strips types natively; relative imports keep the `.ts` extension, allowed by `allowImportingTsExtensions`).
- Browser E2E/Playwright was removed by project policy; integration coverage lives in Go HTTP/worker tests and targeted manual smoke commands such as `scripts/smoke-xai-stt.ps1`.
- A test double for an external client (e.g. a fake `SupabaseClient`) types only the call surface it fakes and is cast once at creation with `as unknown as <ClientType>` plus a comment; that is the sole sanctioned use of a double cast.
- Bug fixes need a regression test, same as Go.
