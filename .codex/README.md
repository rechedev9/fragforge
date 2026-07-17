# Codex app and CLI harness for FragForge

Repo-local harness for using Codex Desktop or Codex CLI safely on FragForge.

Codex should automatically load `AGENTS.md` from the repo root. That file holds
project boundaries, Go style, safety rules, and verification expectations.

## Product operation from Codex Desktop

The unified Windows CLI is the primary interface. Open
`C:\Users\reche\Documents\zackvideo` as a folder in Codex Desktop, select
Codex, and ask for the desired FragForge result. Studio is not a prerequisite.
Codex follows this machine-readable loop:

```powershell
.\bin\zv.exe capabilities --format json
.\bin\zv.exe flows show demo --format json
.\bin\zv.exe flows show stream --format json
.\bin\zv.exe workflows list --format json
.\bin\zv.exe workflows show short --format json
.\bin\zv.exe workflows validate short --format json -- match.dem --prompt "all kills 76561198000000000" --dry-run --format json
.\bin\zv.exe workflows run short -- match.dem --prompt "all kills 76561198000000000" --dry-run --format json
```

Run `.\scripts\build.ps1` first when `bin\zv.exe` is missing or stale. Keep
`--dry-run --format json` for planning; remove both flags only when the user
requested the real capture/render. Real execution streams human-readable stage
progress. Task-specific skills under `.codex/skills/` use the same CLI for
granular workflows and QA.

`flows show` is the first-stop journey guide: it exposes each decision boundary,
safe/dry-run command, expensive stage, artifact, and both delivery profiles.
For demos where the user chooses plays, use `demo players -> demo parse -> demo
moments -> demo select -> record -> shorts render`. For streams use `stream
variants -> stream plan -> stream killfeed -> stream transcribe -> review -> stream captions -> stream render`.
Do not skip the selection/review boundary before HLAE or a paid caption/render
pass. Reviewed Spanish word timings make the caption stage credential-free;
xAI is only the automatic transcription and translation fallback.

The demo journey also exposes two agent gates. `creative-brief` asks only for
unanswered format, HUD/killfeed, effect, transition, kill-numbering,
intro/outro, music, and thumbnail choices before expensive work.
`thumbnail-selection` applies when covers are enabled, shows generated
candidates, and requires a selection or an explicit delegation before the pack
is considered upload-ready. With `--covers=false`, there is no thumbnail gate.

The JSON dry-run is one resolved document with `executed: false`, exact stage
argv, and output paths. Real `short` and `record` calls auto-fill missing
HLAE/CS2 paths from the same detection shown by `capabilities`; do not repeat
those paths in agent-generated commands unless overriding detection.
Detection selects the highest installed numeric HLAE version, and capture work
must keep it aligned with the latest official AdvancedFX release.
The `output.publish_dir` field points to the required upload-ready
`<run>\shortslistosparasubir` folder; `output.shorts_dir` contains intermediates.

