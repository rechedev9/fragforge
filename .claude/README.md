# Claude Code harness for FragForge

Repo-local harness for using Claude Code safely on FragForge.

Claude Code automatically loads `CLAUDE.md` from the repo root. That file holds
project boundaries, Go and TypeScript style, safety rules, and verification
expectations. All style and operational rules live directly in `CLAUDE.md`.

The project `.mcp.json` registers the local FragForge MCP server for Claude
Code. Start FragForge Studio and open Claude Code from the repository root so
the relative TypeScript entry resolves correctly. Use its `search` tool to
discover exact operation schemas and live IDs before `execute`.
Mutations require an explicit apply+confirmation pair. Full details are in
[`desktop/README.md`](../desktop/README.md#model-context-protocol-mcp).

## Interactive use

From WSL:

```bash
cd /mnt/c/Users/reche/Documents/FragForge
claude
```

Then use project slash commands:

```text
/zv-plan describe the change
/zv-tdd implement behavior with tests
/zv-bugfix fix bug with regression test
/zv-parser-change adjust parser/killplan behavior
/zv-media-change adjust editor/recording/FFmpeg behavior
/zv-worker-api-change adjust orchestrator/API/worker behavior
/zv-pr-ready
/zv-artifact-audit
/zv-toolchain-diagnose
```

Use repo-local reviewer agents directly when useful:

```text
@go-readability-reviewer review the current diff
@go-test-reviewer review the tests in this diff
@go-concurrency-reviewer review shared-state changes
@go-security-reviewer review filesystem/subprocess/security changes
@zv-media-pipeline-reviewer review FFmpeg/rendering changes
```

## Non-interactive wrappers

```bash
scripts/claude-run.sh .claude/commands/zv-plan.md "custom prompt run"
scripts/claude-zv-plan.sh "plan a small change"
scripts/claude-zv-tdd.sh "implement a behavior change"
scripts/claude-zv-bugfix.sh "fix a bug with a regression test"
scripts/claude-zv-parser-change.sh "change parser/killplan behavior"
scripts/claude-zv-media-change.sh "change editor/recording/FFmpeg behavior"
scripts/claude-zv-worker-api-change.sh "change orchestrator/API/worker behavior"
scripts/claude-zv-pr-ready.sh
scripts/claude-zv-artifact-audit.sh
scripts/claude-zv-toolchain-diagnose.sh
```

Useful environment variables:

```bash
CLAUDE_MODEL=sonnet scripts/claude-zv-tdd.sh "..."
CLAUDE_MAX_TURNS=18 scripts/claude-zv-tdd.sh "..."
CLAUDE_DRY_RUN=1 scripts/claude-zv-tdd.sh "preview prompt only"
CLAUDE_ALLOWED_TOOLS=Read,Bash scripts/claude-zv-artifact-audit.sh
```

## Safety defaults

- Read-only commands restrict tools to `Read,Bash,WebSearch,WebFetch`.
- Write-oriented commands allow `Read,Edit,Write,Bash,WebSearch,WebFetch`.
- `.claude/settings.json` allows normal Go checks and asks before dependency,
  git-history, Docker, migration, HLAE/CS2, and destructive operations.
- Do not use `--dangerously-skip-permissions` for this repo unless you have an
  external sandbox.
