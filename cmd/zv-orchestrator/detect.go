package main

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/rechedev9/fragforge/internal/recording"
)

// captureToolSource records how each capture/render tool path was resolved, so
// /api/capabilities can tell the user "auto-detected" vs "you set it" vs missing.
// Keys are env var names; values are "env" | "detected" | "none".
type captureToolSource map[string]string

// detectCaptureTools fills any empty capture/render tool path in cfg by probing
// the host for a standard install, so capture and rendering work on the user's
// PC without setting env vars. Explicit env always wins (reported "env"); a
// probe hit is "detected"; a blank nothing matched is "none" and the UI tells
// the user what to install. It is best-effort and never fails startup; on a
// container/Linux host the probes simply find nothing.
func detectCaptureTools(cfg config) (config, captureToolSource) {
	src := captureToolSource{}
	resolve := func(name, current string, probe func() string) string {
		if current != "" {
			src[name] = "env"
			return current
		}
		if found := probe(); found != "" {
			src[name] = "detected"
			return found
		}
		src[name] = "none"
		return ""
	}
	cfg.RecorderPath = resolve("ZV_RECORDER_PATH", cfg.RecorderPath, func() string { return detectSibling("zv-recorder") })
	cfg.HLAEPath = resolve("ZV_HLAE_PATH", cfg.HLAEPath, detectHLAE)
	cfg.CS2Path = resolve("ZV_CS2_PATH", cfg.CS2Path, detectCS2)
	cfg.EditorPath = resolve("ZV_EDITOR_PATH", cfg.EditorPath, func() string { return detectSibling("zv-editor") })
	cfg.FFmpegPath = resolve("ZV_FFMPEG_PATH", cfg.FFmpegPath, recording.FindFFmpeg)
	cfg.FFprobePath = resolve("ZV_FFPROBE_PATH", cfg.FFprobePath, recording.FindFFprobe)
	return cfg, src
}

// detectSibling looks for a pipeline binary (zv-recorder, zv-editor) next to
// this orchestrator binary; the build script emits them into the same bin/
// directory, and the desktop installer stages them together.
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

// detectHLAE looks for a versioned HLAE install (C:\HLAE-<ver>\HLAE.exe),
// preferring the highest version. It deliberately ignores a bare C:\HLAE, which
// is a known-wrong install for FragForge capture.
func detectHLAE() string {
	if runtime.GOOS != "windows" {
		return ""
	}
	matches := keepExisting(globNoErr(`C:\HLAE-*\HLAE.exe`))
	if len(matches) == 0 {
		return ""
	}
	sort.Strings(matches)
	return matches[len(matches)-1]
}

// detectCS2 finds cs2.exe across the user's Steam libraries by reading
// steamapps\libraryfolders.vdf, falling back to the default Steam dir.
func detectCS2() string {
	if runtime.GOOS != "windows" {
		return ""
	}
	const rel = `steamapps\common\Counter-Strike Global Offensive\game\bin\win64\cs2.exe`
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
	addRoot(`C:\Program Files (x86)\Steam`)
	if pf := os.Getenv("ProgramFiles(x86)"); pf != "" {
		addRoot(filepath.Join(pf, "Steam"))
	}
	for _, root := range roots {
		if p := firstExisting(filepath.Join(root, rel)); p != "" {
			return p
		}
	}
	return ""
}

// steamLibraryPaths pulls the "path" values out of a libraryfolders.vdf without a
// full VDF parser; the file lists each library root as: "path"  "<dir>".
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
	for _, p := range paths {
		if p == "" {
			continue
		}
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func keepExisting(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			out = append(out, p)
		}
	}
	return out
}
