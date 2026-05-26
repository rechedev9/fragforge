package recording

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type scheduledCommand struct {
	Tick int    `json:"tick"`
	Key  string `json:"key"`
	Cmd  string `json:"cmd"`
}

// GenerateHLAEJavaScript renders a self-contained HLAE 2.x mirv-script file.
func GenerateHLAEJavaScript(plan RecordingPlan) (string, error) {
	plan.Stream = normalizeStreamConfig(plan.Stream)
	if err := plan.Validate(); err != nil {
		return "", err
	}

	schedule := buildSchedule(plan)
	sort.SliceStable(schedule, func(i, j int) bool {
		if schedule[i].Tick == schedule[j].Tick {
			return schedule[i].Key < schedule[j].Key
		}
		return schedule[i].Tick < schedule[j].Tick
	})

	b, err := json.MarshalIndent(schedule, "    ", "  ")
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	sb.WriteString("\"use strict\";\n")
	sb.WriteString("{\n")
	sb.WriteString("    const id = \"zackvideo/generated-recorder\";\n\n")
	sb.WriteString("    if (globalThis[id] !== undefined) {\n")
	sb.WriteString("        globalThis[id].unregister();\n")
	sb.WriteString("        delete globalThis[id];\n")
	sb.WriteString("    }\n\n")
	sb.WriteString("    const schedule = ")
	sb.Write(b)
	sb.WriteString(";\n\n")
	sb.WriteString("    const fired = {};\n")
	sb.WriteString("    let armed = false;\n")
	sb.WriteString("    let lastTick = null;\n")
	sb.WriteString("    const run = (item) => {\n")
	sb.WriteString("        if (fired[item.key]) return;\n")
	sb.WriteString("        fired[item.key] = true;\n")
	sb.WriteString("        mirv.message(`[zackvideo] ${item.key}: ${item.cmd}\\n`);\n")
	sb.WriteString("        mirv.exec(item.cmd);\n")
	sb.WriteString("    };\n\n")
	sb.WriteString("    mirv.events.clientFrameStageNotify.on(id, (e) => {\n")
	sb.WriteString("        if (e.isBefore) return;\n")
	sb.WriteString("        const tick = mirv.getDemoTick();\n")
	sb.WriteString("        if (!armed) {\n")
	sb.WriteString("            if (lastTick !== null && tick < lastTick) {\n")
	sb.WriteString("                armed = true;\n")
	sb.WriteString("                mirv.message(`[zackvideo] demo playback armed at tick ${tick}\\n`);\n")
	sb.WriteString("            }\n")
	sb.WriteString("            lastTick = tick;\n")
	sb.WriteString("            return;\n")
	sb.WriteString("        }\n")
	sb.WriteString("        for (const item of schedule) {\n")
	sb.WriteString("            if (!fired[item.key] && tick >= item.tick) run(item);\n")
	sb.WriteString("        }\n")
	sb.WriteString("    });\n\n")
	sb.WriteString("    globalThis[id] = {\n")
	sb.WriteString("        unregister: () => mirv.events.clientFrameStageNotify.off(id)\n")
	sb.WriteString("    };\n")
	sb.WriteString("}\n")
	return sb.String(), nil
}

func buildSchedule(plan RecordingPlan) []scheduledCommand {
	commands := []scheduledCommand{}
	setupTick := 25
	for i, cmd := range streamSetupCommands(plan) {
		commands = append(commands, scheduledCommand{
			Tick: setupTick,
			Key:  fmt.Sprintf("stream-setup-%02d", i+1),
			Cmd:  cmd,
		})
	}

	for i, s := range plan.Segments {
		seekTick := 50
		if i > 0 {
			seekTick = plan.Segments[i-1].TickEnd + 32
		}
		recordStart := EffectiveRecordStartTick(s, plan.Tickrate)
		leadTicks := plan.Tickrate * 5
		seekTarget := max(1, s.TickStart-leadTicks)
		cameraWarmupTick := seekTarget + max(1, plan.Tickrate/2)
		cameraLead3Tick := recordStart - plan.Tickrate*3
		cameraLead2Tick := recordStart - plan.Tickrate*2
		cameraLead1Tick := recordStart - plan.Tickrate
		cameraLockTick := recordStart - 1
		if cameraWarmupTick >= recordStart {
			cameraWarmupTick = recordStart - max(2, plan.Tickrate/2)
		}

		commands = append(commands,
			scheduledCommand{Tick: seekTick, Key: "seek-" + s.ID, Cmd: fmt.Sprintf("demo_gototick %d", seekTarget)},
			scheduledCommand{Tick: max(seekTarget+1, cameraWarmupTick), Key: "camera-warmup-" + s.ID, Cmd: cameraCommand(plan.TargetNameInDemo, plan.TargetAccountID)},
			scheduledCommand{Tick: max(seekTarget+2, cameraLead3Tick), Key: "camera-lead-3s-" + s.ID, Cmd: cameraCommand(plan.TargetNameInDemo, plan.TargetAccountID)},
			scheduledCommand{Tick: max(seekTarget+3, cameraLead2Tick), Key: "camera-lead-2s-" + s.ID, Cmd: cameraCommand(plan.TargetNameInDemo, plan.TargetAccountID)},
			scheduledCommand{Tick: max(seekTarget+4, cameraLead1Tick), Key: "camera-lead-1s-" + s.ID, Cmd: cameraCommand(plan.TargetNameInDemo, plan.TargetAccountID)},
			scheduledCommand{Tick: max(seekTarget+5, cameraLockTick), Key: "camera-lock-" + s.ID, Cmd: cameraCommand(plan.TargetNameInDemo, plan.TargetAccountID)},
			scheduledCommand{Tick: recordStart + max(1, plan.Tickrate/2), Key: "camera-relock-" + s.ID, Cmd: cameraCommand(plan.TargetNameInDemo, plan.TargetAccountID)},
		)
		if i == 0 {
			commands = append(commands,
				scheduledCommand{Tick: max(seekTarget, recordStart-6), Key: "hide-demoui", Cmd: "demoui"},
			)
		}

		if plan.Runtime.HostTimescale > 0 && plan.Runtime.HostTimescale != 1 {
			commands = append(commands,
				scheduledCommand{
					Tick: max(1, recordStart-6),
					Key:  "timescale-up-" + s.ID,
					Cmd:  fmt.Sprintf("host_timescale %s", formatFloat(plan.Runtime.HostTimescale)),
				},
			)
		}

		commands = append(commands,
			scheduledCommand{Tick: recordStart, Key: "record-start-" + s.ID, Cmd: "mirv_streams record start"},
			scheduledCommand{Tick: s.TickEnd, Key: "record-end-" + s.ID, Cmd: "mirv_streams record end"},
		)

		if plan.Runtime.HostTimescale > 0 && plan.Runtime.HostTimescale != 1 {
			commands = append(commands,
				scheduledCommand{Tick: s.TickEnd + 4, Key: "timescale-reset-" + s.ID, Cmd: "host_timescale 1"},
			)
		}
	}

	lastEnd := plan.Segments[len(plan.Segments)-1].TickEnd
	pad := plan.Runtime.QuitTickPad
	if pad <= 0 {
		pad = 200
	}
	commands = append(commands,
		scheduledCommand{Tick: lastEnd + pad/2, Key: "shutdown", Cmd: "disconnect; quit"},
	)
	return commands
}

