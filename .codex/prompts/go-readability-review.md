# Go readability review for FragForge

Follow `AGENTS.md`.

Review only the current diff. Do not edit files.

Use these priorities:

1. clarity
2. simplicity
3. concision
4. maintainability
5. consistency with this repo

Look for:

- unclear names or package boundaries
- unnecessary interfaces or abstractions
- `util`/`common`/`helper` style dumping grounds
- Java/Spring/.NET architecture leaking into Go
- bad error wrapping or missing context
- logging and returning the same error
- panics used for normal control flow
- poor `context.Context` usage
- global mutable state
- public API/CLI behavior changes not called out
- generated media/data accidentally included

Use BLOCKER/WARNING/NIT. If clean, say `No blocking readability issues found.`
