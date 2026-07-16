package main

const usage = `zv - deterministic CS2 demo-to-video workflows

Usage:
  zv short <demo.dem> --prompt "<instruction>" [--preset <name>] [--out <dir>] [--music <audio>] [--target-steamid <SteamID64>] [--from-recording <recording-result.json>] [--dry-run]
  zv batch <dir> [--recursive] [--steamid <id>] [--out <dir>] [--jobs <n>] [--format text|json] [--report <path>]
  zv metrics [--reset]
  zv errors [--tail <n>] [--json] [--clear]
  zv presets [--format text|json]
  zv capabilities [--format text|json]
  zv demo parse [zv-parser parse flags]
  zv demo players [zv-demo-players flags]
  zv utility audit [zv-parser utility-audit flags]
  zv record [zv-recorder flags]
  zv compose final [zv-composer flags]
  zv shorts render [zv-editor flags]
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
  zv workflows run <name> -- [workflow flags]
  zv workflows check
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

One command from demo to upload-ready vertical Short (always 1080x1920 @ 60fps):
parse -> record (HLAE/CS2) -> [music analyze] -> render + publish pack.

Flags:
  --prompt <text>            editing instruction (Spanish or English); required
  --preset <name>            render preset; overrides the prompt (see zv presets)
  --out <dir>                run output directory; defaults under data/runs
  --music <audio>            music file; required for beat-synced shorts
  --target-steamid <id>      target player SteamID64 when the prompt only names a player
  --hlae <HLAE.exe>          HLAE path; defaults to ZV_HLAE_PATH
  --cs2 <cs2.exe>            CS2 path; defaults to ZV_CS2_PATH
  --from-recording <json>    existing recording-result.json; skips parse and record
  --dry-run                  print the resolved plan without launching HLAE/CS2 or FFmpeg

Prompt rules (deterministic keywords, no model calls):
  "todas las kills" / "all kills"        one compiled Short with every kill (default)
  "mejores" / "best" / "highlights"      best-moments compilation (top segments)
  "música" / "music" / "beat" / "ritmo"  beat-synced edit; needs --music
  a SteamID64 in the prompt              selects the target player
  a preset name in the prompt            selects that preset
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

const demoUsage = `usage: zv demo parse [zv-parser parse flags] | zv demo players [zv-demo-players flags]
`

const utilityUsage = `usage: zv utility audit [zv-parser utility-audit flags]
`

const composeUsage = `usage: zv compose final [zv-composer flags]
`

const shortsUsage = `usage: zv shorts render [zv-editor flags]
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
