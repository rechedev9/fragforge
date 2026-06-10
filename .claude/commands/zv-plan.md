Plan a FragForge change. Task: $ARGUMENTS

Read-only. Do not edit files.

Process:

1. Run `git status --short`.
2. Read `CLAUDE.md` and the files/tests/docs relevant to the task.
3. Identify affected boundaries: parser, killplan, lineups, recording, FFmpeg,
   editor, worker/API, storage, generated artifacts, dependencies, or external
   tools.
4. Propose a small implementation plan with exact files, test strategy, and
   commands to verify.
5. Call out risks and assumptions. If real HLAE/CS2/FFmpeg/Docker/DB work is
   needed, mark it explicitly as requiring approval.

Output:

- Goal
- Current behavior / evidence
- Proposed changes
- Tests and verification
- Risks / open questions
