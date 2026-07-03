// Package vodfetch downloads a Twitch clip/VOD (or any yt-dlp-supported URL)
// to a local MP4 using an external yt-dlp binary. It is a standalone
// building block for the streamclips pipeline: it does not know about jobs,
// workers, or storage, only how to fetch one URL to one destination path.
package vodfetch

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// Sentinel errors classified from yt-dlp stderr. Callers can use errors.Is to
// branch on them; Download always wraps the underlying yt-dlp output as
// additional context.
var (
	// ErrNotFound means the source no longer exists (HTTP 404, deleted clip).
	ErrNotFound = errors.New("vodfetch: source not found")
	// ErrAuthRequired means the source needs a login/subscription yt-dlp does
	// not have credentials for.
	ErrAuthRequired = errors.New("vodfetch: authentication required")
	// ErrUnavailable means the source exists but cannot currently be
	// downloaded (geo-restricted, expired, private).
	ErrUnavailable = errors.New("vodfetch: source unavailable")
)

// SourceKind classifies a URL so callers can special-case Twitch clips and
// VODs while still allowing any other yt-dlp-supported URL to pass through.
type SourceKind int

const (
	// SourceOther is any http(s) URL that is not a recognized Twitch clip or
	// VOD URL. yt-dlp supports many sites, so this is not an error.
	SourceOther SourceKind = iota
	// SourceTwitchClip is a clips.twitch.tv/<slug> or
	// www.twitch.tv/<channel>/clip/<slug> URL.
	SourceTwitchClip
	// SourceTwitchVOD is a www.twitch.tv/videos/<id> URL.
	SourceTwitchVOD
)

func (k SourceKind) String() string {
	switch k {
	case SourceTwitchClip:
		return "twitch_clip"
	case SourceTwitchVOD:
		return "twitch_vod"
	default:
		return "other"
	}
}

var twitchVODPath = regexp.MustCompile(`^/videos/\d+/?$`)
var twitchClipPath = regexp.MustCompile(`^/[^/]+/clip/[^/]+/?$`)

// nonVideoExts are file extensions whose URLs are direct links to a non-video
// asset: an image pasted from a clipboard uploader (the reported case was a
// ShareX .png), a document, an archive, or an audio-only file. yt-dlp's
// generic extractor cannot turn any of these into an MP4, so ClassifySource
// rejects them up front instead of enqueuing a download that is guaranteed to
// fail deep inside the acquire worker. Video container extensions (.mp4, .mov,
// .webm, ...) are intentionally absent: a direct link to one of those is a
// legitimate source yt-dlp can fetch.
var nonVideoExts = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true,
	".bmp": true, ".svg": true, ".ico": true, ".tif": true, ".tiff": true,
	".heic": true, ".avif": true,
	".pdf": true, ".txt": true, ".md": true, ".csv": true, ".json": true,
	".xml": true, ".html": true, ".htm": true,
	".zip": true, ".rar": true, ".7z": true, ".gz": true, ".tar": true,
	".mp3": true, ".wav": true, ".flac": true, ".ogg": true, ".m4a": true,
	".doc": true, ".docx": true, ".xls": true, ".xlsx": true,
}

// ClassifySource validates url as http(s) and reports what kind of source it
// is. Non-http(s) schemes are rejected.
func ClassifySource(rawURL string) (SourceKind, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return SourceOther, fmt.Errorf("parse url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return SourceOther, fmt.Errorf("unsupported url scheme %q, want http or https", u.Scheme)
	}
	if u.Host == "" {
		return SourceOther, errors.New("url has no host")
	}
	if ext := strings.ToLower(path.Ext(u.Path)); ext != "" && nonVideoExts[ext] {
		return SourceOther, fmt.Errorf("url points to a %s file, not a video; paste a twitch or youtube clip or vod link", ext)
	}

	host := strings.ToLower(u.Hostname())
	switch host {
	case "clips.twitch.tv":
		if strings.Trim(u.Path, "/") == "" {
			return SourceOther, errors.New("twitch clip url has no slug")
		}
		return SourceTwitchClip, nil
	case "www.twitch.tv", "twitch.tv", "m.twitch.tv":
		switch {
		case twitchVODPath.MatchString(u.Path):
			return SourceTwitchVOD, nil
		case twitchClipPath.MatchString(u.Path):
			return SourceTwitchClip, nil
		}
	}
	return SourceOther, nil
}

// CommandRunner runs an external command and captures its stdout/stderr
// separately, so callers can classify failures from stderr text. It is the
// consumer-side seam Fetcher depends on, mirroring commandRunner in
// internal/workers.
type CommandRunner interface {
	Run(ctx context.Context, dir, name string, args ...string) (stdout, stderr string, err error)
}

// execCommandRunner runs commands with os/exec.
type execCommandRunner struct{}

