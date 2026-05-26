package editor

import (
	"strings"
	"testing"
)

func TestPublishSmokeTextUsesSearchableHumanTitle(t *testing.T) {
	title, caption, hashtags := publishSmokeText("iM", "de_inferno", SmokeCue{
		Type:        "smokegrenade",
		FromArea:    "CTSpawn",
		Destination: "T ramp",
		Matched:     true,
		ThrowAction: "jumpthrow",
		Stance:      "standing",
	})

	if title != "iM T Ramp Smoke from CT Spawn | Inferno CS2" {
		t.Fatalf("title = %q", title)
	}
	for _, want := range []string{
		"standing jumpthrow smoke",
		"CT Spawn to T Ramp",
		"CS2 Inferno utility reference",
	} {
		if !strings.Contains(caption, want) {
			t.Fatalf("caption %q missing %q", caption, want)
		}
	}
	if len(hashtags) > 5 {
		t.Fatalf("hashtags len = %d, want <= 5: %#v", len(hashtags), hashtags)
	}
	for _, want := range []string{"#CS2", "#CounterStrike2", "#Inferno", "#Smoke", "#CS2Lineups"} {
		if !containsString(hashtags, want) {
			t.Fatalf("hashtags = %#v missing %q", hashtags, want)
		}
	}
}

func TestPublishSmokeTextLabelsCrouchJumpthrow(t *testing.T) {
	_, caption, _ := publishSmokeText("iM", "de_inferno", SmokeCue{
		Type:        "smokegrenade",
		FromArea:    "CTSpawn",
		Destination: "Banana",
		Matched:     true,
		ThrowAction: "jumpthrow",
		Stance:      "crouching",
	})
	if !strings.Contains(caption, "crouch jumpthrow smoke") {
		t.Fatalf("caption = %q", caption)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
