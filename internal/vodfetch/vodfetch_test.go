package vodfetch

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeRunner is a CommandRunner test double. It never touches the network;
// on success it writes fileContent to the -o argument's path, so it exercises
// the same temp-file-then-rename path a real yt-dlp run would.
type fakeRunner struct {
	stdout      string
	stderr      string
	err         error
	fileContent []byte
	// gotArgs records the last call's dir/name/args for assertions.
	gotDir  string
	gotName string
	gotArgs []string
	// ctxErr, when set, makes Run return ctx.Err() as if the process was
	// killed by cancellation.
	respectCtx bool
}

func (f *fakeRunner) Run(ctx context.Context, dir, name string, args ...string) (string, string, error) {
	f.gotDir = dir
	f.gotName = name
	f.gotArgs = args

	if f.respectCtx {
		select {
		case <-ctx.Done():
			return "", "context canceled", ctx.Err()
		default:
		}
	}

	if f.err != nil {
		return f.stdout, f.stderr, f.err
	}

	outPath := outArg(args)
	if outPath != "" && f.fileContent != nil {
		if err := os.WriteFile(outPath, f.fileContent, 0o644); err != nil {
			return "", "", err
		}
	}
	return f.stdout, f.stderr, nil
}

// outArg extracts the path following "-o" from a yt-dlp argument list.
func outArg(args []string) string {
	return argValue(args, "-o")
}

func argValue(args []string, key string) string {
	for i, a := range args {
		if a == key && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func TestDownload_Success(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "clip.mp4")
	runner := &fakeRunner{fileContent: []byte("fake mp4 bytes")}
	f := Fetcher{BinaryPath: "yt-dlp", Runner: runner}

	got, err := f.Download(context.Background(), "https://clips.twitch.tv/SomeClipSlug", dest)
	if err != nil {
		t.Fatalf("Download() error = %v, want nil", err)
	}
	if got.Path != dest {
		t.Errorf("got.Path = %q, want %q", got.Path, dest)
	}
	if got.Bytes != int64(len("fake mp4 bytes")) {
		t.Errorf("got.Bytes = %d, want %d", got.Bytes, len("fake mp4 bytes"))
	}
	if _, err := os.Stat(dest); err != nil {
		t.Fatalf("dest file missing after Download: %v", err)
	}
	// The temp file yt-dlp wrote to must not survive the rename.
	if runner.gotDir == "" {
		t.Fatal("runner was not invoked")
	}
	tmpPath := outArg(runner.gotArgs)
	if tmpPath == dest {
		t.Errorf("yt-dlp -o path = dest %q, want a distinct temp file", dest)
	}
	if _, err := os.Stat(tmpPath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("temp file %q still exists after rename", tmpPath)
	}
	content, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if string(content) != "fake mp4 bytes" {
		t.Errorf("dest content = %q, want %q", content, "fake mp4 bytes")
	}
	if runtime.GOOS != "windows" {
		if info, statErr := os.Stat(dest); statErr != nil {
			t.Fatalf("stat dest: %v", statErr)
		} else if got, want := info.Mode().Perm(), os.FileMode(0o600); got != want {
			t.Errorf("dest permissions = %o, want %o", got, want)
		}
		if info, statErr := os.Stat(dir); statErr != nil {
			t.Fatalf("stat dest dir: %v", statErr)
		} else if got, want := info.Mode().Perm(), os.FileMode(0o700); got != want {
			t.Errorf("dest dir permissions = %o, want %o", got, want)
		}
	}
}

func TestDownload_RejectsOversizedResultAndRemovesTemp(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "clip.mp4")
	runner := &fakeRunner{fileContent: []byte("12345")}
	f := Fetcher{Runner: runner, MaxBytes: 4}

	_, err := f.Download(context.Background(), "https://cdn.example.com/clip.mp4", dest)
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("Download() error = %v, want errors.Is(_, ErrTooLarge)", err)
	}
	if _, statErr := os.Stat(dest); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("destination exists after oversized download; stat error = %v", statErr)
	}
	tmpPath := outArg(runner.gotArgs)
	if _, statErr := os.Stat(tmpPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("temporary download exists after size rejection; stat error = %v", statErr)
	}
	if got := argValue(runner.gotArgs, "--max-filesize"); got != "4" {
		t.Fatalf("--max-filesize = %q, want 4", got)
	}
}

