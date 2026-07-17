package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rechedev9/fragforge/internal/capturetools"
	"github.com/rechedev9/fragforge/internal/editor"
)

// multiRunner records every delegated call and can fail a specific stage. It
// answers zv-editor --list-presets like a freshly built binary unless
// editorPresets or presetsErr simulate a stale one.
type multiRunner struct {
	calls         []fakeSubcommandCall
	failOn        int      // 1-based call index to fail; 0 never fails
	editorPresets []string // presets reported by --list-presets; nil means the registry
	presetsErr    error    // error returned by --list-presets
	capturePaths  capturetools.Paths
}

func (m *multiRunner) Run(_ context.Context, name string, args []string, _ io.Reader, stdout io.Writer, _ io.Writer) error {
	m.calls = append(m.calls, fakeSubcommandCall{Executable: name, Args: append([]string(nil), args...)})
	if len(args) == 1 && args[0] == "--list-presets" {
		if m.presetsErr != nil {
			return m.presetsErr
		}
		presets := m.editorPresets
		if presets == nil {
			presets = editor.PresetNames()
		}
		fmt.Fprintln(stdout, strings.Join(presets, "\n"))
		return nil
	}
	if m.failOn == len(m.calls) {
		return fmt.Errorf("stage boom")
	}
	return nil
}

func (m *multiRunner) CapturePaths() capturetools.Paths {
	return m.capturePaths
}

func setShortCaptureEnv(t *testing.T) {
	t.Helper()
	t.Setenv("ZV_HLAE_PATH", "")
	t.Setenv("ZV_CS2_PATH", "")
}

func TestRunShortChainsAllStages(t *testing.T) {
	setShortCaptureEnv(t)
	runner := &multiRunner{}
	var stdout, stderr strings.Builder
	outDir := filepath.Join(t.TempDir(), "run")

	code := Run([]string{
		"zv", "short", "inferno.dem",
		"--prompt", "haz un short con todas las kills de martinez",
		"--target-steamid", "76561198000000000",
		"--out", outDir,
		"--hlae", `C:\HLAE-2.190.1\HLAE.exe`,
		"--cs2", `C:\cs2.exe`,
	}, &stdout, &stderr, nil, runner)

	if got, want := code, exitSuccess; got != want {
		t.Fatalf("code = %d, want %d; stderr=%s", got, want, stderr.String())
	}
	if got, want := len(runner.calls), 4; got != want {
		t.Fatalf("calls len = %d, want %d: %#v", got, want, runner.calls)
	}
	wantArgs := [][]string{
		{"--list-presets"},
		{"parse", "--demo", "inferno.dem", "--steamid", "76561198000000000", "--out", filepath.Join(outDir, "killplan.json")},
		{"--killplan", filepath.Join(outDir, "killplan.json"), "--demo", "inferno.dem", "--out", filepath.Join(outDir, "recording"), "--hlae", `C:\HLAE-2.190.1\HLAE.exe`, "--cs2", `C:\cs2.exe`, "--hud", "deathnotices", "--portrait-safe-killfeed"},
		{"--recording-result", filepath.Join(outDir, "recording", "recording-result.json"), "--out", filepath.Join(outDir, "shorts"), "--publish-dir", filepath.Join(outDir, "shortslistosparasubir"), "--preset", "viral-60-clean", "--output-format", "short-9x16", "--kill-effect", "punch-in", "--transition", "flash", "--killplan", filepath.Join(outDir, "killplan.json"), "--compile-segments"},
	}
	wantBinaries := []string{"zv-editor", "zv-parser", "zv-recorder", "zv-editor"}
	for i, call := range runner.calls {
		if want := executableName(wantBinaries[i]); call.Executable != want && !strings.HasSuffix(call.Executable, want) {
			t.Fatalf("call %d executable = %q, want suffix %q", i, call.Executable, want)
		}
		if got, want := strings.Join(call.Args, " "), strings.Join(wantArgs[i], " "); got != want {
			t.Fatalf("call %d args = %q, want %q", i, got, want)
		}
	}
	for _, wantLine := range []string{
		"player: 76561198000000000 (martinez)",
		"selection: all kills (one compiled short)",
		"preset: viral-60-clean (1080x1920 @ 60fps, short-9x16)",
		"[1/3] parsing demo...",
		"[2/3] recording segments with HLAE/CS2...",
		"[3/3] rendering short and publish pack...",
		"publish pack: " + filepath.Join(outDir, "shortslistosparasubir"),
	} {
		if !strings.Contains(stdout.String(), wantLine) {
			t.Fatalf("stdout missing %q:\n%s", wantLine, stdout.String())
		}
	}
}

