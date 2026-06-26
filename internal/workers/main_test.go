package workers

import (
	"os"
	"testing"
)

// TestMain redirects observability output to a temp dir so the best-effort obs
// recording in recordTaskFailure never writes data/obs into the source tree.
func TestMain(m *testing.M) {
	obsDir, _ := os.MkdirTemp("", "zv-workers-test-obs-")
	if obsDir != "" {
		os.Setenv("ZV_DATA_DIR", obsDir)
	}
	code := m.Run()
	if obsDir != "" {
		_ = os.RemoveAll(obsDir)
	}
	os.Exit(code)
}