func TestDownload_RedactsSensitiveURLFromErrors(t *testing.T) {
	rawURL := "https://sentinel-user:sentinel-pass@example.com/clip.mp4?token=sentinel-query#sentinel-fragment"
	tests := []struct {
		name    string
		stderr  string
		wantErr error
	}{
		{
			name:   "generic reflected url",
			stderr: "ERROR: failed to download " + rawURL,
		},
		{
			name:    "classified reflected url",
			stderr:  "ERROR: not found at " + rawURL,
			wantErr: ErrNotFound,
		},
		{
			name:    "classified error after warning",
			stderr:  "WARNING: retrying download\nERROR: login required at " + rawURL,
			wantErr: ErrAuthRequired,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dest := filepath.Join(t.TempDir(), "clip.mp4")
			runErr := errors.New("exit status 1")
			f := Fetcher{Runner: &fakeRunner{stderr: tt.stderr, err: runErr}}

			_, err := f.Download(context.Background(), rawURL, dest)
			if err == nil {
				t.Fatal("Download() error = nil, want non-nil")
			}
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Fatalf("Download() error = %v, want errors.Is(_, %v)", err, tt.wantErr)
			}
			if tt.wantErr == nil && !errors.Is(err, runErr) {
				t.Fatalf("Download() error = %v, want wrapped command error", err)
			}
			for _, secret := range []string{"sentinel-user", "sentinel-pass", "sentinel-query", "sentinel-fragment"} {
				if strings.Contains(err.Error(), secret) {
					t.Errorf("Download() error leaked %q: %v", secret, err)
				}
			}
			if !strings.Contains(err.Error(), "https://example.com/clip.mp4") {
				t.Errorf("Download() error = %v, want redacted source URL", err)
			}
		})
	}
}

func TestDownload_IdempotentSkipsExisting(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "clip.mp4")
	if err := os.WriteFile(dest, []byte("already here"), 0o644); err != nil {
		t.Fatalf("seed dest file: %v", err)
	}

	runner := &fakeRunner{fileContent: []byte("should not be used")}
	f := Fetcher{Runner: runner}

	got, err := f.Download(context.Background(), "https://clips.twitch.tv/SomeClipSlug", dest)
	if err != nil {
		t.Fatalf("Download() error = %v, want nil", err)
	}
	if got.Bytes != int64(len("already here")) {
		t.Errorf("got.Bytes = %d, want %d", got.Bytes, len("already here"))
	}
	if runner.gotName != "" {
		t.Errorf("runner was invoked for an already-present dest, gotName = %q", runner.gotName)
	}
}

func TestDownload_StderrClassification(t *testing.T) {
	tests := []struct {
		name    string
		stderr  string
		wantErr error
	}{
		{
			name:    "not found",
			stderr:  "ERROR: [twitch:clip] SomeSlug: Clip does not exist",
			wantErr: ErrNotFound,
		},
		{
			name:    "http 404",
			stderr:  "ERROR: unable to download video data: HTTP Error 404: Not Found",
			wantErr: ErrNotFound,
		},
		{
			name:    "auth required",
			stderr:  "ERROR: [twitch] This video is only available to subscribers",
			wantErr: ErrAuthRequired,
		},
		{
			name:    "login required",
			stderr:  "ERROR: Login required to access this content",
			wantErr: ErrAuthRequired,
		},
		{
			name:    "unavailable expired",
			stderr:  "ERROR: This video has expired and is no longer available",
			wantErr: ErrUnavailable,
		},
		{
			name:    "unavailable geo",
			stderr:  "ERROR: This content is not available in your geo region",
			wantErr: ErrUnavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			dest := filepath.Join(dir, "clip.mp4")
			runner := &fakeRunner{err: errors.New("exit status 1"), stderr: tt.stderr}
			f := Fetcher{Runner: runner}

			_, err := f.Download(context.Background(), "https://clips.twitch.tv/SomeClipSlug", dest)
			if err == nil {
				t.Fatal("Download() error = nil, want non-nil")
			}
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("Download() error = %v, want errors.Is(_, %v)", err, tt.wantErr)
			}
			if _, statErr := os.Stat(dest); !errors.Is(statErr, os.ErrNotExist) {
				t.Errorf("dest file was created despite failure")
			}
		})
	}
}

func TestDownload_GenericFailureWraps(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "clip.mp4")
	runner := &fakeRunner{err: errors.New("exit status 1"), stderr: "ERROR: something unexpected broke"}
	f := Fetcher{Runner: runner}

	_, err := f.Download(context.Background(), "https://clips.twitch.tv/SomeClipSlug", dest)
	if err == nil {
		t.Fatal("Download() error = nil, want non-nil")
	}
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrAuthRequired) || errors.Is(err, ErrUnavailable) {
		t.Errorf("Download() error = %v, want no sentinel match", err)
	}
	if !strings.Contains(err.Error(), "something unexpected broke") {
		t.Errorf("Download() error = %v, want it to contain stderr text", err)
	}
}

func TestDownload_ContextCancellation(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "clip.mp4")
	runner := &fakeRunner{respectCtx: true}
	f := Fetcher{Runner: runner}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := f.Download(ctx, "https://clips.twitch.tv/SomeClipSlug", dest)
	if err == nil {
		t.Fatal("Download() error = nil, want non-nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Download() error = %v, want errors.Is(_, context.Canceled)", err)
	}
}

