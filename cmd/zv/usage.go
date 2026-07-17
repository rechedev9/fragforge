package main

const usage = `zv - deterministic CS2 demo-to-video workflows

Usage:
  zv short <demo.dem> --prompt "<instruction>" [--output-format short-9x16|landscape-16x9] [--kill-effect <style>] [--transition <style>] [--preset <name>] [--out <dir>] [--music <audio>] [--target-steamid <SteamID64>] [--from-recording <recording-result.json>] [--dry-run] [--format text|json]
  zv batch <dir> [--recursive] [--steamid <id>] [--out <dir>] [--jobs <n>] [--format text|json] [--report <path>]
  zv metrics [--reset]
  zv errors [--tail <n>] [--json] [--clear]
  zv presets [--format text|json]
  zv capabilities [--format text|json]
  zv demo parse [zv-parser parse flags]
  zv demo players [zv-demo-players flags]
  zv demo moments --killplan <plan.json> [--top <n>] [--out <moments.json>] [--dry-run] [--format text|json]
  zv demo select --killplan <plan.json> --segments <ids> --out <selected-plan.json> [--dry-run] [--format text|json]
  zv utility audit [zv-parser utility-audit flags]
  zv record [zv-recorder flags]
  zv compose final [zv-composer flags]
  zv shorts render [zv-editor flags]
  zv stream variants [--format text|json]
  zv stream plan --input <stream.mp4> --out <edit-plan.json> [--captions] [--dry-run] [--format text|json]
  zv stream killfeed --plan <edit-plan.json> --events <killfeed-events.json> --out <reviewed-plan.json> [--dry-run] [--format text|json]
  zv stream transcribe --input <stream.mp4> --plan <edit-plan.json> --model <ggml-model.bin> --vad-model <ggml-vad.bin> --out <transcript-review.json> [--dry-run] [--format text|json]
  zv stream captions --plan <edit-plan.json> --words <caption-words.json> --out <captioned-plan.json> [--dry-run] [--format text|json]
  zv stream render --input <stream.mp4> --plan <edit-plan.json> --out <run-dir> [--dry-run] [--format text|json]
  zv music analyze [zv-rhythm analyze flags]
  zv analysis tactical-data [zv-tactical-data flags]
  zv analysis view [zv-analysis-viewer flags]
  zv gallery open --path <index.html>
  zv check
  zv skills list
  zv skills show <name>
  zv skills check
  zv workflows list
  zv workflows show <name>
  zv workflows validate <name> [--format text|json] -- [workflow flags]
  zv workflows run <name> -- [workflow flags]
  zv workflows check
  zv flows list [--format text|json]
  zv flows show <demo|stream> [--format text|json]
  zv flows run <demo|stream> --run-dir <dir> --dry-run [--format text|json]
  zv serve
  zv tui [--url <orchestrator>] [--token <token>]

Legacy pass-throughs:
  zv parser [zv-parser args]
  zv editor [zv-editor args]
  zv recorder [zv-recorder args]
  zv composer [zv-composer args]
  zv orchestrator [zv-orchestrator args]
  zv analysis-viewer [zv-analysis-viewer args]
  zv tactical-data [zv-tactical-data args]
  zv rhythm [zv-rhythm args]
  zv tui [zv-tui args]

Use "zv <command> --help" for the underlying command help.
`

const shortUsage = `usage: zv short <demo.dem> --prompt "<instruction>" [flags]

One command from demo to an upload-ready vertical Short or 16:9 long-form video:
parse -> record (HLAE/CS2) -> [music analyze] -> render + publish pack.

Flags:
  --prompt <text>            editing instruction (Spanish or English); required
  --preset <name>            render preset; overrides the prompt (see zv presets)
  --out <dir>                run output directory; defaults under data/runs
  --music <audio>            music file; required for beat-synced shorts
  --target-steamid <id>      target player SteamID64 when the prompt only names a player
  --hlae <HLAE.exe>          HLAE path; defaults to env or local autodetection
  --cs2 <cs2.exe>            CS2 path; defaults to env or Steam autodetection
  --from-recording <json>    existing recording-result.json; skips parse and record
  --output-format <format>   short-9x16 (TikTok/Shorts) or landscape-16x9 (YouTube)
  --kill-effect <style>      clean, punch-in, velocity, or freeze-flash
  --transition <style>       cut, flash, whip, or dip
  --intro / --outro          add professional title bookends
  --dry-run                  print the resolved plan without launching HLAE/CS2 or FFmpeg
  --format <text|json>       dry-run plan format (default text; JSON requires --dry-run)

Prompt rules (deterministic keywords, no model calls):
  "todas las kills" / "all kills"        one compiled Short with every kill (default)
  "mejores" / "best" / "highlights"      best-moments compilation (top segments)
  "música" / "music" / "beat" / "ritmo"  beat-synced edit; needs --music
  a SteamID64 in the prompt              selects the target player
  a preset name in the prompt            selects that preset
  "16:9" / "horizontal" / "video largo" selects landscape-16x9
`

const presetsUsage = `usage: zv presets [--format text|json]
`

const capabilitiesUsage = `usage: zv capabilities [--format text|json]
`

