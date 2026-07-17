// Package capturetools resolves the local executables used by the capture and
// render pipeline. It is shared by the orchestrator and the shell CLI so both
// surfaces report the same paths and sources.
package capturetools

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/rechedev9/fragforge/internal/recording"
)

const (
	SourceEnvironment = "env"
	SourceDetected    = "detected"
	SourceNone        = "none"
)

// Paths contains the optional explicit paths and their resolved values.
type Paths struct {
	Recorder string
	HLAE     string
	CS2      string
	Composer string
	Editor   string
	FFmpeg   string
	FFprobe  string
	Ytdlp    string
}

// Sources records how each path was resolved, keyed by its environment
// variable name.
type Sources map[string]string

// Tool is one resolved local executable and its current accessibility.
type Tool struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	Source     string `json:"source"`
	Configured bool   `json:"configured"`
	Accessible bool   `json:"accessible"`
}

// FromEnvironment reads only the non-secret executable path variables used by
// local capture and rendering.
func FromEnvironment() Paths {
	return Paths{
		Recorder: os.Getenv("ZV_RECORDER_PATH"),
		HLAE:     os.Getenv("ZV_HLAE_PATH"),
		CS2:      os.Getenv("ZV_CS2_PATH"),
		Composer: os.Getenv("ZV_COMPOSER_PATH"),
		Editor:   os.Getenv("ZV_EDITOR_PATH"),
		FFmpeg:   os.Getenv("ZV_FFMPEG_PATH"),
		FFprobe:  os.Getenv("ZV_FFPROBE_PATH"),
		Ytdlp:    os.Getenv("ZV_YTDLP_PATH"),
	}
}

// Detect fills empty paths from standard local installations. Explicit paths
// always win, even when currently inaccessible, so callers can distinguish a
// bad configuration from a missing tool.
func Detect(paths Paths) (Paths, Sources) {
	sources := Sources{}
	resolve := func(name, current string, probe func() string) string {
		if current != "" {
			sources[name] = SourceEnvironment
			return current
		}
		if found := probe(); found != "" {
			sources[name] = SourceDetected
			return found
		}
		sources[name] = SourceNone
		return ""
	}

	paths.Recorder = resolve("ZV_RECORDER_PATH", paths.Recorder, func() string { return detectSibling("zv-recorder") })
	paths.HLAE = resolve("ZV_HLAE_PATH", paths.HLAE, detectHLAE)
	paths.CS2 = resolve("ZV_CS2_PATH", paths.CS2, detectCS2)
	paths.Composer = resolve("ZV_COMPOSER_PATH", paths.Composer, func() string { return detectSibling("zv-composer") })
	paths.Editor = resolve("ZV_EDITOR_PATH", paths.Editor, func() string { return detectSibling("zv-editor") })
	paths.FFmpeg = resolve("ZV_FFMPEG_PATH", paths.FFmpeg, recording.FindFFmpeg)
	paths.FFprobe = resolve("ZV_FFPROBE_PATH", paths.FFprobe, recording.FindFFprobe)
	paths.Ytdlp = resolve("ZV_YTDLP_PATH", paths.Ytdlp, func() string { return lookPath("yt-dlp") })
	return paths, sources
}

// ResolveTool builds a current, machine-readable status row for one path.
func ResolveTool(name, path string, sources Sources) Tool {
	configured := path != ""
	return Tool{
		Name:       name,
		Path:       path,
		Source:     sources[name],
		Configured: configured,
		Accessible: configured && isExecutableFile(path),
	}
}

func lookPath(name string) string {
	path, err := exec.LookPath(name)
	if err != nil {
		return ""
	}
	return path
}

func detectSibling(name string) string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return firstExisting(filepath.Join(filepath.Dir(exe), name))
}

// detectHLAE deliberately ignores the known-wrong bare C:\HLAE install. The
// highest installed versioned release always wins so CS2 signature updates do
// not leave capture pinned to an older AfxHookSource2 build.
func detectHLAE() string {
	if runtime.GOOS != "windows" {
		return ""
	}
	return selectHLAE(keepExisting(globNoErr(`C:\HLAE-*\HLAE.exe`)))
}

func selectHLAE(matches []string) string {
	versioned := make([]string, 0, len(matches))
	for _, match := range matches {
		if _, ok := hlaeVersion(match); ok {
			versioned = append(versioned, match)
		}
	}
	if len(versioned) == 0 {
		return ""
	}
	sort.Slice(versioned, func(i, j int) bool {
		left, _ := hlaeVersion(versioned[i])
		right, _ := hlaeVersion(versioned[j])
		if comparison := compareVersionParts(left, right); comparison != 0 {
			return comparison < 0
		}
		return strings.ToLower(versioned[i]) < strings.ToLower(versioned[j])
	})
	return versioned[len(versioned)-1]
}