func TestRunShortAutoDetectsCaptureTools(t *testing.T) {
	setShortCaptureEnv(t)
	runner := &multiRunner{capturePaths: capturetools.Paths{
		HLAE: `C:\HLAE-detected\HLAE.exe`,
		CS2:  `C:\Steam\cs2.exe`,
	}}
	var stdout, stderr strings.Builder
	outDir := filepath.Join(t.TempDir(), "run")

	code := Run([]string{
		"zv", "short", "inferno.dem",
		"--prompt", "all kills of 76561198000000000",
		"--out", outDir,
	}, &stdout, &stderr, nil, runner)

	if got, want := code, exitSuccess; got != want {
		t.Fatalf("code = %d, want %d; stderr=%s", got, want, stderr.String())
	}
	if got, want := len(runner.calls), 4; got != want {
		t.Fatalf("calls len = %d, want %d: %#v", got, want, runner.calls)
	}
	recorderArgs := strings.Join(runner.calls[2].Args, " ")
	for _, want := range []string{
		`--hlae C:\HLAE-detected\HLAE.exe`,
		`--cs2 C:\Steam\cs2.exe`,
	} {
		if !strings.Contains(recorderArgs, want) {
			t.Fatalf("recorder args = %q, missing %q", recorderArgs, want)
		}
	}
}

func TestRunRecordAutoDetectsCaptureTools(t *testing.T) {
	tests := []struct {
		name string
		argv []string
	}{
		{
			name: "direct",
			argv: []string{"zv", "record", "--killplan", "plan.json", "--demo", "inferno.dem", "--out", "recording"},
		},
		{
			name: "workflow",
			argv: []string{"zv", "workflows", "run", "record", "--", "--killplan", "plan.json", "--demo", "inferno.dem", "--out", "recording"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &multiRunner{capturePaths: capturetools.Paths{
				HLAE: `C:\HLAE-detected\HLAE.exe`,
				CS2:  `C:\Steam\cs2.exe`,
			}}
			var stdout, stderr strings.Builder

			code := Run(tt.argv, &stdout, &stderr, nil, runner)

			if got, want := code, exitSuccess; got != want {
				t.Fatalf("code = %d, want %d; stderr=%s", got, want, stderr.String())
			}
			if got, want := len(runner.calls), 1; got != want {
				t.Fatalf("calls len = %d, want %d: %#v", got, want, runner.calls)
			}
			if got, want := strings.Join(runner.calls[0].Args, " "), `--killplan plan.json --demo inferno.dem --out recording --hlae C:\HLAE-detected\HLAE.exe --cs2 C:\Steam\cs2.exe`; got != want {
				t.Fatalf("record args = %q, want %q", got, want)
			}
		})
	}
}

func TestRunRecordReportsUnavailableCaptureTools(t *testing.T) {
	runner := &multiRunner{}
	var stdout, stderr strings.Builder

	code := Run([]string{
		"zv", "record",
		"--killplan", "plan.json",
		"--demo", "inferno.dem",
		"--out", "recording",
	}, &stdout, &stderr, nil, runner)

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d; stderr=%s", got, want, stderr.String())
	}
	if got := len(runner.calls); got != 0 {
		t.Fatalf("calls len = %d, want 0: %#v", got, runner.calls)
	}
	for _, want := range []string{"capture tools are unavailable", "zv capabilities --format json"} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr = %q, missing %q", stderr.String(), want)
		}
	}
}

func TestRunRecordJSONErrorsStayStructured(t *testing.T) {
	tests := []struct {
		name string
		argv []string
		want string
	}{
		{
			name: "wrapper validation",
			argv: []string{"zv", "record", "--format", "json"},
			want: "required",
		},
		{
			name: "capture preflight",
			argv: []string{
				"zv", "record", "--killplan", "plan.json", "--demo", "inferno.dem",
				"--out", "recording", "--format=json",
			},
			want: "capture tools are unavailable",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &multiRunner{}
			var stdout, stderr strings.Builder

			code := Run(tt.argv, &stdout, &stderr, nil, runner)

			if got, want := code, exitInvalidArgs; got != want {
				t.Fatalf("code = %d, want %d", got, want)
			}
			if stderr.Len() != 0 {
				t.Fatalf("stderr = %q, want empty JSON-mode stderr", stderr.String())
			}
			if len(runner.calls) != 0 {
				t.Fatalf("runner calls = %#v, want none", runner.calls)
			}
			var result recordErrorResult
			if err := json.Unmarshal([]byte(stdout.String()), &result); err != nil {
				t.Fatalf("decode JSON error: %v\n%s", err, stdout.String())
			}
			if result.OK || result.Executed || !strings.Contains(result.Error, tt.want) {
				t.Fatalf("result = %#v, want error containing %q", result, tt.want)
			}
		})
	}
}

