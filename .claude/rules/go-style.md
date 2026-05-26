# Go style rule

Use Google-style Go: clarity, simplicity, concision, maintainability, and repo
consistency, in that order.

- Keep packages cohesive and names boring.
- Avoid `util`, `common`, `helper`, `manager`, and generic service layers.
- Prefer concrete types. Introduce interfaces at the consumer side only when
  they buy a real seam.
- Return errors with context; do not panic for normal control flow.
- Use lowercase error strings without trailing punctuation.
- Keep exported APIs narrow and documented.
- Use table tests for business logic and regression tests for bug fixes.
- Respect context cancellation around subprocesses, DB, Redis, HTTP, and workers.
- Every goroutine needs an owner, a stop condition, and a test strategy.
