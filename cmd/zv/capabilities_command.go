package main

import (
	"fmt"
	"io"

	"github.com/rechedev9/fragforge/internal/capturetools"
)

type localCapabilityGroup struct {
	Ready bool                `json:"ready"`
	Tools []capturetools.Tool `json:"tools"`
}

type localCapabilities struct {
	LocalStudioReady bool                 `json:"local_studio_ready"`
	Record           localCapabilityGroup `json:"record"`
	Compose          localCapabilityGroup `json:"compose"`
	Render           localCapabilityGroup `json:"render"`
}

func runCapabilities(args []string, stdout, stderr io.Writer) int {
	if isSingleHelp(args) {
		fmt.Fprint(stdout, capabilitiesUsage)
		return exitSuccess
	}
	format, rest, err := parseFormatArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		fmt.Fprint(stderr, capabilitiesUsage)
		return exitInvalidArgs
	}
	if len(rest) != 0 {
		fmt.Fprintln(stderr, `error: unexpected extra args for "capabilities"`)
		fmt.Fprint(stderr, capabilitiesUsage)
		return exitInvalidArgs
	}

	paths, sources := capturetools.Detect(capturetools.FromEnvironment())
	report := buildLocalCapabilities(paths, sources)
	if format == "json" {
		if err := writeJSON(stdout, report); err != nil {
			fmt.Fprintf(stderr, "error: writing json: %v\n", err)
			return exitUnexpected
		}
		return exitSuccess
	}

	fmt.Fprintf(stdout, "local_studio_ready: %t\n", report.LocalStudioReady)
	writeLocalCapabilityGroup(stdout, "record", report.Record)
	writeLocalCapabilityGroup(stdout, "compose", report.Compose)
	writeLocalCapabilityGroup(stdout, "render", report.Render)
	return exitSuccess
}

func buildLocalCapabilities(paths capturetools.Paths, sources capturetools.Sources) localCapabilities {
	record := localCapabilityGroup{Tools: []capturetools.Tool{
		capturetools.ResolveTool("ZV_RECORDER_PATH", paths.Recorder, sources),
		capturetools.ResolveTool("ZV_HLAE_PATH", paths.HLAE, sources),
		capturetools.ResolveTool("ZV_CS2_PATH", paths.CS2, sources),
	}}
	compose := localCapabilityGroup{Tools: []capturetools.Tool{
		capturetools.ResolveTool("ZV_COMPOSER_PATH", paths.Composer, sources),
		capturetools.ResolveTool("ZV_FFMPEG_PATH", paths.FFmpeg, sources),
	}}
	render := localCapabilityGroup{Tools: []capturetools.Tool{
		capturetools.ResolveTool("ZV_EDITOR_PATH", paths.Editor, sources),
		capturetools.ResolveTool("ZV_FFMPEG_PATH", paths.FFmpeg, sources),
		capturetools.ResolveTool("ZV_FFPROBE_PATH", paths.FFprobe, sources),
	}}
	record.Ready = allToolsAccessible(record.Tools)
	compose.Ready = allToolsAccessible(compose.Tools)
	render.Ready = allToolsAccessible(render.Tools)
	return localCapabilities{
		LocalStudioReady: record.Ready && compose.Ready && render.Ready,
		Record:           record,
		Compose:          compose,
		Render:           render,
	}
}

func allToolsAccessible(tools []capturetools.Tool) bool {
	for _, tool := range tools {
		if !tool.Accessible {
			return false
		}
	}
	return len(tools) > 0
}

func writeLocalCapabilityGroup(w io.Writer, name string, group localCapabilityGroup) {
	fmt.Fprintf(w, "%s_ready: %t\n", name, group.Ready)
	for _, tool := range group.Tools {
		path := tool.Path
		if path == "" {
			path = "-"
		}
		fmt.Fprintf(w, "  %s: source=%s accessible=%t path=%s\n", tool.Name, tool.Source, tool.Accessible, path)
	}
}