The project-local FragForge MCP registration is intentionally disabled in
`.codex/config.toml`. It is an optional adapter for work against a running
Studio queue/UI. Set its `enabled` field to `true`, start Studio, and open a new
Codex session only for that use case. Full details are in
[`desktop/README.md`](../desktop/README.md#model-context-protocol-mcp).

## Common commands

From WSL:

```bash
cd /mnt/c/Users/reche/Documents/zackvideo

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
- `zackvideo-stream-clips`: plan, caption, and render stream VOD clips after the creative brief gate.
- `zackvideo-youtube-shorts-publish`: review publish packs, prepare YouTube Shorts metadata, and guide manual publication in YouTube Studio.

The unified CLI can discover the same repo-local skills:

```bash
./bin/zv skills list
./bin/zv skills show zackvideo-cheater-pov-reels
./bin/zv skills show zackvideo-cs2-utility-shorts
./bin/zv skills show zackvideo-lineup-audit
./bin/zv skills show zackvideo-music-scripted-shorts
./bin/zv skills show zackvideo-shorts-production
./bin/zv skills show zackvideo-stream-clips
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
./bin/zv skills show zackvideo-stream-clips --format json
./bin/zv skills show zackvideo-youtube-shorts-publish --format json
./bin/zv skills check --format json
./bin/zv workflows list
./bin/zv workflows list --format json
./bin/zv flows list --format json
./bin/zv flows show demo --format json
./bin/zv flows show stream --format json
./bin/zv workflows show short
./bin/zv workflows show short --format json
./bin/zv workflows show capabilities
./bin/zv workflows show capabilities --format json
./bin/zv workflows show demo-parse
./bin/zv workflows show demo-parse --format json
./bin/zv workflows show demo-players
./bin/zv workflows show demo-players --format json
./bin/zv workflows show demo-moments
./bin/zv workflows show demo-moments --format json
./bin/zv workflows show demo-select
./bin/zv workflows show demo-select --format json
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
./bin/zv workflows show stream-variants
./bin/zv workflows show stream-variants --format json
./bin/zv workflows show stream-plan
./bin/zv workflows show stream-plan --format json
./bin/zv workflows show stream-killfeed
./bin/zv workflows show stream-killfeed --format json
./bin/zv workflows show stream-transcribe
./bin/zv workflows show stream-transcribe --format json
./bin/zv workflows show stream-captions
./bin/zv workflows show stream-captions --format json
./bin/zv workflows show stream-render
./bin/zv workflows show stream-render --format json
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
./bin/zv workflows validate short --format json -- testdata/foo.dem --prompt "all kills 76561198000000000" --dry-run
./bin/zv workflows validate demo-parse --format json -- --demo testdata/foo.dem --steamid 76561198000000000 --segment-mode utility --out plan.json
./bin/zv workflows validate record --format json -- --killplan plan.json --demo testdata/foo.dem --out data/runs/run-004/recording --dry-run --hud deathnotices
./bin/zv workflows validate stream-plan --format json -- --input stream.mp4 --out data/runs/stream/edit-plan.json --captions --dry-run
./bin/zv workflows validate stream-transcribe --format json -- --input stream.mp4 --plan data/runs/stream/reviewed-plan.json --model data/models/whisper/ggml-large-v3.bin --vad-model data/models/whisper/ggml-silero-v6.2.0.bin --out data/runs/stream/transcript-review.json --dry-run
./bin/zv workflows validate stream-captions --format json -- --plan data/runs/stream/reviewed-plan.json --words testdata/stream-caption-words.json --out data/runs/stream/final-plan.json --dry-run
./bin/zv workflows validate stream-render --format json -- --input stream.mp4 --plan data/runs/stream/edit-plan.json --out data/runs/stream --dry-run
./bin/zv short testdata/foo.dem --prompt "all kills 76561198000000000" --dry-run
./bin/zv capabilities --format json
./bin/zv demo parse --demo testdata/foo.dem --steamid 76561198000000000 --out plan.json
./bin/zv demo players --demo testdata/foo.dem
./bin/zv demo moments --killplan testdata/agent-killplan.json --format json
./bin/zv demo select --killplan testdata/agent-killplan.json --segments seg-001 --out data/runs/agent-doc/selected-plan.json --dry-run --format json
./bin/zv utility audit --plan plan-utility.json --lineup-catalog data/lineups --out utility-audit.csv
./bin/zv record --killplan plan.json --demo testdata/foo.dem --out data/runs/run-004/recording
./bin/zv compose final --recording-result data/runs/run-004/recording/recording-result.json --out data/runs/run-004/final.mp4
./bin/zv music analyze --input data/music/track.mp4 --out data/runs/run-004/rhythm.json
./bin/zv shorts render --recording-result data/runs/run-004/recording/recording-result.json --out data/runs/run-004/shorts --publish-dir data/runs/run-004/shortslistosparasubir
./bin/zv stream variants
# Independent preflight examples; these do not create their --out artifacts.
./bin/zv stream plan --input stream.mp4 --out data/runs/stream/edit-plan.json --captions --dry-run
./bin/zv stream killfeed --plan data/runs/stream/edit-plan.json --events testdata/stream-killfeed-events.json --out data/runs/stream/reviewed-plan.json --dry-run --format json
./bin/zv stream transcribe --input stream.mp4 --plan data/runs/stream/reviewed-plan.json --model data/models/whisper/ggml-large-v3.bin --model data/models/whisper/ggml-large-v3-turbo-q5_0.bin --vad-model data/models/whisper/ggml-silero-v6.2.0.bin --out data/runs/stream/transcript-review.json --dry-run --format json
./bin/zv stream captions --plan data/runs/stream/reviewed-plan.json --words testdata/stream-caption-words.json --out data/runs/stream/final-plan.json --dry-run --format json
./bin/zv stream render --input stream.mp4 --plan data/runs/stream/final-plan.json --out data/runs/stream --dry-run
# Persist the approved stream chain in order before the real render.
./bin/zv stream plan --input stream.mp4 --out data/runs/stream/edit-plan.json --captions --killfeed-crop 0.82,0.05,0.17,0.18 --detect-killfeed
# Review the detected cue frames and save matching factual events here first.
./bin/zv stream killfeed --plan data/runs/stream/edit-plan.json --events data/runs/stream/killfeed-events.json --out data/runs/stream/reviewed-plan.json
# Generate local multi-model candidates; the output remains requires_review.
./bin/zv stream transcribe --input stream.mp4 --plan data/runs/stream/reviewed-plan.json --model data/models/whisper/ggml-large-v3.bin --model data/models/whisper/ggml-large-v3-turbo-q5_0.bin --vad-model data/models/whisper/ggml-silero-v6.2.0.bin --out data/runs/stream/transcript-review.json
# Compare every pass, then save only verified Spanish word timings.
./bin/zv stream captions --plan data/runs/stream/reviewed-plan.json --words data/runs/stream/caption-words.json --out data/runs/stream/final-plan.json
./bin/zv stream render --input stream.mp4 --plan data/runs/stream/final-plan.json --out data/runs/stream
./bin/zv analysis tactical-data --demo testdata/foo.dem --out data/runs/run-004/tactical.json --start 1000 --end 2000
./bin/zv analysis view --json data/analysis/MarcusN1-deaths.json
./bin/zv gallery open --path data/runs/run-004/shortslistosparasubir/index.html
./bin/zv serve
./bin/zv workflows run short -- testdata/foo.dem --prompt "all kills 76561198000000000" --dry-run
./bin/zv workflows run capabilities -- --format json
./bin/zv workflows run demo-parse -- --demo testdata/foo.dem --steamid 76561198000000000 --out plan.json
./bin/zv workflows run demo-players -- --demo testdata/foo.dem
./bin/zv workflows run demo-moments -- --killplan testdata/agent-killplan.json --format json
./bin/zv workflows run demo-select -- --killplan testdata/agent-killplan.json --segments seg-001 --out data/runs/agent-doc/selected-plan.json --dry-run --format json
./bin/zv workflows run utility-audit -- --plan plan-utility.json --lineup-catalog data/lineups --out utility-audit.csv
./bin/zv workflows run record -- --killplan plan.json --demo testdata/foo.dem --out data/runs/run-004/recording
./bin/zv workflows run compose-final -- --recording-result data/runs/run-004/recording/recording-result.json --out data/runs/run-004/final.mp4
./bin/zv workflows run music-analyze -- --input data/music/track.mp4 --out data/runs/run-004/rhythm.json
./bin/zv workflows run shorts-render -- --recording-result data/runs/run-004/recording/recording-result.json --out data/runs/run-004/shorts --publish-dir data/runs/run-004/shortslistosparasubir
./bin/zv workflows run stream-variants
./bin/zv workflows run stream-plan -- --input stream.mp4 --out data/runs/stream/edit-plan.json --captions --dry-run
./bin/zv workflows run stream-killfeed -- --plan data/runs/stream/edit-plan.json --events testdata/stream-killfeed-events.json --out data/runs/stream/reviewed-plan.json --dry-run --format json
./bin/zv workflows run stream-transcribe -- --input stream.mp4 --plan data/runs/stream/reviewed-plan.json --model data/models/whisper/ggml-large-v3.bin --vad-model data/models/whisper/ggml-silero-v6.2.0.bin --out data/runs/stream/transcript-review.json --dry-run
./bin/zv workflows run stream-captions -- --plan data/runs/stream/reviewed-plan.json --words testdata/stream-caption-words.json --out data/runs/stream/final-plan.json --dry-run
./bin/zv workflows run stream-render -- --input stream.mp4 --plan data/runs/stream/final-plan.json --out data/runs/stream --dry-run
./bin/zv workflows run analysis-tactical-data -- --demo testdata/foo.dem --out data/runs/run-004/tactical.json --start 1000 --end 2000
./bin/zv workflows run analysis-viewer -- --json data/analysis/MarcusN1-deaths.json
./bin/zv workflows run gallery-open -- --path data/runs/run-004/shortslistosparasubir/index.html
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
include `command` for the direct canonical command, `run_command` for execution,
and `validate_command` for zero-side-effect preflight. The `arguments` object
describes positionals, every required/value/boolean flag, conditional
requirements, and enum-like allowed values/defaults; `safety` describes
read-only, dry-run, and long-running behavior. These fields are derived from the
same command contract enforced at execution time, so an agent does not need to
guess from prose or probe the CLI with invalid calls. A JSON preflight always
reports `scope: "arguments"` and `executed: false`; runtime tool/file readiness
is intentionally outside that claim.

## Verify AGENTS.md loading

```bash
codex --cd . debug prompt-input "test" | grep -i FragForge
```

Or run:

```bash
scripts/check-codex-harness.sh
```
