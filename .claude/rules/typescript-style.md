# TypeScript style rule (web/)

Applies to everything under `web/` (Next.js App Router, React 19, Tailwind 4, Playwright).
Same priorities as the Go rule: clarity, simplicity, concision, maintainability, and repo consistency, in that order.
Adapted from the jvidalv/berrus project guidelines.

## Full TypeScript, strict

- The project is full TypeScript: no `.js`/`.jsx` source files, `strict: true` and `allowJs: false` stay on in `web/tsconfig.json`.
- `npm run typecheck` (`tsc --noEmit`) must pass before any change is considered done.

## Type safety

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

## Modules and imports

- No re-exports: a module never re-exports a symbol it does not define.
  When moving code, update every import to the new location; do not leave "backwards compat" shims.
- Prefer direct file imports over barrels for heavy libraries.
  Exception: `lucide-react` uses barrel imports only (its direct paths lack type declarations).
- Shared API types live in `web/lib/api`; do not duplicate response shapes in components.

## No magic strings

- A string literal that crosses a boundary or repeats (an error code, a status value, a query param, a storage key) must be a named `const`, ideally an `as const` map with a derived union type, imported at every use site.
  `SERVICE_UNAVAILABLE_CODE` is the house example; inline duplicates of such strings are a review finding.

## Async

- Sequential `await` of independent operations is a performance bug; use `Promise.all`.
- Every `fetch` to the orchestrator goes through `callOrchestrator` (`web/app/api/demos/_lib.ts`) so failures map to `503 {code: "service_unavailable"}` instead of a code-less 500.

## Server/client boundary

- Secrets (orchestrator URL, tokens) stay server-side: route handlers and `server-only` modules.
  Never read them in a client component or ship them via `NEXT_PUBLIC_*`.
- Keep components server components unless they need state, effects, or browser APIs; add `"use client"` at the leaf, not the layout.

## React

- Derive types from data (`typeof x`, `as const` unions) instead of maintaining parallel enums.
- No `React.FC`; type props with an explicit interface or inline object type.
- Handle loading, error, and empty states explicitly in UI that fetches; the mock/real client split in `web/lib/api` exists so both paths stay typed.

## Tests

- E2E lives in `web/e2e` on `@playwright/test`; specs are TypeScript under the same no-`any` rules.
- Bug fixes need a regression test, same as Go.
