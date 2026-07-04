package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/rechedev9/fragforge/internal/tuiclient"
)

// truncate shortens s to at most n runes, adding an ellipsis when cut. A
// non-positive n yields "" (guards against negative widths at tiny terminal
// sizes, which would otherwise slice out of range).
func truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n == 1 {
		return string(r[:1])
	}
	return string(r[:n-1]) + "…"
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// shortID is the first 8 characters of a job UUID, enough to identify a row.
func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// defaultUploadDir prefills the upload prompt with the current directory so the
// operator only appends a filename.
func defaultUploadDir() string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return wd + string(os.PathSeparator)
}

// saveFinal streams a composed job's MP4 to a file in the current directory and
// returns its absolute path.
func saveFinal(cl *tuiclient.Client, id string) (string, error) {
	name := fmt.Sprintf("fragforge-%s.mp4", shortID(id))
	f, err := os.Create(name) // #nosec G304 -- fixed name in the operator's cwd
	if err != nil {
		return "", err
	}
	c, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := cl.DownloadFinal(c, id, f); err != nil {
		_ = f.Close()
		_ = os.Remove(name)
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	abs, err := filepath.Abs(name)
	if err != nil {
		return name, nil
	}
	return abs, nil
}

// relTime renders a compact "3m", "2h", "5d" age from a timestamp.
func relTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
