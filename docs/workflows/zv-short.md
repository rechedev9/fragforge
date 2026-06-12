# zv short: one command from demo to upload-ready Short

`zv short` is the primary product workflow: drop a `.dem` file, describe the
Short in plain Spanish or English, and get an upload-ready vertical video.
Output is always 1080x1920 @ 60fps by construction of the render preset
registry (`internal/editor/preset.go`); the default preset is `viral-60-clean`.

The command chains the existing stage binaries (zv-parser, zv-recorder,
zv-rhythm, zv-editor) with stage logging and stops with an actionable error
when a stage fails:

1. parsing demo
2. recording segments with HLAE/CS2
3. analyzing music beats (beat-synced shorts only)
4. rendering short and publish pack

## Examples

The examples use `--dry-run` so they are safe to copy; drop the flag to launch
the real HLAE/CS2 recording and FFmpeg render.

All kills of a player, default viral edit:

```bash
./bin/zv short testdata/foo.dem --prompt "haz un short con todas las kills de martinez" --target-steamid 76561198000000000 --dry-run
```

Best moments, beat-synced to a track (keeps the selected/default preset and
adds the music analysis stage):

```bash
./bin/zv short testdata/foo.dem --prompt "short con las mejores kills al ritmo de la musica" --target-steamid 76561198000000000 --music data/music/track.mp3 --dry-run
```

Preview the resolved plan without launching HLAE/CS2 or FFmpeg:

```bash
./bin/zv short testdata/foo.dem --prompt "all kills of 76561198000000000" --dry-run
```

Reuse existing footage and skip the parse and record stages:

```bash
./bin/zv short --prompt "todas las kills" --from-recording data/runs/run-004/recording/recording-result.json --dry-run
```

List the render presets with descriptions:

```bash
./bin/zv presets
./bin/zv presets --format json
```

## Prompt interpretation

Prompts are interpreted with deterministic keyword and regex rules
(`cmd/zv/short_prompt.go`); there are no model calls.

| Prompt condition | Effect |
| --- | --- |
| "todas las kills", "all kills" (default) | one compiled Short containing every kill |
| "mejores", "best", "highlights" | best-moments compilation (top 5 segments) |
| "musica", "music", "beat", "ritmo", "song" | beat-synced edit with the selected/default preset; requires `--music` |
| a 17-digit SteamID64 in the prompt | selects the target player |
| a registered preset name in the prompt | selects that preset |

Preset resolution order: explicit `--preset` flag, then a preset named in the
prompt, then the default `viral-60-clean`. When music is provided, the command
adds rhythm analysis and passes the generated `rhythm.json` to the render.

When the prompt only names a player (no SteamID64), the command fails with a
clear error asking for `--target-steamid`; list candidates with
`zv demo players --demo <demo.dem> --contains <name>`.

HLAE and CS2 paths come from `--hlae`/`--cs2` or the `ZV_HLAE_PATH`/
`ZV_CS2_PATH` environment variables, the same configuration the orchestrator
uses. On this machine HLAE lives at `C:\HLAE-2.190.1\HLAE.exe`.