func TestRunRecordJSONWrapsDelegatedFailure(t *testing.T) {
	runner := &multiRunner{
		failOn: 1,
		capturePaths: capturetools.Paths{
			HLAE: `C:\HLAE-detected\HLAE.exe`,
			CS2:  `C:\Steam\cs2.exe`,
		},
	}
	var stdout, stderr strings.Builder

	code := Run([]string{
		"zv", "record", "--killplan", "plan.json", "--demo", "inferno.dem",
		"--out", "recording", "--format=json",
	}, &stdout, &stderr, nil, runner)

	if got, want := code, exitUnexpected; got != want {
		t.Fatalf("code = %d, want delegated code %d", got, want)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty JSON-mode stderr", stderr.String())
	}
	var result recordErrorResult
	if err := json.Unmarshal([]byte(stdout.String()), &result); err != nil {
		t.Fatalf("decode JSON error: %v\n%s", err, stdout.String())
	}
	if result.OK || result.Executed || !strings.Contains(result.Error, "stage boom") {
		t.Fatalf("result = %#v, want delegated error", result)
	}
}

func TestRunRecordHelpDoesNotRequireCaptureTools(t *testing.T) {
	runner := &multiRunner{}
	var stdout, stderr strings.Builder

	code := Run([]string{"zv", "record", "--help"}, &stdout, &stderr, nil, runner)

	if got, want := code, exitSuccess; got != want {
		t.Fatalf("code = %d, want %d; stderr=%s", got, want, stderr.String())
	}
	if got, want := len(runner.calls), 1; got != want {
		t.Fatalf("calls len = %d, want %d: %#v", got, want, runner.calls)
	}
	if got, want := strings.Join(runner.calls[0].Args, " "), "--help"; got != want {
		t.Fatalf("record help args = %q, want %q", got, want)
	}
}

func TestRunShortCreatesStageOutputDirectories(t *testing.T) {
	setShortCaptureEnv(t)
	runner := &multiRunner{}
	var stdout, stderr strings.Builder
	outDir := filepath.Join(t.TempDir(), "runs", "inferno-short")

	code := Run([]string{
		"zv", "short", "inferno.dem",
		"--prompt", "all kills of 76561198000000000",
		"--out", outDir,
		"--hlae", "HLAE.exe",
		"--cs2", "cs2.exe",
	}, &stdout, &stderr, nil, runner)

	if got, want := code, exitSuccess; got != want {
		t.Fatalf("code = %d, want %d; stderr=%s", got, want, stderr.String())
	}
	for _, dir := range []string{outDir, filepath.Join(outDir, "recording")} {
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("stat %s: %v", dir, err)
		}
		if !info.IsDir() {
			t.Fatalf("%s is not a directory", dir)
		}
	}
}

func TestRunShortFailsFastWhenEditorPresetIsMissing(t *testing.T) {
	setShortCaptureEnv(t)
	runner := &multiRunner{editorPresets: []string{"short-classic", "short-premium"}}
	var stdout, stderr strings.Builder

	code := Run([]string{
		"zv", "short", "inferno.dem",
		"--prompt", "all kills of 76561198000000000",
		"--out", filepath.Join(t.TempDir(), "run"),
		"--hlae", "HLAE.exe",
		"--cs2", "cs2.exe",
	}, &stdout, &stderr, nil, runner)

	if got, want := code, exitUnexpected; got != want {
		t.Fatalf("code = %d, want %d; stderr=%s", got, want, stderr.String())
	}
	if got, want := len(runner.calls), 1; got != want {
		t.Fatalf("calls len = %d, want %d: %#v", got, want, runner.calls)
	}
	for _, want := range []string{`does not know preset "viral-60-clean"`, "scripts/build.ps1"} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr = %q, missing %q", stderr.String(), want)
		}
	}
}

func TestRunShortFailsFastWhenEditorPreflightFails(t *testing.T) {
	setShortCaptureEnv(t)
	runner := &multiRunner{presetsErr: fmt.Errorf("flag provided but not defined: -list-presets")}
	var stdout, stderr strings.Builder

	code := Run([]string{
		"zv", "short", "inferno.dem",
		"--prompt", "all kills of 76561198000000000",
		"--out", filepath.Join(t.TempDir(), "run"),
		"--hlae", "HLAE.exe",
		"--cs2", "cs2.exe",
	}, &stdout, &stderr, nil, runner)

	if got, want := code, exitUnexpected; got != want {
		t.Fatalf("code = %d, want %d; stderr=%s", got, want, stderr.String())
	}
	if got, want := len(runner.calls), 1; got != want {
		t.Fatalf("calls len = %d, want %d: %#v", got, want, runner.calls)
	}
	for _, want := range []string{"--list-presets", "scripts/build.ps1"} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr = %q, missing %q", stderr.String(), want)
		}
	}
}

