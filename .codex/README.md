# Codex harness for FragForge

Repo-local harness for using Codex CLI safely on FragForge.

Codex should automatically load `AGENTS.md` from the repo root. That file holds
project boundaries, Go style, safety rules, and verification expectations.

The trusted project config in `.codex/config.toml` also registers the local
FragForge MCP server. Start FragForge Studio, then launch Codex from the
repository root (for example, `codex --cd C:\Users\reche\Documents\zackvideo`);
the MCP's `cwd = "."` and entry path are intentionally root-relative. Then ask it
to search FragForge operations; MCP writes/capture/render/delete remain preview
only until explicitly applied and confirmed. Full details are in
[`desktop/README.md`](../desktop/README.md#model-context-protocol-mcp).

## Common commands

From WSL:

```bash
cd /mnt/c/Users/reche/Documents/FragForge

scripts/codex-run.sh .codex/prompts/go-tdd.md "custom prompt run"
scripts/codex-plan.sh "plan a small change"
scripts/codex-go-tdd.sh "implement a behavior change"
scripts/codex-go-bugfix.sh "fix a bug with a regression test"
scripts/codex-review-diff.sh
scripts/codex-go-pr-ready.sh
```

Focused read-only reviews:

```bash
scripts/codex-go-readability-review.sh
scripts/codex-go-test-review.sh
scripts/codex-go-concurrency-review.sh
scripts/codex-go-security-review.sh
```

Uncertain work:

```bash
scripts/codex-spike.sh "test whether approach X works"
```

## Safety defaults

- Write-oriented scripts use `workspace-write` sandbox and `on-request`
  approvals.
- Review and planning scripts use `read-only` sandbox and `never` approval by
  default.
- `scripts/go-gate.sh` formats changed Go files unless `--no-format` is passed.
  In a very dirty repo, use `--no-format` or format explicit files first.
- `scripts/go-gate.sh` also runs `zv check`, so repo-local skills,
  the workflow catalog, and workflow docs stay aligned with the unified CLI
  contract.
- `scripts/fix-loop.ps1` runs the same project check on Windows.
- `make test` runs the same project check for Unix-like local loops.
- `scripts/check-codex-harness.sh` runs the same project check when validating
  the Codex harness.

## Useful environment variables

```bash
CODEX_MODEL=gpt-5.1-codex scripts/codex-go-tdd.sh "..."
CODEX_PROFILE=work scripts/codex-go-tdd.sh "..."
CODEX_SEARCH=1 scripts/codex-plan.sh "research-dependent task"
CODEX_DRY_RUN=1 scripts/codex-go-tdd.sh "preview prompt only"
CODEX_OUTPUT_LAST_MESSAGE=/tmp/codex-last.md scripts/codex-review-diff.sh
```

Sandbox override examples:

```bash
CODEX_SANDBOX=read-only scripts/codex-go-tdd.sh "inspect only"
CODEX_SANDBOX=workspace-write CODEX_APPROVAL=on-request scripts/codex-spike.sh "..."
```

Do not use `danger-full-access` unless you have an external sandbox or you are
intentionally allowing Codex to touch the machine outside the repo.

## Local checks

```bash
scripts/check-codex-harness.sh
scripts/go-tools-check.sh
scripts/go-gate.sh --no-format
scripts/go-gate.sh --race --security --build
```

`go-tools-check.sh` verifies optional tools:

- `goimports`
- `staticcheck`
- `govulncheck`
- `gosec`

## Prompt playbooks

- `.codex/prompts/go-plan.md`: read-only implementation plan.
- `.codex/prompts/go-tdd.md`: behavior change with test first.
- `.codex/prompts/go-bugfix.md`: regression-test bug fix.
- `.codex/prompts/go-pr-ready.md`: final PR preparation.
- `.codex/prompts/review-diff.md`: full diff review.
- `.codex/prompts/go-readability-review.md`: Go readability review.
- `.codex/prompts/go-test-review.md`: test quality review.
- `.codex/prompts/go-concurrency-review.md`: race/leak/cancellation review.
- `.codex/prompts/go-security-review.md`: filesystem/subprocess/security review.
- `.codex/prompts/go-spike.md`: reversible experiment.

## Project skills

Repo-local skills live under `.codex/skills/`:

- `zackvideo-cheater-pov-reels`: create suspected-cheater reels by pairing killer POV before each target death.
- `zackvideo-cs2-utility-shorts`: parse, audit, record, render, and review CS2 utility Shorts.
- `zackvideo-lineup-audit`: correct utility destinations through manual lineup catalogs.
- `zackvideo-music-scripted-shorts`: create 24fps Lua-scripted Shorts with CC0 music and rhythm sync.
- `zackvideo-shorts-production`: generate, polish, and QA professional CS2 Shorts packs.
- `zackvideo-youtube-shorts-publish`: review publish packs, prepare YouTube Shorts metadata, and guide manual publication in YouTube Studio.

The unified CLI can discover the same repo-local skills:

```bash
./bin/zv skills list
./bin/zv skills show zackvideo-cheater-pov-reels
./bin/zv skills show zackvideo-cs2-utility-shorts
./bin/zv skills show zackvideo-lineup-audit
./bin/zv skills show zackvideo-music-scripted-shorts
./bin/zv skills show zackvideo-shorts-production
./bin/zv skills show zackvideo-youtube-shorts-publish
./bin/zv skills check
./bin/zv check
./bin/zv check --format json
./bin/zv skills list --format json
./bin/zv skills show zackvideo-cheater-pov-reels --format json
./bin/zv skills show zackvideo-cs2-utility-shorts --format json
./bin/zv skills show zackvideo-lineup-audit --format json
./bin/zv skills show zackvideo-music-scripted-shorts --format json
./bin/zv skills show zackvideo-shorts-production --format json
./bin/zv skills show zackvideo-youtube-shorts-publish --format json
./bin/zv skills check --format json
./bin/zv workflows list
./bin/zv workflows list --format json
./bin/zv workflows show demo-parse
./bin/zv workflows show demo-parse --format json
./bin/zv workflows show demo-players
./bin/zv workflows show demo-players --format json
./bin/zv workflows show utility-audit
./bin/zv workflows show utility-audit --format json
./bin/zv workflows show record
./bin/zv workflows show record --format json
./bin/zv workflows show compose-final
./bin/zv workflows show compose-final --format json
./bin/zv workflows show music-analyze
./bin/zv workflows show music-analyze --format json
./bin/zv workflows show shorts-render
./bin/zv workflows show shorts-render --format json
./bin/zv workflows show analysis-tactical-data
./bin/zv workflows show analysis-tactical-data --format json
./bin/zv workflows show analysis-viewer
./bin/zv workflows show analysis-viewer --format json
./bin/zv workflows show gallery-open
./bin/zv workflows show gallery-open --format json
./bin/zv workflows show serve
./bin/zv workflows show serve --format json
./bin/zv workflows show skills-check
./bin/zv workflows show skills-check --format json
./bin/zv workflows show workflows-check
./bin/zv workflows show workflows-check --format json
./bin/zv workflows show project-check
./bin/zv workflows show project-check --format json
./bin/zv demo parse --demo testdata/foo.dem --steamid 76561198000000000 --out plan.json
./bin/zv demo players --demo testdata/foo.dem
./bin/zv utility audit --plan plan-utility.json --lineup-catalog data/lineups --out utility-audit.csv
./bin/zv record --killplan plan.json --demo testdata/foo.dem --out data/runs/run-004/recording --hlae C:\HLAE-2.190.1\HLAE.exe --cs2 "C:\Games\Counter-Strike 2\game\bin\win64\cs2.exe"
./bin/zv compose final --recording-result data/runs/run-004/recording/recording-result.json --out data/runs/run-004/final.mp4
./bin/zv music analyze --input data/music/track.mp4 --out data/runs/run-004/rhythm.json
./bin/zv shorts render --recording-result data/runs/run-004/recording/recording-result.json --out data/runs/run-004/shorts
./bin/zv analysis tactical-data --demo testdata/foo.dem --out data/runs/run-004/tactical.json --start 1000 --end 2000
./bin/zv analysis view --json data/analysis/MarcusN1-deaths.json
./bin/zv gallery open --path data/runs/run-004/shorts/publish/index.html
./bin/zv serve
./bin/zv workflows run demo-parse -- --demo testdata/foo.dem --steamid 76561198000000000 --out plan.json
./bin/zv workflows run demo-players -- --demo testdata/foo.dem
./bin/zv workflows run utility-audit -- --plan plan-utility.json --lineup-catalog data/lineups --out utility-audit.csv
./bin/zv workflows run record -- --killplan plan.json --demo testdata/foo.dem --out data/runs/run-004/recording --hlae C:\HLAE-2.190.1\HLAE.exe --cs2 "C:\Games\Counter-Strike 2\game\bin\win64\cs2.exe"
./bin/zv workflows run compose-final -- --recording-result data/runs/run-004/recording/recording-result.json --out data/runs/run-004/final.mp4
./bin/zv workflows run music-analyze -- --input data/music/track.mp4 --out data/runs/run-004/rhythm.json
./bin/zv workflows run shorts-render -- --recording-result data/runs/run-004/recording/recording-result.json --out data/runs/run-004/shorts
./bin/zv workflows run analysis-tactical-data -- --demo testdata/foo.dem --out data/runs/run-004/tactical.json --start 1000 --end 2000
./bin/zv workflows run analysis-viewer -- --json data/analysis/MarcusN1-deaths.json
./bin/zv workflows run gallery-open -- --path data/runs/run-004/shorts/publish/index.html
./bin/zv workflows run serve
./bin/zv workflows run skills-check
./bin/zv workflows run skills-check -- --format json
./bin/zv workflows run workflows-check
./bin/zv workflows run workflows-check -- --format json
./bin/zv workflows run project-check
./bin/zv workflows run project-check -- --format json
./bin/zv workflows check
./bin/zv workflows check --format json
```

`zv check` is the full project contract. It validates repo-local skills, the
workflow catalog, and the active docs/scripts that document the CLI.
`workflows list --format json` and `workflows show <name> --format json`
include `command` for the direct canonical command and `run_command` for the
standard workflow entrypoint automation should execute.

## Verify AGENTS.md loading

```bash
codex --cd . debug prompt-input "test" | grep -i FragForge
```

Or run:

```bash
scripts/check-codex-harness.sh
```
