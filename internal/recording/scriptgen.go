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
		leadTicks := plan.Tickrate * 2
		seekTarget := max(1, s.TickStart-leadTicks)
		cameraTick := s.TickStart - plan.Tickrate
		if cameraTick <= seekTarget {
			cameraTick = seekTarget + max(1, plan.Tickrate/2)
		}
		if cameraTick >= s.TickStart {
			cameraTick = s.TickStart - 1
		}

		commands = append(commands,
			scheduledCommand{Tick: seekTick, Key: "seek-" + s.ID, Cmd: fmt.Sprintf("demo_gototick %d", seekTarget)},
			scheduledCommand{Tick: cameraTick, Key: "camera-" + s.ID, Cmd: cameraCommand(plan.TargetAccountID)},
			scheduledCommand{Tick: max(cameraTick+1, s.TickStart-1), Key: "camera-lock-" + s.ID, Cmd: cameraCommand(plan.TargetAccountID)},
			scheduledCommand{Tick: s.TickStart + max(1, plan.Tickrate/2), Key: "camera-relock-" + s.ID, Cmd: cameraCommand(plan.TargetAccountID)},
		)
		if i == 0 {
			commands = append(commands,
				scheduledCommand{Tick: max(seekTarget, s.TickStart-6), Key: "hide-demoui", Cmd: "demoui"},
			)
		}

		if plan.Runtime.HostTimescale > 0 && plan.Runtime.HostTimescale != 1 {
			commands = append(commands,
				scheduledCommand{
					Tick: max(1, s.TickStart-6),
					Key:  "timescale-up-" + s.ID,
					Cmd:  fmt.Sprintf("host_timescale %s", formatFloat(plan.Runtime.HostTimescale)),
				},
			)
		}

		commands = append(commands,
			scheduledCommand{Tick: s.TickStart, Key: "record-start-" + s.ID, Cmd: "mirv_streams record start"},
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
		scheduledCommand{Tick: lastEnd + pad/2, Key: "disconnect", Cmd: "disconnect"},
		scheduledCommand{Tick: lastEnd + pad, Key: "quit", Cmd: "quit"},
	)
	return commands
}

func cameraCommand(accountID uint32) string {
	return fmt.Sprintf("spec_player_by_accountid %d; spec_mode 4", accountID)
}

func streamSetupCommands(plan RecordingPlan) []string {
	recordName := slashPath(plan.OutputDir)
	recordFPS := fmt.Sprintf("mirv_streams record fps %d", plan.Stream.FPS)
	commands := []string{
		fmt.Sprintf(`mirv_streams record name "%s"`, recordName),
		recordFPS,
		"mirv_streams record screen enabled 1",
	}
	switch plan.Stream.Mode {
	case StreamModeTGASequence:
		commands = append(commands, "mirv_streams record screen settings afxClassic")
	default:
		commands = append(commands, "mirv_streams record screen settings afxFfmpegYuv420p")
	}
	return append(commands, hudSetupCommands(plan.Stream.HUDMode)...)
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