func cameraCommand(targetName string, accountID uint32) string {
	if targetName != "" {
		target := quoteConsoleArg(targetName)
		return fmt.Sprintf("spec_autodirector 0; spec_mode 1; spec_player %s; spec_mode 1; spec_player %s", target, target)
	}
	return fmt.Sprintf("spec_autodirector 0; spec_mode 1; spec_player_by_accountid %d; spec_player_by_accountid %d", accountID, accountID)
}

func quoteConsoleArg(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `"`, `\"`)
	return `"` + value + `"`
}

// EffectiveRecordStartTick returns the actual tick where HLAE starts recording
// a segment after applying recorder camera-settle timing.
func EffectiveRecordStartTick(segment RecordingSegment, tickrate int) int {
	if tickrate <= 0 || len(segment.Kills) == 0 {
		return segment.TickStart
	}
	firstKill := firstKillTick(segment)
	if firstKill <= 0 {
		return segment.TickStart
	}
	latestStart := firstKill - tickrate
	if latestStart <= segment.TickStart {
		return segment.TickStart
	}
	stabilizedStart := segment.TickStart + tickrate*2
	if stabilizedStart > latestStart {
		return latestStart
	}
	return stabilizedStart
}

func effectiveRecordStartTick(segment RecordingSegment, tickrate int) int {
	return EffectiveRecordStartTick(segment, tickrate)
}

func firstKillTick(segment RecordingSegment) int {
	out := 0
	for _, kill := range segment.Kills {
		if kill.Tick <= 0 {
			continue
		}
		if out == 0 || kill.Tick < out {
			out = kill.Tick
		}
	}
	return out
}

func streamSetupCommands(plan RecordingPlan) []string {
	recordName := slashPath(plan.OutputDir)
	recordFPS := fmt.Sprintf("mirv_streams record fps %d", plan.Stream.FPS)
	commands := []string{
		"cl_demo_predict 0",
		"cl_trueview_show_status 0",
		"mirv_panorama panelstyle panelId=trueview_row opacity=0",
		fmt.Sprintf(`mirv_streams record name "%s"`, recordName),
		recordFPS,
		"mirv_streams record screen enabled 1",
	}
	switch plan.Stream.Mode {
	case StreamModeTGASequence:
		commands = append(commands, "mirv_streams record screen settings afxClassic")
	default:
		settingName := ffmpegSettingName(plan.Stream.CRF)
		commands = append(commands,
			ffmpegSettingsCommand(settingName, plan.Stream.CRF),
			"mirv_streams record screen settings "+settingName,
		)
	}
	return append(commands, hudSetupCommands(plan.Stream.HUDMode)...)
}

func ffmpegSettingName(crf int) string {
	return fmt.Sprintf("zvFfmpegYuv420pCrf%d", crf)
}

func ffmpegSettingsCommand(name string, crf int) string {
	return fmt.Sprintf(
		`mirv_streams settings add ffmpeg %s "-c:v libx264 -preset fast -crf %d -pix_fmt yuv420p {QUOTE}{AFX_STREAM_PATH}\video.mp4{QUOTE}"`,
		name,
		crf,
	)
}

func hudSetupCommands(mode HUDMode) []string {
	switch mode {
	case HUDModeClean:
		return []string{
			"spec_show_xray 0; cl_drawhud 0",
		}
	default:
		return []string{
			"spec_show_xray 0",
			"cl_spec_show_bindings 0",
			"cl_drawhud 1",
			"cl_draw_only_deathnotices 0",
			"cl_show_observer_crosshair 2",
			"crosshair 1",
		}
	}
}

func slashPath(path string) string {
	return strings.ReplaceAll(path, `\`, `/`)
}

func formatFloat(v float64) string {
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.3f", v), "0"), ".")
}