func TestRunShortBeatSyncPromptAddsRhythmStage(t *testing.T) {
	setShortCaptureEnv(t)
	runner := &multiRunner{}
	var stdout, stderr strings.Builder
	outDir := filepath.Join(t.TempDir(), "run")

	code := Run([]string{
		"zv", "short", "inferno.dem",
		"--prompt", "las mejores kills de martinez al ritmo de la musica",
		"--target-steamid", "76561198000000000",
		"--out", outDir,
		"--music", "track.mp3",
		"--hlae", "HLAE.exe",
		"--cs2", "cs2.exe",
	}, &stdout, &stderr, nil, runner)

	if got, want := code, exitSuccess; got != want {
		t.Fatalf("code = %d, want %d; stderr=%s", got, want, stderr.String())
	}
	if got, want := len(runner.calls), 5; got != want {
		t.Fatalf("calls len = %d, want %d: %#v", got, want, runner.calls)
	}
	if !strings.Contains(stdout.String(), "selection: best moments (top 5 segments)") {
		t.Fatalf("stdout missing best-moments selection line:\n%s", stdout.String())
	}
	if got, want := strings.Join(runner.calls[3].Args, " "), "analyze --input track.mp3 --out "+filepath.Join(outDir, "rhythm.json")+" --killplan "+filepath.Join(outDir, "killplan.json"); got != want {
		t.Fatalf("rhythm args = %q, want %q", got, want)
	}
	renderArgs := strings.Join(runner.calls[4].Args, " ")
	for _, want := range []string{
		"--preset viral-60-clean",
		"--music track.mp3",
		"--rhythm " + filepath.Join(outDir, "rhythm.json"),
		"--compile-segments",
		"--limit 5",
	} {
		if !strings.Contains(renderArgs, want) {
			t.Fatalf("render args = %q, missing %q", renderArgs, want)
		}
	}
}

func TestRunShortCleanPresetRecordsDeathnoticesHUD(t *testing.T) {
	setShortCaptureEnv(t)
	runner := &multiRunner{}
	var stdout, stderr strings.Builder
	outDir := filepath.Join(t.TempDir(), "run")

	code := Run([]string{
		"zv", "short", "inferno.dem",
		"--prompt", "all kills of 76561198000000000",
		"--preset", "viral-60-clean",
		"--out", outDir,
		"--hlae", "HLAE.exe",
		"--cs2", "cs2.exe",
	}, &stdout, &stderr, nil, runner)

	if got, want := code, exitSuccess; got != want {
		t.Fatalf("code = %d, want %d; stderr=%s", got, want, stderr.String())
	}
	if got, want := len(runner.calls), 4; got != want {
		t.Fatalf("calls len = %d, want %d: %#v", got, want, runner.calls)
	}
	recorderArgs := strings.Join(runner.calls[2].Args, " ")
	if !strings.Contains(recorderArgs, "--hud deathnotices") {
		t.Fatalf("recorder args = %q, missing --hud deathnotices", recorderArgs)
	}
	if !strings.Contains(recorderArgs, "--portrait-safe-killfeed") {
		t.Fatalf("recorder args = %q, missing --portrait-safe-killfeed", recorderArgs)
	}
	renderArgs := strings.Join(runner.calls[3].Args, " ")
	if !strings.Contains(renderArgs, "--preset viral-60-clean") {
		t.Fatalf("render args = %q, missing --preset viral-60-clean", renderArgs)
	}
}

func TestRunShortDefaultPresetRecordsDeathnoticesHUD(t *testing.T) {
	setShortCaptureEnv(t)
	runner := &multiRunner{}
	var stdout, stderr strings.Builder
	outDir := filepath.Join(t.TempDir(), "run")

	code := Run([]string{
		"zv", "short", "inferno.dem",
		"--prompt", "all kills of 76561198000000000",
		"--out", outDir,
		"--hlae", "HLAE.exe",
		"--cs2", "cs2.exe",
	}, &stdout, &stderr, nil, runner)

	if got, want := code, exitSuccess; got != want {
		t.Fatalf("code = %d, want %d; stderr=%s", got, want, stderr.String())
	}
	recorderArgs := strings.Join(runner.calls[2].Args, " ")
	if !strings.Contains(recorderArgs, "--hud deathnotices") {
		t.Fatalf("recorder args = %q, missing --hud deathnotices", recorderArgs)
	}
	if !strings.Contains(recorderArgs, "--portrait-safe-killfeed") {
		t.Fatalf("recorder args = %q, missing --portrait-safe-killfeed", recorderArgs)
	}
}