func TestDownload_RejectsNonHTTPScheme(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "clip.mp4")
	runner := &fakeRunner{}
	f := Fetcher{Runner: runner}

	_, err := f.Download(context.Background(), "ftp://example.com/clip.mp4", dest)
	if err == nil {
		t.Fatal("Download() error = nil, want non-nil")
	}
	if runner.gotName != "" {
		t.Errorf("runner was invoked for a rejected url scheme, gotName = %q", runner.gotName)
	}
}

func TestDownload_RejectsNonVideoURL(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "clip.mp4")
	runner := &fakeRunner{}
	f := Fetcher{Runner: runner}

	_, err := f.Download(context.Background(), "http://167.233.55.246/root/uploads/FragForge_Studio_ao6vfNGPXa.png", dest)
	if err == nil {
		t.Fatal("Download() error = nil, want non-nil")
	}
	if runner.gotName != "" {
		t.Errorf("runner was invoked for a non-video url, gotName = %q", runner.gotName)
	}
	if _, statErr := os.Stat(dest); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("dest file was created despite rejection")
	}
}

func TestDownload_ArgsShape(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "clip.mp4")
	runner := &fakeRunner{fileContent: []byte("x")}
	f := Fetcher{Runner: runner}

	if _, err := f.Download(context.Background(), "https://clips.twitch.tv/SomeClipSlug", dest); err != nil {
		t.Fatalf("Download() error = %v", err)
	}

	want := []string{
		"-f", "bv*[ext=mp4]+ba[ext=m4a]/b[ext=mp4]/b",
		"--merge-output-format", "mp4",
		"--no-playlist",
		"--no-progress",
		"--max-filesize", "8589934592",
	}
	got := runner.gotArgs
	if len(got) < len(want)+3 { // +3 for "-o", tmpPath, url
		t.Fatalf("got args %v, too short to contain %v plus -o/tmp/url", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("args[%d] = %q, want %q", i, got[i], w)
		}
	}
	if got[len(got)-1] != "https://clips.twitch.tv/SomeClipSlug" {
		t.Errorf("last arg = %q, want the source url", got[len(got)-1])
	}
}

func TestClassifySource(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		want    SourceKind
		wantErr bool
	}{
		{
			name: "twitch clip subdomain",
			url:  "https://clips.twitch.tv/AwesomeSlug123",
			want: SourceTwitchClip,
		},
		{
			name: "twitch clip channel path",
			url:  "https://www.twitch.tv/somechannel/clip/AwesomeSlug123",
			want: SourceTwitchClip,
		},
		{
			name: "twitch vod",
			url:  "https://www.twitch.tv/videos/1234567890",
			want: SourceTwitchVOD,
		},
		{
			name: "other https url",
			url:  "https://www.youtube.com/watch?v=abc123",
			want: SourceOther,
		},
		{
			name: "twitch channel homepage is other",
			url:  "https://www.twitch.tv/somechannel",
			want: SourceOther,
		},
		{
			name: "direct mp4 link is allowed",
			url:  "https://cdn.example.com/clips/highlight.mp4",
			want: SourceOther,
		},
		{
			// The reported bug: a ShareX screenshot upload URL pasted into the
			// clip field. It is an image, not a video, so it must be rejected
			// before a doomed yt-dlp job is enqueued.
			name:    "png image url rejected",
			url:     "http://167.233.55.246/root/uploads/FragForge_Studio_ao6vfNGPXa.png",
			wantErr: true,
		},
		{
			name:    "jpg image url rejected",
			url:     "https://cdn.example.com/thumb.JPG",
			wantErr: true,
		},
		{
			name:    "image url with query string rejected",
			url:     "https://cdn.example.com/uploads/frame.webp?v=2",
			wantErr: true,
		},
		{
			name:    "audio-only url rejected",
			url:     "https://cdn.example.com/audio/track.mp3",
			wantErr: true,
		},
		{
			name:    "ftp scheme rejected",
			url:     "ftp://example.com/clip.mp4",
			wantErr: true,
		},
		{
			name:    "garbage url rejected",
			url:     "not a url at all",
			wantErr: true,
		},
		{
			name:    "no host rejected",
			url:     "https:///videos/1",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ClassifySource(tt.url)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ClassifySource(%q) error = nil, want non-nil", tt.url)
				}
				return
			}
			if err != nil {
				t.Fatalf("ClassifySource(%q) error = %v, want nil", tt.url, err)
			}
			if got != tt.want {
				t.Errorf("ClassifySource(%q) = %v, want %v", tt.url, got, tt.want)
			}
		})
	}
}

func TestSourceKind_String(t *testing.T) {
	tests := []struct {
		k    SourceKind
		want string
	}{
		{SourceTwitchClip, "twitch_clip"},
		{SourceTwitchVOD, "twitch_vod"},
		{SourceOther, "other"},
	}
	for _, tt := range tests {
		if got := tt.k.String(); got != tt.want {
			t.Errorf("SourceKind(%d).String() = %q, want %q", tt.k, got, tt.want)
		}
	}
}
