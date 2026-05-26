# Go test review for ZackVideo

Follow `AGENTS.md`.

Review tests in the current diff. Do not edit files unless explicitly asked.

Look for:

- bug fixes without regression tests
- behavior changes without tests
- missing edge cases around demo parsing, segment windows, FFmpeg command
  construction, artifact paths, worker idempotency, and publish metadata
- brittle tests tied to implementation details
- tests requiring real CS2/HLAE/large media when a pure unit test would work
- table tests that are too broad or too clever
- flaky timing/concurrency tests
- missing `t.Helper()` in helpers
- unhelpful failure messages or reversed got/want ordering
- excessive shared setup

Prefer concrete test additions over generic coverage advice.