func TestRunShortFromRecordingSkipsParseAndRecord(t *testing.T) {
	setShortCaptureEnv(t)
	runner := &multiRunner{}
	var stdout, stderr strings.Builder
	outDir := filepath.Join(t.TempDir(), "run")

	code := Run([]string{
		"zv", "short",
		"--prompt", "todas las kills",
		"--from-recording", filepath.Join(outDir, "recording", "recording-result.json"),
		"--out", outDir,
	}, &stdout, &stderr, nil, runner)

	if got, want := code, exitSuccess; got != want {
		t.Fatalf("code = %d, want %d; stderr=%s", got, want, stderr.String())
	}
	if got, want := len(runner.calls), 2; got != want {
		t.Fatalf("calls len = %d, want %d: %#v", got, want, runner.calls)
	}
	if got, want := strings.Join(runner.calls[1].Args, " "), "--recording-result "+filepath.Join(outDir, "recording", "recording-result.json")+" --out "+filepath.Join(outDir, "shorts")+" --publish-dir "+filepath.Join(outDir, "shortslistosparasubir")+" --preset viral-60-clean --output-format short-9x16 --kill-effect punch-in --transition flash --compile-segments"; got != want {
		t.Fatalf("render args = %q, want %q", got, want)
	}
	if !strings.Contains(stdout.String(), "player: from existing recording") {
		t.Fatalf("stdout missing from-recording player line:\n%s", stdout.String())
	}
}

func TestRunShortDryRunExecutesNoStages(t *testing.T) {
	setShortCaptureEnv(t)
	runner := &multiRunner{}
	var stdout, stderr strings.Builder

	code := Run([]string{
		"zv", "short", "inferno.dem",
		"--prompt", "all kills of 76561198000000000",
		"--dry-run",
	}, &stdout, &stderr, nil, runner)

	if got, want := code, exitSuccess; got != want {
		t.Fatalf("code = %d, want %d; stderr=%s", got, want, stderr.String())
	}
	if got := len(runner.calls); got != 0 {
		t.Fatalf("calls len = %d, want 0: %#v", got, runner.calls)
	}
	for _, wantLine := range []string{
		"preset: viral-60-clean (1080x1920 @ 60fps, short-9x16)",
		"[1/3] parsing demo: zv-parser parse --demo inferno.dem --steamid 76561198000000000",
		"dry-run: no stages executed",
	} {
		if !strings.Contains(stdout.String(), wantLine) {
			t.Fatalf("stdout missing %q:\n%s", wantLine, stdout.String())
		}
	}
}