const batchUsage = `usage: zv batch <dir> [flags]

Parse every .dem under <dir> in-process and record each failure to the local
error journal, so a folder of demos can be exercised without driving the CLI
once per demo. Exit code is non-zero when any demo failed.

Flags:
  --recursive            descend into subdirectories
  --steamid <id>         target SteamID64 for every demo; default auto-picks the top fragger
  --out <dir>            optional directory to write each kill plan into
  --obs-dir <dir>        observability directory (default data/obs or $ZV_DATA_DIR/obs)
  --jobs <n>             max concurrent demos; 0 picks a CPU-based default
  --segment-mode <mode>  kills, smokes, or utility (default kills)
  --format text|json     summary format (default text)
  --report <path>        also write the JSON summary report to <path>
`

const metricsUsage = `usage: zv metrics [--obs-dir <dir>] [--reset]

Print the local pipeline counters in Prometheus text format. --reset clears them.
`

const errorsUsage = `usage: zv errors [--obs-dir <dir>] [--tail <n>] [--json] [--clear]

Summarize the local error journal. --clear truncates it (use between fix-loop runs).
`

const demoUsage = `usage: zv demo parse [zv-parser parse flags] | zv demo players [zv-demo-players flags] | zv demo moments [flags] | zv demo select [flags]
`

const demoMomentsUsage = `usage: zv demo moments --killplan <plan.json> [--top <n>] [--out <moments.json>] [--dry-run] [--format text|json]

Score and rank every planned segment for review before expensive capture.
The JSON result includes stable segment ids, kill counts, weapons, victims,
reason codes, duration, and score. --out persists the same moments document;
--dry-run scores in memory and skips the write.
`

const demoSelectUsage = `usage: zv demo select --killplan <plan.json> --segments <seg-ids> --out <selected-plan.json> [--dry-run] [--format text|json]

Create a recorder-ready kill plan containing only the requested segments, in
the exact order supplied. This is the decision boundary between review and
HLAE/CS2 capture; use --dry-run before committing expensive GPU work.
`

const utilityUsage = `usage: zv utility audit [zv-parser utility-audit flags]
`

const composeUsage = `usage: zv compose final [zv-composer flags]
`

const shortsUsage = `usage: zv shorts render [zv-editor flags]
`

const streamUsage = `usage: zv stream variants [--format text|json] | zv stream plan [flags] | zv stream killfeed [flags] | zv stream transcribe [flags] | zv stream captions [flags] | zv stream render [flags]

Local CLI-first stream workflow. Generate an edit plan, review or enrich its
clip ranges/killfeed events, then render production artifacts directly under
<out>/shortslistosparasubir without starting Studio or MCP.
`

const musicUsage = `usage: zv music analyze --input <audio-or-video> --out <rhythm.json> [--killplan <plan.json>]
`

const analysisUsage = `usage: zv analysis tactical-data [zv-tactical-data flags] | zv analysis view [zv-analysis-viewer flags]
`

const galleryUsage = `usage: zv gallery open --path <index.html>
`

const serveUsage = `usage: zv serve
`

const checkUsage = `usage: zv check [--format text|json]
`

const skillsUsage = `usage: zv skills list [--format text|json] | zv skills show <name> [--format text|json] | zv skills check [--format text|json]
`

const skillsListUsage = `usage: zv skills list [--format text|json]
`

const skillsShowUsage = `usage: zv skills show <name> [--format text|json]
`

const skillsCheckUsage = `usage: zv skills check [--format text|json]
`

const workflowsUsage = `usage: zv workflows list [--format text|json] | zv workflows show <name> [--format text|json] | zv workflows validate <name> [--format text|json] -- [workflow flags] | zv workflows run <name> -- [workflow flags] | zv workflows check [--format text|json]
`

const workflowsListUsage = `usage: zv workflows list [--format text|json]
`

const workflowsShowUsage = `usage: zv workflows show <name> [--format text|json]
`

const workflowsRunUsage = `usage: zv workflows run <name> -- [workflow flags]
`

const workflowsValidateUsage = `usage: zv workflows validate <name> [--format text|json] -- [workflow flags]
`

const workflowsCheckUsage = `usage: zv workflows check [--format text|json]
`

const flowsUsage = `usage: zv flows list [--format text|json] | zv flows show <demo|stream> [--format text|json] | zv flows run <demo|stream> --run-dir <dir> --dry-run [--format text|json]

End-to-end production journeys for agents. Workflows describe atomic commands;
flows describe decision points and the safe order from source to upload pack.
"flows run" chains a whole journey in --dry-run mode.
`

const flowsListUsage = `usage: zv flows list [--format text|json]
`

const flowsShowUsage = `usage: zv flows show <demo|stream> [--format text|json]
`

const flowsRunUsage = `usage: zv flows run <demo|stream> --run-dir <dir> --dry-run [flags]

Chain a whole production journey safely: cheap deterministic stages run for real
and write chainable JSON into --run-dir, expensive capture/render stages run with
--dry-run, and creative gates are reported as skipped. Real execution stays stage
by stage behind the creative gates, so --dry-run is required.

Demo flags:
  --demo <dem>           demo to parse and capture from
  --steamid <SteamID64>  target POV player for demo parse
  --killplan <plan.json> existing kill plan; skips demo parse
  --run-dir <dir>        run output directory (required)

Stream flags:
  --input <mp4>          stream/VOD source (required)
  --events <json>        reviewed killfeed events; skips import when absent
  --words <json>         reviewed Spanish caption words; skips import when absent
  --run-dir <dir>        run output directory (required)

Common flags:
  --dry-run              required; the only supported execution mode
  --format <text|json>   report format (default text)
`