func hlaeVersion(path string) ([]int, bool) {
	normalized := strings.TrimRight(strings.ReplaceAll(path, `\`, "/"), "/")
	separator := strings.LastIndexByte(normalized, '/')
	if separator < 0 {
		return nil, false
	}
	parent := strings.TrimRight(normalized[:separator], "/")
	separator = strings.LastIndexByte(parent, '/')
	dir := parent[separator+1:]
	const prefix = "HLAE-"
	if len(dir) <= len(prefix) || !strings.EqualFold(dir[:len(prefix)], prefix) {
		return nil, false
	}
	raw := dir[len(prefix):]
	end := 0
	for end < len(raw) && ((raw[end] >= '0' && raw[end] <= '9') || raw[end] == '.') {
		end++
	}
	raw = strings.Trim(raw[:end], ".")
	if raw == "" {
		return nil, false
	}
	parts := strings.Split(raw, ".")
	version := make([]int, len(parts))
	for i, part := range parts {
		if part == "" {
			return nil, false
		}
		value, err := strconv.Atoi(part)
		if err != nil {
			return nil, false
		}
		version[i] = value
	}
	return version, true
}

func compareVersionParts(left, right []int) int {
	length := len(left)
	if len(right) > length {
		length = len(right)
	}
	for i := 0; i < length; i++ {
		var leftPart, rightPart int
		if i < len(left) {
			leftPart = left[i]
		}
		if i < len(right) {
			rightPart = right[i]
		}
		if leftPart < rightPart {
			return -1
		}
		if leftPart > rightPart {
			return 1
		}
	}
	return 0
}

func detectCS2() string {
	if runtime.GOOS != "windows" {
		return ""
	}
	const relativePath = `steamapps\common\Counter-Strike Global Offensive\game\bin\win64\cs2.exe`
	seen := map[string]bool{}
	var roots []string
	addRoot := func(steam string) {
		if steam == "" || seen[steam] {
			return
		}
		seen[steam] = true
		roots = append(roots, steam)
		roots = append(roots, steamLibraryPaths(filepath.Join(steam, `steamapps\libraryfolders.vdf`))...)
	}
	addRoot(steamRootFromRegistry())
	addRoot(`C:\Program Files (x86)\Steam`)
	if programFiles := os.Getenv("ProgramFiles(x86)"); programFiles != "" {
		addRoot(filepath.Join(programFiles, "Steam"))
	}
	for _, root := range roots {
		if path := firstExisting(filepath.Join(root, relativePath)); path != "" {
			return path
		}
	}
	return ""
}

func steamRootFromRegistry() string {
	if runtime.GOOS != "windows" {
		return ""
	}
	out, err := exec.Command("reg", "query", `HKCU\Software\Valve\Steam`, "/v", "SteamPath").Output()
	if err != nil {
		return ""
	}
	return filepath.FromSlash(steamPathFromRegOutput(string(out)))
}

func steamPathFromRegOutput(out string) string {
	for _, line := range strings.Split(out, "\n") {
		if !strings.Contains(line, "SteamPath") {
			continue
		}
		_, value, found := strings.Cut(line, "REG_SZ")
		if found {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func steamLibraryPaths(vdf string) []string {
	data, err := os.ReadFile(vdf)
	if err != nil {
		return nil
	}
	var paths []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, `"path"`) {
			continue
		}
		rest := strings.TrimPrefix(line, `"path"`)
		open := strings.Index(rest, `"`)
		if open < 0 {
			continue
		}
		rest = rest[open+1:]
		if end := strings.Index(rest, `"`); end >= 0 {
			paths = append(paths, strings.ReplaceAll(rest[:end], `\\`, `\`))
		}
	}
	return paths
}

func globNoErr(pattern string) []string {
	matches, _ := filepath.Glob(pattern)
	return matches
}

func firstExisting(paths ...string) string {
	for _, path := range paths {
		if path != "" && isExecutableFile(path) {
			return path
		}
	}
	return ""
}

func keepExisting(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		if isExecutableFile(path) {
			out = append(out, path)
		}
	}
	return out
}

func isExecutableFile(path string) bool {
	resolved, err := exec.LookPath(path)
	if err != nil {
		return false
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	if runtime.GOOS == "windows" && !hasWindowsExecutableExtension(resolved) {
		return false
	}
	return true
}

func hasWindowsExecutableExtension(path string) bool {
	extension := strings.ToLower(filepath.Ext(path))
	pathExtensions := os.Getenv("PATHEXT")
	if pathExtensions == "" {
		pathExtensions = ".COM;.EXE;.BAT;.CMD"
	}
	for _, allowed := range strings.Split(pathExtensions, ";") {
		if extension == strings.ToLower(strings.TrimSpace(allowed)) {
			return true
		}
	}
	return false
}