func (execCommandRunner) Run(ctx context.Context, dir, name string, args ...string) (string, string, error) {
	// #nosec G204 -- vodfetch executes a configured local yt-dlp binary with an argument slice, not a shell string.
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

// Fetcher downloads clips/VODs with yt-dlp.
type Fetcher struct {
	// BinaryPath is the yt-dlp executable to run. Defaults to "yt-dlp" (found
	// via PATH) when empty.
	BinaryPath string
	// Runner executes BinaryPath. Defaults to execCommandRunner when nil.
	Runner CommandRunner
}

// Result describes a downloaded (or already-present) file.
type Result struct {
	Path  string
	Bytes int64
}

func (f Fetcher) binaryPath() string {
	if f.BinaryPath != "" {
		return f.BinaryPath
	}
	return "yt-dlp"
}

func (f Fetcher) runner() CommandRunner {
	if f.Runner != nil {
		return f.Runner
	}
	return execCommandRunner{}
}

// Download fetches url into destPath as an MP4. It is idempotent: if
// destPath already exists, Download returns its size without invoking
// yt-dlp. Otherwise it downloads into a temp file in destPath's directory
// and atomically renames it into place, so a crash or cancellation never
// leaves a truncated file at destPath.
func (f Fetcher) Download(ctx context.Context, rawURL, destPath string) (Result, error) {
	if _, err := ClassifySource(rawURL); err != nil {
		return Result{}, fmt.Errorf("classify source: %w", err)
	}

	if info, err := os.Stat(destPath); err == nil {
		if info.IsDir() {
			return Result{}, fmt.Errorf("dest path %q is a directory", destPath)
		}
		return Result{Path: destPath, Bytes: info.Size()}, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return Result{}, fmt.Errorf("stat dest path: %w", err)
	}

	destDir := filepath.Dir(destPath)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return Result{}, fmt.Errorf("create dest dir: %w", err)
	}

	tmp, err := os.CreateTemp(destDir, filepath.Base(destPath)+".*.part")
	if err != nil {
		return Result{}, fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	if closeErr := tmp.Close(); closeErr != nil {
		_ = os.Remove(tmpPath)
		return Result{}, fmt.Errorf("close temp file: %w", closeErr)
	}
	// yt-dlp writes directly to -o, so it must own file creation, not just
	// open the placeholder CreateTemp made. Remove it first and let yt-dlp
	// create tmpPath itself.
	if err := os.Remove(tmpPath); err != nil {
		return Result{}, fmt.Errorf("remove temp placeholder: %w", err)
	}

	args := []string{
		"-f", "bv*[ext=mp4]+ba[ext=m4a]/b[ext=mp4]/b",
		"--merge-output-format", "mp4",
		"--no-playlist",
		"--no-progress",
		"-o", tmpPath,
		rawURL,
	}

	_, stderr, runErr := f.runner().Run(ctx, destDir, f.binaryPath(), args...)
	if runErr != nil {
		_ = os.Remove(tmpPath)
		if ctx.Err() != nil {
			return Result{}, fmt.Errorf("download %s: %w", rawURL, ctx.Err())
		}
		return Result{}, classifyError(rawURL, stderr, runErr)
	}

	info, err := os.Stat(tmpPath)
	if err != nil {
		return Result{}, fmt.Errorf("stat downloaded file: %w", err)
	}
	if info.Size() == 0 {
		_ = os.Remove(tmpPath)
		return Result{}, fmt.Errorf("download %s: yt-dlp produced an empty file", rawURL)
	}

	if err := os.Rename(tmpPath, destPath); err != nil {
		_ = os.Remove(tmpPath)
		return Result{}, fmt.Errorf("rename temp file into place: %w", err)
	}

	return Result{Path: destPath, Bytes: info.Size()}, nil
}

// classifyError maps yt-dlp stderr text to a typed sentinel error, falling
// back to a generic wrapped error when no known pattern matches.
func classifyError(rawURL, stderr string, runErr error) error {
	text := strings.ToLower(stderr)

	switch {
	case strings.Contains(text, "404") || strings.Contains(text, "does not exist") || strings.Contains(text, "not found"):
		return fmt.Errorf("download %s: %w: %s", rawURL, ErrNotFound, firstLine(stderr))
	case strings.Contains(text, "subscriber") || strings.Contains(text, "login") || strings.Contains(text, "authentication") || strings.Contains(text, "sign in"):
		return fmt.Errorf("download %s: %w: %s", rawURL, ErrAuthRequired, firstLine(stderr))
	case strings.Contains(text, "geo") || strings.Contains(text, "expired") || strings.Contains(text, "unavailable") || strings.Contains(text, "private"):
		return fmt.Errorf("download %s: %w: %s", rawURL, ErrUnavailable, firstLine(stderr))
	}

	if trimmed := strings.TrimSpace(stderr); trimmed != "" {
		return fmt.Errorf("download %s: yt-dlp failed: %w: %s", rawURL, runErr, firstLine(stderr))
	}
	return fmt.Errorf("download %s: yt-dlp failed: %w", rawURL, runErr)
}

// firstLine returns the first non-empty line of s, trimmed, so error
// messages stay short even when yt-dlp emits a multi-line traceback.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return trimmed
		}
	}
	return strings.TrimSpace(s)
}
