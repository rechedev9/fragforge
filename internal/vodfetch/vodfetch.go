// Package vodfetch downloads an allowlisted Twitch or YouTube clip/VOD to a
// local MP4 using an external yt-dlp binary. It is a standalone
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
	"strconv"
	"strings"
)

// DefaultMaxBytes matches the direct stream-upload ceiling. URL acquisition
// uses the same limit so yt-dlp cannot fill the local disk with an unbounded
// VOD before the worker timeout expires.
const DefaultMaxBytes int64 = 8 << 30

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
	// ErrTooLarge means the source exceeds the configured download ceiling.
	ErrTooLarge = errors.New("vodfetch: source exceeds maximum size")
)

// SourceKind classifies an allowlisted provider URL.
type SourceKind int

const (
	// SourceOther is an allowlisted provider URL that is not a recognized
	// Twitch clip or VOD URL (currently YouTube and Twitch channel URLs).
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
var twitchClipPath = regexp.MustCompile(`^/[A-Za-z0-9_]{1,25}/clip/[A-Za-z0-9_-]{1,128}/?$`)
var twitchClipSlugPath = regexp.MustCompile(`^/[A-Za-z0-9_-]{1,128}/?$`)
var youtubeVideoPath = regexp.MustCompile(`^/(?:shorts|live|embed)/([A-Za-z0-9_-]{1,64})/?$`)
var reflectedURLPattern = regexp.MustCompile(`https?://[^\s<>"']+`)
var youtubeVideoIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

var allowedProviderHosts = map[string]struct{}{
	"clips.twitch.tv":   {},
	"m.twitch.tv":       {},
	"twitch.tv":         {},
	"www.twitch.tv":     {},
	"m.youtube.com":     {},
	"music.youtube.com": {},
	"www.youtube.com":   {},
	"youtu.be":          {},
	"youtube.com":       {},
}

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

// Source is a validated acquisition URL and its safe public representation.
// AcquisitionURL may retain provider query parameters needed by yt-dlp and
// must never be serialized or logged. PublicURL contains no userinfo,
// fragment, or secret query fields.
type Source struct {
	Kind           SourceKind
	AcquisitionURL string
	PublicURL      string
}

// ValidateSource accepts only HTTPS URLs on the exact Twitch and YouTube
// provider allowlist. Exact provider ownership is the SSRF boundary: yt-dlp
// owns its HTTP transport, redirects, and DNS lookups, so arbitrary public
// hostnames cannot be made safe against rebinding by a one-time Go DNS check.
func ValidateSource(rawURL string) (Source, error) {
	if rawURL == "" || strings.TrimSpace(rawURL) != rawURL || strings.ContainsAny(rawURL, "\r\n\t") {
		return Source{}, errors.New("parse url")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		// url.Parse errors can quote the input. Keep credentials and signed
		// query parameters out of callers' logs even for malformed URLs.
		return Source{}, errors.New("parse url")
	}
	if !u.IsAbs() || u.Opaque != "" || !strings.EqualFold(u.Scheme, "https") {
		return Source{}, errors.New("source url must use https")
	}
	if u.Host == "" {
		return Source{}, errors.New("url has no host")
	}
	if u.User != nil {
		return Source{}, errors.New("source url must not contain userinfo")
	}
	if u.Port() != "" {
		return Source{}, errors.New("source url must not contain an explicit port")
	}
	host := strings.ToLower(u.Hostname())
	if _, ok := allowedProviderHosts[host]; !ok {
		return Source{}, errors.New("source provider is not supported; use a Twitch or YouTube URL")
	}
	if ext := strings.ToLower(path.Ext(u.Path)); ext != "" && nonVideoExts[ext] {
		return Source{}, fmt.Errorf("url points to a %s file, not a video; paste a twitch or youtube clip or vod link", ext)
	}

	kind := SourceOther
	switch host {
	case "clips.twitch.tv":
		if !twitchClipSlugPath.MatchString(u.Path) {
			return Source{}, errors.New("unsupported twitch video url")
		}
		kind = SourceTwitchClip
	case "www.twitch.tv", "twitch.tv", "m.twitch.tv":
		switch {
		case twitchVODPath.MatchString(u.Path):
			kind = SourceTwitchVOD
		case twitchClipPath.MatchString(u.Path):
			kind = SourceTwitchClip
		default:
			return Source{}, errors.New("unsupported twitch video url")
		}
	case "youtu.be":
		if !twitchClipSlugPath.MatchString(u.Path) {
			return Source{}, errors.New("unsupported youtube video url")
		}
	default:
		if u.Path == "/watch" {
			if !youtubeVideoIDPattern.MatchString(u.Query().Get("v")) {
				return Source{}, errors.New("youtube watch url has no valid video id")
			}
		} else if !youtubeVideoPath.MatchString(u.Path) {
			return Source{}, errors.New("unsupported youtube video url")
		}
	}

	acquisition := *u
	acquisition.Scheme = "https"
	acquisition.Host = host
	acquisition.Fragment = ""
	acquisition.RawFragment = ""
	return Source{
		Kind:           kind,
		AcquisitionURL: acquisition.String(),
		PublicURL:      publicProviderURL(&acquisition),
	}, nil
}

// ClassifySource validates rawURL and reports its provider kind.
func ClassifySource(rawURL string) (SourceKind, error) {
	source, err := ValidateSource(rawURL)
	if err != nil {
		return SourceOther, err
	}
	return source.Kind, nil
}

func publicProviderURL(source *url.URL) string {
	public := *source
	public.User = nil
	public.RawQuery = ""
	public.ForceQuery = false
	public.Fragment = ""
	public.RawFragment = ""
	if strings.HasSuffix(public.Hostname(), "youtube.com") && public.Path == "/watch" {
		videoID := source.Query().Get("v")
		if youtubeVideoIDPattern.MatchString(videoID) {
			query := make(url.Values)
			query.Set("v", videoID)
			public.RawQuery = query.Encode()
		}
	}
	return public.String()
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
	// MaxBytes caps the downloaded file. Values <= 0 use DefaultMaxBytes.
	MaxBytes int64
	// Runner executes BinaryPath. Defaults to execCommandRunner when nil.
	Runner CommandRunner
}

// Result describes a downloaded (or already-present) file.
type Result struct {
	Path  string
	Bytes int64
	Title string
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

func (f Fetcher) maxBytes() int64 {
	if f.MaxBytes > 0 {
		return f.MaxBytes
	}
	return DefaultMaxBytes
}

// Download fetches url into destPath as an MP4. It is idempotent: if
// destPath already exists, Download returns its size without invoking
// yt-dlp. Otherwise it downloads into a temp file in destPath's directory
// and atomically renames it into place, so a crash or cancellation never
// leaves a truncated file at destPath.
func (f Fetcher) Download(ctx context.Context, rawURL, destPath string) (Result, error) {
	validated, err := ValidateSource(rawURL)
	if err != nil {
		return Result{}, fmt.Errorf("classify source: %w", err)
	}
	rawURL = validated.AcquisitionURL
	maxBytes := f.maxBytes()
	source := validated.PublicURL

	if info, err := os.Stat(destPath); err == nil {
		if info.IsDir() {
			return Result{}, fmt.Errorf("dest path %q is a directory", destPath)
		}
		if info.Size() > maxBytes {
			return Result{}, fmt.Errorf("download %s: %w (limit %d bytes)", source, ErrTooLarge, maxBytes)
		}
		return Result{Path: destPath, Bytes: info.Size()}, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return Result{}, fmt.Errorf("stat dest path: %w", err)
	}

	destDir := filepath.Dir(destPath)
	if err := os.MkdirAll(destDir, 0o700); err != nil {
		return Result{}, fmt.Errorf("create dest dir: %w", err)
	}
	if err := os.Chmod(destDir, 0o700); err != nil {
		return Result{}, fmt.Errorf("restrict dest dir permissions: %w", err)
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
		"--ignore-config",
		"-f", "bv*[ext=mp4]+ba[ext=m4a]/b[ext=mp4]/b",
		"--merge-output-format", "mp4",
		"--no-playlist",
		"--no-progress",
		"--socket-timeout", "30",
		"--retries", "2",
		"--fragment-retries", "2",
		"--extractor-retries", "2",
		"--print", "after_move:%(title)s",
		"--max-filesize", strconv.FormatInt(maxBytes, 10),
		"-o", tmpPath,
		rawURL,
	}

	stdout, stderr, runErr := f.runner().Run(ctx, destDir, f.binaryPath(), args...)
	if runErr != nil {
		_ = os.Remove(tmpPath)
		if ctx.Err() != nil {
			return Result{}, fmt.Errorf("download %s: %w", source, ctx.Err())
		}
		return Result{}, classifyError(rawURL, stderr, runErr)
	}

	info, err := os.Stat(tmpPath)
	if err != nil {
		return Result{}, fmt.Errorf("stat downloaded file: %w", err)
	}
	if info.Size() == 0 {
		_ = os.Remove(tmpPath)
		return Result{}, fmt.Errorf("download %s: yt-dlp produced an empty file", source)
	}
	if info.Size() > maxBytes {
		_ = os.Remove(tmpPath)
		return Result{}, fmt.Errorf("download %s: %w (limit %d bytes)", source, ErrTooLarge, maxBytes)
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		_ = os.Remove(tmpPath)
		return Result{}, fmt.Errorf("restrict downloaded file permissions: %w", err)
	}

	if err := os.Rename(tmpPath, destPath); err != nil {
		_ = os.Remove(tmpPath)
		return Result{}, fmt.Errorf("rename temp file into place: %w", err)
	}

	return Result{Path: destPath, Bytes: info.Size(), Title: downloadTitle(stdout)}, nil
}

func downloadTitle(stdout string) string {
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(lines) == 0 {
		return ""
	}
	title := strings.TrimSpace(lines[len(lines)-1])
	if len(title) > 180 {
		title = strings.ToValidUTF8(title[:180], "")
	}
	return title
}

// classifyError maps yt-dlp stderr text to a typed sentinel error, falling
// back to a generic wrapped error when no known pattern matches.
func classifyError(rawURL, stderr string, runErr error) error {
	source := redactedURL(rawURL)
	if validated, err := ValidateSource(rawURL); err == nil {
		source = validated.PublicURL
	}
	sanitized := sanitizedErrorText(rawURL, stderr)
	line := firstLine(sanitized)
	text := strings.ToLower(sanitized)

	switch {
	case strings.Contains(text, "404") || strings.Contains(text, "does not exist") || strings.Contains(text, "not found"):
		return fmt.Errorf("download %s: %w: %s", source, ErrNotFound, line)
	case strings.Contains(text, "subscriber") || strings.Contains(text, "login") || strings.Contains(text, "authentication") || strings.Contains(text, "sign in"):
		return fmt.Errorf("download %s: %w: %s", source, ErrAuthRequired, line)
	case strings.Contains(text, "geo") || strings.Contains(text, "expired") || strings.Contains(text, "unavailable") || strings.Contains(text, "private"):
		return fmt.Errorf("download %s: %w: %s", source, ErrUnavailable, line)
	}

	if trimmed := strings.TrimSpace(stderr); trimmed != "" {
		return fmt.Errorf("download %s: %w: %s", source, redactedCommandError{err: runErr}, line)
	}
	return fmt.Errorf("download %s: %w", source, redactedCommandError{err: runErr})
}

// redactedCommandError preserves errors.Is/errors.As without reflecting an
// external command's potentially sensitive error text into logs.
type redactedCommandError struct{ err error }

func (e redactedCommandError) Error() string { return "yt-dlp command failed" }
func (e redactedCommandError) Unwrap() error { return e.err }

// redactedURL retains enough source context for diagnostics while removing
// the three URL components that commonly carry credentials.
func redactedURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "[redacted-url]"
	}
	u.User = nil
	u.RawQuery = ""
	u.ForceQuery = false
	u.Fragment = ""
	u.RawFragment = ""
	return u.String()
}

func sanitizedErrorText(rawURL, stderr string) string {
	text := strings.ReplaceAll(stderr, rawURL, redactedURL(rawURL))
	return reflectedURLPattern.ReplaceAllStringFunc(text, redactedURL)
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
