package editor

import (
	"fmt"
	"strings"
)

func GenerateCoverPrompt(short ShortEdit) string {
	mapLine := "Map: CS2 match map not available; keep the environment authentic but map-neutral."
	if short.Map != "" {
		mapLine = "Map: " + short.Map
	}
	weapons := uniqueNonEmpty(append([]string{short.PrimaryWeapon}, killWeapons(short.Kills)...))
	victims := uniqueNonEmpty(killVictims(short.Kills))
	headshots := 0
	for _, kill := range short.Kills {
		if kill.Headshot {
			headshots++
		}
	}

	var sb strings.Builder
	sb.WriteString("# GPT Image Cover Prompt\n\n")
	sb.WriteString("Create a premium 9:16 cover image for a Counter-Strike 2 short. Use this as a GPT Image prompt, not as a video edit instruction.\n\n")
	sb.WriteString("Core details:\n")
	sb.WriteString("- Player: " + short.Player + "\n")
	sb.WriteString("- " + mapLine + "\n")
	sb.WriteString(fmt.Sprintf("- Highlight: %dK", short.KillCount))
	if headshots > 0 {
		sb.WriteString(fmt.Sprintf(", %d headshot", headshots))
		if headshots > 1 {
			sb.WriteString("s")
		}
	}
	sb.WriteString("\n")
	if len(weapons) > 0 {
		sb.WriteString("- Weapon focus: " + strings.Join(weapons, ", ") + "\n")
	}
	if len(victims) > 0 {
		sb.WriteString("- Defeated opponents from metadata: " + strings.Join(victims, ", ") + "\n")
	}
	sb.WriteString("\nReference assets:\n")
	if short.CoverPath != "" {
		sb.WriteString("- Gameplay frame: " + short.CoverPath + "\n")
	}
	if short.PlayerImage != "" {
		sb.WriteString("- Player cutout/reference: " + short.PlayerImage + "\n")
	} else {
		sb.WriteString("- Player cutout/reference: not provided; do not invent a face or team jersey.\n")
	}
	if short.Output != "" {
		sb.WriteString("- Source short: " + short.Output + "\n")
	}
	sb.WriteString("\nVisual direction:\n")
	sb.WriteString("Premium esports thumbnail, clean and realistic, centered on POV match energy, sharp action framing, refined contrast, crisp weapon emphasis, subtle map atmosphere, minimal clutter. If a player cutout is provided, place the player in the lower third without hiding the crosshair moment or weapon identity.\n\n")
	sb.WriteString("Text direction:\n")
	sb.WriteString(fmt.Sprintf("If text is used, keep it minimal and readable: \"%s\" and \"%dK\" only. Do not invent extra names or stats.\n\n", short.Player, short.KillCount))
	sb.WriteString("Avoid:\n")
	sb.WriteString("Fake scoreboard UI, fake killfeed, random team logos, meme graphics, excessive glow, unreadable typography, generic soldier art, and inaccurate weapons.\n")
	return sb.String()
}

func killWeapons(kills []KillCue) []string {
	out := make([]string, 0, len(kills))
	for _, kill := range kills {
		out = append(out, kill.Weapon)
	}
	return out
}

func killVictims(kills []KillCue) []string {
	out := make([]string, 0, len(kills))
	for _, kill := range kills {
		out = append(out, kill.Victim)
	}
	return out
}

func uniqueNonEmpty(values []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}
