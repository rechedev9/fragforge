package trace

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/rechedev9/fragforge/internal/editor"
)

// update regenerates the golden files from the current trace output instead of
// comparing against them. Run with:
//
//	go test ./internal/trace/... -run TestTraceGolden -update
var update = flag.Bool("update", false, "update the golden trace files")

// goldenCases pin the CLI contract from two committed fixtures: the sample
// plan with the default preset, and a plan with out-of-order kills within a
// segment (pinning cue sorting) rendered with a non-default preset.
var goldenCases = []struct {
	name    string
	fixture string
	golden  string
	preset  string
}{
	{
		name:    "sample default preset",
		fixture: "../../testdata/trace/killplan.sample.json",
		golden:  "../../testdata/trace/golden/trace.sample.json",
	},
	{
		name:    "out-of-order kills clean-pov-60",
		fixture: "../../testdata/trace/killplan.outoforder.json",
		golden:  "../../testdata/trace/golden/trace.outoforder.json",
		preset:  editor.PresetCleanPOV60,
	},
}

// goldenOptions mirrors what `zv trace --from-plan <fixture> --deterministic
// --pretty` resolves: notably the production default tail trim, so the traced
// argv models what zv-editor would actually run.
func goldenOptions(fixture, preset string) Options {
	return Options{
		FromPlan:        fixture,
		Preset:          preset,
		TailTrimSeconds: editor.DefaultTailTrimSeconds,
		Deterministic:   true,
	}
}

// marshalPretty mirrors cmd/zv/trace_command.go's writeTrace pretty path
// exactly: json.MarshalIndent with a two-space indent, plus a trailing
// newline. The golden test must byte-for-byte match what the CLI would
// print for --pretty, or it is not testing the real contract.
func marshalPretty(t *testing.T, doc TraceDocument) []byte {
	t.Helper()
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatalf("marshal trace: %v", err)
	}
	return append(b, '\n')
}

// TestTraceGolden runs the trace from each committed kill plan fixture in
// deterministic mode and compares the pretty-printed output byte-for-byte
// against its golden file. Regenerate the goldens with
// `go test ./internal/trace/... -run TestTraceGolden -update` after an
// intentional change to the trace document shape.
func TestTraceGolden(t *testing.T) {
	for _, tc := range goldenCases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := os.Stat(tc.fixture); err != nil {
				t.Fatalf("fixture missing at %s: %v", tc.fixture, err)
			}

			doc, err := Run(context.Background(), goldenOptions(tc.fixture, tc.preset))
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			got := marshalPretty(t, doc)

			if *update {
				if err := os.MkdirAll(filepath.Dir(tc.golden), 0o755); err != nil {
					t.Fatalf("mkdir golden dir: %v", err)
				}
				if err := os.WriteFile(tc.golden, got, 0o644); err != nil {
					t.Fatalf("write golden: %v", err)
				}
				t.Logf("wrote golden file %s (%d bytes)", tc.golden, len(got))
				return
			}

			want, err := os.ReadFile(tc.golden)
			if err != nil {
				t.Fatalf("reading golden file %s: %v (run with -update to generate it)", tc.golden, err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("trace output does not match golden %s (run with -update to regenerate if the change is intentional)\n--- got ---\n%s\n--- want ---\n%s", tc.golden, got, want)
			}
		})
	}
}

// TestTraceGoldenIsDeterministicAcrossRuns proves the golden tests are not
// accidentally comparing against a moving target: two independent Run calls
// over the same fixture, marshaled the same way, must be byte-identical.
func TestTraceGoldenIsDeterministicAcrossRuns(t *testing.T) {
	for _, tc := range goldenCases {
		t.Run(tc.name, func(t *testing.T) {
			first, err := Run(context.Background(), goldenOptions(tc.fixture, tc.preset))
			if err != nil {
				t.Fatalf("first Run: %v", err)
			}
			second, err := Run(context.Background(), goldenOptions(tc.fixture, tc.preset))
			if err != nil {
				t.Fatalf("second Run: %v", err)
			}
			a := marshalPretty(t, first)
			b := marshalPretty(t, second)
			if !bytes.Equal(a, b) {
				t.Fatalf("two Run calls over the same fixture produced different output:\nfirst:  %s\nsecond: %s", a, b)
			}
		})
	}
}
