package streamcli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

const (
	exitSuccess     = 0
	exitUnexpected  = 1
	exitInvalidArgs = 2
)

func isHelp(value string) bool {
	return value == "-h" || value == "--help" || value == "help"
}

func isSingleHelp(args []string) bool {
	return len(args) == 1 && isHelp(args[0])
}

func parseFormatArgs(args []string) (string, []string, error) {
	format := "text"
	var rest []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--format":
			if i+1 >= len(args) {
				return "", nil, fmt.Errorf("missing value for --format")
			}
			i++
			format = args[i]
		case strings.HasPrefix(arg, "--format="):
			format = strings.TrimPrefix(arg, "--format=")
		default:
			rest = append(rest, arg)
		}
	}
	if format != "text" && format != "json" {
		return "", nil, fmt.Errorf("unsupported format %q", format)
	}
	return format, rest, nil
}

func writeJSON(w io.Writer, value any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(value)
}

const streamUsage = `usage: zv-stream variants [--format text|json] | zv-stream plan [flags] | zv-stream killfeed [flags] | zv-stream transcribe [flags] | zv-stream captions [flags] | zv-stream render [flags]
`

const streamVariantsUsage = `usage: zv stream variants [--format text|json]
`

const streamPlanUsage = `usage: zv stream plan --input <stream.mp4> --out <edit-plan.json> [flags]

Flags:
  --variant <name>             layout from "zv stream variants"
  --clip-id <id>               initial clip id (default clip-001)
  --clip-start <seconds>       initial clip start (default 0)
  --clip-end <seconds>         initial clip end (default full source duration)
  --title <text>               initial clip title
  --streamer <nick>            optional streamer banner
  --face-crop x,y,w,h          override normalized facecam crop
  --gameplay-crop x,y,w,h      override normalized gameplay crop
  --killfeed-crop x,y,w,h      normalized source killfeed crop
  --detect-killfeed            detect highlighted notices and add exact cues
  --captions                   enable xAI transcription and Spanish subtitles
  --ffmpeg <path>              ffmpeg path for detection; defaults to discovery
  --ffprobe <path>             ffprobe path; defaults to local discovery
  --dry-run                    probe and validate without writing the plan
  --format <text|json>         output format (default text)
`

const streamKillfeedUsage = `usage: zv stream killfeed --plan <edit-plan.json> --events <killfeed-events.json> --out <reviewed-plan.json> [flags]

Imports factual notices after local cue detection. The events document uses:
{"schema_version":"1.0","clip_id":"clip-001","cues":[{"at_seconds":2.75,"kills":[{"attacker_side":"CT","attacker_name":"player","victim_side":"T","victim_name":"opponent","weapon":"awp"}]}]}
Cue timestamps and count must match the detected killfeed_seconds exactly. Set
kills to [] for a detected cue reviewed as a false positive; it is removed from
the enriched plan instead of becoming a fabricated notice.

Flags:
  --dry-run                    validate and print without writing the plan
  --format <text|json>         output format (default text)
`

const streamTranscribeUsage = `usage: zv stream transcribe --input <stream.mp4> --plan <edit-plan.json> --model <ggml-model.bin> --vad-model <ggml-vad.bin> --out <transcript-review.json> [flags]

Runs local FFmpeg/Whisper candidates over the selected clip, including a raw
audio pass and a dialogue-enhanced pass. Repeat --model to compare independent
models. The output is deliberately marked requires_review and cannot be used as
caption_words until an agent or person verifies the Spanish text and timings.

Flags:
  --clip-id <id>               clip to transcribe (default first plan clip)
  --model <path>               local Whisper GGML model; repeat for consensus
  --vad-model <path>           local Silero VAD GGML model
  --language <code>            transcription language (default es)
  --ffmpeg <path>              ffmpeg path; defaults to local discovery
  --ffprobe <path>             ffprobe path; defaults to local discovery
  --work-dir <dir>             optional reusable temporary directory
  --timeout <duration>         total local transcription timeout (default 10m)
  --dry-run                    validate tools, media, plan, and models only
  --format <text|json>         output format (default text)
`

const streamCaptionsUsage = `usage: zv stream captions --plan <edit-plan.json> --words <caption-words.json> --out <captioned-plan.json> [flags]

Imports reviewed Spanish word timings so any agent can render deterministic
subtitles without a cloud transcription key. The words document uses:
{"schema_version":"1.0","clip_id":"clip-001","language":"es","words":[{"word":"Buena","start_seconds":0.5,"end_seconds":0.9}]}
Times are relative to the selected clip's source range. For an audible clip
reviewed as containing no speech, use "no_speech":true and "words":[].

Flags:
  --dry-run                    validate and print without writing the plan
  --format <text|json>         output format (default text)
`

const streamRenderUsage = `usage: zv stream render --input <stream.mp4> --plan <edit-plan.json> --out <run-dir> [flags]

The plan is the source of truth for ranges, crops, killfeed events, captions,
music, effects, and text. Captions use reviewed Spanish words when imported;
otherwise they require XAI_API_KEY and are translated to Spanish. A cover JPG
is selected from the strongest confirmed killfeed event (or a stable fallback
when there are no confirmed kills). Final videos, covers, ASS captions,
manifest, and gallery are copied to
<out>/shortslistosparasubir.

Flags:
  --title <text>               gallery title
  --ffmpeg <path>              ffmpeg path; defaults to local discovery
  --ffprobe <path>             ffprobe path; defaults to local discovery
  --timeout <duration>         render timeout (default 20m)
  --work-dir <dir>             temporary stage directory
  --music-dir <dir>            optional music catalog directory
  --dry-run                    probe and validate without rendering
  --format <text|json>         output format (default text)
`