func TestRunShortDryRunJSONIsOneMachineReadablePlan(t *testing.T) {
	setShortCaptureEnv(t)
	runner := &multiRunner{capturePaths: capturetools.Paths{
		HLAE: `C:\HLAE-detected\HLAE.exe`,
		CS2:  `C:\Steam\cs2.exe`,
	}}
	var stdout, stderr strings.Builder

	code := Run([]string{
		"zv", "short", "inferno.dem",
		"--prompt", "all kills of 76561198000000000",
		"--dry-run",
		"--format", "json",
	}, &stdout, &stderr, nil, runner)

	if got, want := code, exitSuccess; got != want {
		t.Fatalf("code = %d, want %d; stderr=%s", got, want, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	if got := len(runner.calls); got != 0 {
		t.Fatalf("calls len = %d, want 0: %#v", got, runner.calls)
	}
	var result struct {
		OK        bool   `json:"ok"`
		DryRun    bool   `json:"dry_run"`
		Executed  bool   `json:"executed"`
		Player    string `json:"player"`
		Selection string `json:"selection"`
		Preset    struct {
			Name   string `json:"name"`
			Width  int    `json:"width"`
			Height int    `json:"height"`
			FPS    int    `json:"fps"`
		} `json:"preset"`
		Edit   shortDryRunEdit `json:"edit"`
		Output struct {
			RunDir     string `json:"run_dir"`
			ShortsDir  string `json:"shorts_dir"`
			PublishDir string `json:"publish_dir"`
		} `json:"output"`
		Stages []struct {
			Index      int      `json:"index"`
			Label      string   `json:"label"`
			Executable string   `json:"executable"`
			Args       []string `json:"args"`
		} `json:"stages"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &result); err != nil {
		t.Fatalf("unmarshal stdout: %v\n%s", err, stdout.String())
	}
	if !result.OK || !result.DryRun || result.Executed {
		t.Fatalf("result state = ok:%v dry_run:%v executed:%v, want true/true/false", result.OK, result.DryRun, result.Executed)
	}
	if result.Player != "76561198000000000" || result.Selection != "all kills (one compiled short)" {
		t.Fatalf("result player/selection = %q/%q", result.Player, result.Selection)
	}
	if result.Preset.Name != "viral-60-clean" || result.Preset.Width != 1080 || result.Preset.Height != 1920 || result.Preset.FPS != 60 {
		t.Fatalf("result preset = %#v", result.Preset)
	}
	if result.Edit.OutputFormat != editor.OutputFormatShort9x16 || result.Edit.KillEffect != editor.KillEffectPunchIn || result.Edit.Transition != editor.TransitionFlash {
		t.Fatalf("result edit = %#v", result.Edit)
	}
	if got, want := len(result.Stages), 3; got != want {
		t.Fatalf("stages len = %d, want %d: %#v", got, want, result.Stages)
	}
	if got, want := result.Stages[1].Executable, "zv-recorder"; got != want {
		t.Fatalf("record stage executable = %q, want %q", got, want)
	}
	wantRecordArgs := []string{
		"--killplan", filepath.Join("data", "runs", "inferno-short", "killplan.json"),
		"--demo", "inferno.dem",
		"--out", filepath.Join("data", "runs", "inferno-short", "recording"),
		"--hlae", `C:\HLAE-detected\HLAE.exe`,
		"--cs2", `C:\Steam\cs2.exe`,
		"--hud", "deathnotices",
		"--portrait-safe-killfeed",
	}
	if got, want := strings.Join(result.Stages[1].Args, " "), strings.Join(wantRecordArgs, " "); got != want {
		t.Fatalf("record stage args = %q, want %q", got, want)
	}
	if result.Output.RunDir == "" || result.Output.ShortsDir == "" || result.Output.PublishDir == "" {
		t.Fatalf("result output paths = %#v, want all paths", result.Output)
	}
	if got, want := result.Output.PublishDir, filepath.Join("data", "runs", "inferno-short", "shortslistosparasubir"); got != want {
		t.Fatalf("publish dir = %q, want %q", got, want)
	}
}

func TestRunShortDryRunResolvesLandscapeAndEditorialChoices(t *testing.T) {
	setShortCaptureEnv(t)
	var stdout, stderr strings.Builder
	code := Run([]string{
		"zv", "short", "inferno.dem",
		"--prompt", "video largo 16:9 de 76561198000000000",
		"--kill-effect", "velocity",
		"--transition", "whip",
		"--intro", "--outro",
		"--dry-run", "--format", "json",
	}, &stdout, &stderr, nil, &multiRunner{})
	if code != exitSuccess || stderr.Len() != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
	var result shortDryRunResult
	if err := json.Unmarshal([]byte(stdout.String()), &result); err != nil {
		t.Fatal(err)
	}
	if result.Preset.Width != 1920 || result.Preset.Height != 1080 {
		t.Fatalf("preset dimensions = %#v", result.Preset)
	}
	if result.Edit.OutputFormat != editor.OutputFormatLandscape16x9 || result.Edit.KillEffect != editor.KillEffectVelocity || result.Edit.Transition != editor.TransitionWhip || !result.Edit.Intro || !result.Edit.Outro {
		t.Fatalf("edit = %#v", result.Edit)
	}
	renderArgs := strings.Join(result.Stages[len(result.Stages)-1].Args, " ")
	for _, want := range []string{"--output-format landscape-16x9", "--kill-effect velocity", "--transition whip", "--intro", "--outro"} {
		if !strings.Contains(renderArgs, want) {
			t.Fatalf("render args = %q, missing %q", renderArgs, want)
		}
	}
}

func TestRunShortRejectsJSONForRealExecution(t *testing.T) {
	setShortCaptureEnv(t)
	runner := &multiRunner{}
	var stdout, stderr strings.Builder

	code := Run([]string{
		"zv", "short", "inferno.dem",
		"--prompt", "all kills of 76561198000000000",
		"--format", "json",
	}, &stdout, &stderr, nil, runner)

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d; stderr=%s", got, want, stderr.String())
	}
	if got := len(runner.calls); got != 0 {
		t.Fatalf("calls len = %d, want 0: %#v", got, runner.calls)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty for JSON error", stderr.String())
	}
	var result struct {
		OK       bool   `json:"ok"`
		DryRun   bool   `json:"dry_run"`
		Executed bool   `json:"executed"`
		Error    string `json:"error"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &result); err != nil {
		t.Fatalf("unmarshal stdout: %v\n%s", err, stdout.String())
	}
	if result.OK || result.DryRun || result.Executed || !strings.Contains(result.Error, "--format json requires --dry-run") {
		t.Fatalf("result = %#v, want non-executed JSON error", result)
	}
}

func TestRunShortJSONResolveErrorStaysMachineReadable(t *testing.T) {
	setShortCaptureEnv(t)
	runner := &multiRunner{}
	var stdout, stderr strings.Builder

	code := Run([]string{
		"zv", "short", "inferno.dem",
		"--prompt", "all kills",
		"--dry-run",
		"--format=json",
	}, &stdout, &stderr, nil, runner)

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d; stderr=%s", got, want, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty for JSON error", stderr.String())
	}
	if got := len(runner.calls); got != 0 {
		t.Fatalf("calls len = %d, want 0: %#v", got, runner.calls)
	}
	var result struct {
		OK       bool   `json:"ok"`
		DryRun   bool   `json:"dry_run"`
		Executed bool   `json:"executed"`
		Error    string `json:"error"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &result); err != nil {
		t.Fatalf("unmarshal stdout: %v\n%s", err, stdout.String())
	}
	if result.OK || !result.DryRun || result.Executed || !strings.Contains(result.Error, "does not identify a target player") {
		t.Fatalf("result = %#v, want dry-run resolution error", result)
	}
}

func TestRunShortStopsOnStageFailure(t *testing.T) {
	setShortCaptureEnv(t)
	runner := &multiRunner{failOn: 2}
	var stdout, stderr strings.Builder

	code := Run([]string{
		"zv", "short", "inferno.dem",
		"--prompt", "all kills of 76561198000000000",
		"--out", filepath.Join(t.TempDir(), "run"),
		"--hlae", "HLAE.exe",
		"--cs2", "cs2.exe",
	}, &stdout, &stderr, nil, runner)

	if got, want := code, exitUnexpected; got != want {
		t.Fatalf("code = %d, want %d", got, want)
	}
	if got, want := len(runner.calls), 2; got != want {
		t.Fatalf("calls len = %d, want %d", got, want)
	}
	if !strings.Contains(stderr.String(), "stage 1/3 (parsing demo) failed") {
		t.Fatalf("stderr missing stage failure message:\n%s", stderr.String())
	}
}

func TestRunShortValidationErrors(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantStderr string
	}{
		{
			name:       "unknown preset lists registry names",
			args:       []string{"zv", "short", "inferno.dem", "--prompt", "all kills of 76561198000000000", "--preset", "nope", "--dry-run"},
			wantStderr: fmt.Sprintf("unsupported preset %q (supported presets: %s)", "nope", strings.Join(supportedPresetNames(), ", ")),
		},
		{
			name:       "retired preset is rejected",
			args:       []string{"zv", "short", "inferno.dem", "--prompt", "all kills of 76561198000000000", "--preset", editor.PresetCleanPOV60, "--dry-run"},
			wantStderr: fmt.Sprintf("unsupported preset %q (supported presets: %s)", editor.PresetCleanPOV60, strings.Join(supportedPresetNames(), ", ")),
		},
		{
			name:       "beat sync requires music",
			args:       []string{"zv", "short", "inferno.dem", "--prompt", "all kills of 76561198000000000 with music", "--dry-run"},
			wantStderr: `beat-synced shorts require --music`,
		},
		{
			name:       "unresolved player name",
			args:       []string{"zv", "short", "inferno.dem", "--prompt", "todas las kills de martinez", "--dry-run"},
			wantStderr: `could not resolve player "martinez" to a SteamID64: pass --target-steamid`,
		},
		{
			name:       "no player at all",
			args:       []string{"zv", "short", "inferno.dem", "--prompt", "todas las kills", "--dry-run"},
			wantStderr: "the prompt does not identify a target player: pass --target-steamid",
		},
		{
			name:       "missing prompt",
			args:       []string{"zv", "short", "inferno.dem"},
			wantStderr: "missing required flag --prompt",
		},
		{
			name:       "missing demo and recording",
			args:       []string{"zv", "short", "--prompt", "todas las kills"},
			wantStderr: `missing demo path for "short"; pass <demo.dem> or --from-recording`,
		},
		{
			name:       "missing capture tools",
			args:       []string{"zv", "short", "inferno.dem", "--prompt", "all kills of 76561198000000000"},
			wantStderr: "capture tools are unavailable (HLAE and CS2); inspect zv capabilities --format json",
		},
		{
			name:       "invalid steamid",
			args:       []string{"zv", "short", "inferno.dem", "--prompt", "todas las kills", "--target-steamid", "abc", "--dry-run"},
			wantStderr: `target steamid "abc" must be a 64-bit unsigned integer`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setShortCaptureEnv(t)
			runner := &multiRunner{}
			var stdout, stderr strings.Builder

			code := Run(tt.args, &stdout, &stderr, nil, runner)

			if got, want := code, exitInvalidArgs; got != want {
				t.Fatalf("code = %d, want %d; stderr=%s", got, want, stderr.String())
			}
			if got := len(runner.calls); got != 0 {
				t.Fatalf("calls len = %d, want 0: %#v", got, runner.calls)
			}
			if !strings.Contains(stderr.String(), tt.wantStderr) {
				t.Fatalf("stderr = %q, missing %q", stderr.String(), tt.wantStderr)
			}
		})
	}
}

func TestRunShortHelpUsesStdout(t *testing.T) {
	runner := &multiRunner{}
	var stdout, stderr strings.Builder

	code := Run([]string{"zv", "short", "--help"}, &stdout, &stderr, nil, runner)

	if got, want := code, exitSuccess; got != want {
		t.Fatalf("code = %d, want %d", got, want)
	}
	if got, want := stdout.String(), shortUsage; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestRunPresetsListsRegistry(t *testing.T) {
	runner := &multiRunner{}
	var stdout, stderr strings.Builder

	code := Run([]string{"zv", "presets"}, &stdout, &stderr, nil, runner)

	if got, want := code, exitSuccess; got != want {
		t.Fatalf("code = %d, want %d; stderr=%s", got, want, stderr.String())
	}
	defaultPreset := editor.DefaultPreset()
	if !strings.Contains(stdout.String(), defaultPreset.Name+" (default)\t1080x1920@60fps\t"+defaultPreset.Description) {
		t.Fatalf("stdout missing default preset line:\n%s", stdout.String())
	}
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if got, want := len(lines), len(supportedPresetNames()); got != want {
		t.Fatalf("lines = %d, want %d", got, want)
	}
}

func TestValidateSkillCommandShort(t *testing.T) {
	tests := []struct {
		name    string
		command []string
		want    string
	}{
		{
			name:    "canonical short command",
			command: []string{"short", "inferno.dem", "--prompt", "todas las kills de martinez", "--target-steamid", "76561198000000000"},
			want:    "",
		},
		{
			name:    "short from recording",
			command: []string{"short", "--prompt", "todas las kills", "--from-recording", "run/recording/recording-result.json"},
			want:    "",
		},
		{
			name:    "short json dry run",
			command: []string{"short", "inferno.dem", "--prompt", "all kills 76561198000000000", "--dry-run", "--format", "json"},
			want:    "",
		},
		{
			name:    "short json real execution",
			command: []string{"short", "inferno.dem", "--prompt", "all kills 76561198000000000", "--format", "json"},
			want:    `--format json requires --dry-run for "short"`,
		},
		{
			name:    "short missing prompt",
			command: []string{"short", "inferno.dem"},
			want:    `missing required flag --prompt for "short"`,
		},
		{
			name:    "short missing demo and recording",
			command: []string{"short", "--prompt", "todas las kills"},
			want:    `missing demo path for "short"; pass <demo.dem> or --from-recording <recording-result.json>`,
		},
		{
			name:    "short unknown flag",
			command: []string{"short", "inferno.dem", "--prompt", "x", "--nope", "y"},
			want:    `unknown flag --nope for "short"`,
		},
		{
			name:    "presets canonical",
			command: []string{"presets"},
			want:    "",
		},
		{
			name:    "presets json",
			command: []string{"presets", "--format", "json"},
			want:    "",
		},
		{
			name:    "presets extra args",
			command: []string{"presets", "extra"},
			want:    `unexpected extra args for "presets"`,
		},
		{
			name:    "shorts render retired preset",
			command: []string{"shorts", "render", "--recording-result", "recording-result.json", "--out", "shorts", "--preset", editor.PresetFullHUD60},
			want:    `unsupported preset "full-hud-60" for "shorts render"; supported presets: viral-60-clean`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := validateSkillCommand(tt.command); got != tt.want {
				t.Fatalf("validateSkillCommand(%v) = %q, want %q", tt.command, got, tt.want)
			}
		})
	}
}
