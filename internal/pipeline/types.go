// Package pipeline coordinates local pipeline binaries.
package pipeline

type Config struct {
	KillPlanPath   string
	DemoPath       string
	OutputDir      string
	HLAEPath       string
	CS2Path        string
	RecorderPath   string
	ComposerPath   string
	FFmpegPath     string
	RecordTimeout  string
	ComposeTimeout string
}

type StepResult struct {
	Name            string   `json:"name"`
	Command         []string `json:"command"`
	DurationSeconds float64  `json:"duration_seconds"`
	Output          string   `json:"output,omitempty"`
	Error           string   `json:"error,omitempty"`
}

type Result struct {
	KillPlan          string       `json:"killplan"`
	Demo              string       `json:"demo"`
	OutputDir         string       `json:"output_dir"`
	RecordingDir      string       `json:"recording_dir"`
	RecordingResult   string       `json:"recording_result"`
	CompositionResult string       `json:"composition_result"`
	FinalOutput       string       `json:"final_output"`
	Steps             []StepResult `json:"steps"`
	Error             string       `json:"error,omitempty"`
}
