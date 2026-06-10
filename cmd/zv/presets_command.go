package main

import (
	"fmt"
	"io"

	"github.com/reche/zackvideo/internal/editor"
)

type presetListEntry struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	FPS         int    `json:"fps"`
	Default     bool   `json:"default"`
}

func runPresets(args []string, stdout, stderr io.Writer) int {
	if isSingleHelp(args) {
		fmt.Fprint(stdout, presetsUsage)
		return exitSuccess
	}
	format, rest, err := parseFormatArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return exitInvalidArgs
	}
	if len(rest) != 0 {
		fmt.Fprintln(stderr, `error: unexpected extra args for "presets"`)
		fmt.Fprint(stderr, presetsUsage)
		return exitInvalidArgs
	}
	entries := presetListEntries()
	if format == "json" {
		if err := writeJSON(stdout, entries); err != nil {
			fmt.Fprintf(stderr, "error: writing json: %v\n", err)
			return exitUnexpected
		}
		return exitSuccess
	}
	for _, entry := range entries {
		name := entry.Name
		if entry.Default {
			name += " (default)"
		}
		fmt.Fprintf(stdout, "%s\t%dx%d@%dfps\t%s\n", name, entry.Width, entry.Height, entry.FPS, entry.Description)
	}
	return exitSuccess
}

func presetListEntries() []presetListEntry {
	defaultName := editor.DefaultPreset().Name
	names := editor.PresetNames()
	entries := make([]presetListEntry, 0, len(names))
	for _, name := range names {
		preset, ok := editor.PresetByName(name)
		if !ok {
			continue
		}
		entries = append(entries, presetListEntry{
			Name:        preset.Name,
			Description: preset.Description,
			Width:       preset.Width,
			Height:      preset.Height,
			FPS:         preset.FPS,
			Default:     preset.Name == defaultName,
		})
	}
	return entries
}
