package streamkillfeed

import (
	"strings"
	"testing"
)

var benchmarkStderrTail string

func TestAppendStderrTailRetainsLatestBytes(t *testing.T) {
	t.Parallel()

	lines := []string{
		strings.Repeat("a", maxStderrTail-1),
		"second diagnostic line",
		strings.Repeat("z", maxStderrTail+31),
		"last line",
	}
	var tail stderrTail
	var full strings.Builder
	for i, line := range lines {
		appendStderrTail(&tail, line)
		full.WriteString(line)
		full.WriteByte('\n')
		want := full.String()
		if len(want) > maxStderrTail {
			want = want[len(want)-maxStderrTail:]
		}
		got := tail.String()
		if got != want {
			t.Fatalf("append %d stderr tail length/content mismatch: got %d bytes, want %d", i, len(got), len(want))
		}
	}
}

func BenchmarkAppendStderrTail1000Lines(b *testing.B) {
	line := strings.Repeat("showinfo diagnostic ", 8)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var tail stderrTail
		for range 1000 {
			appendStderrTail(&tail, line)
		}
		benchmarkStderrTail = tail.String()
	}
}
