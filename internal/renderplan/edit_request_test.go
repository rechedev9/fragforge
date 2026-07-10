package renderplan

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEditRequestSerializesAutomaticTextControls(t *testing.T) {
	b, err := json.Marshal(EditRequest{HookText: true, KillCounter: false})
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	if !strings.Contains(got, `"hook_text":true`) || !strings.Contains(got, `"kill_counter":false`) {
		t.Fatalf("EditRequest JSON = %s, want explicit automatic text booleans", got)
	}
}

func TestNormalizeEditRequestDefaultsUnsetFields(t *testing.T) {
	got := NormalizeEditRequest(EditRequest{Intro: true})
	want := EditRequest{
		Format:     FormatShort9x16,
		KillEffect: KillEffectPunchIn,
		Transition: TransitionFlash,
		Intro:      true,
	}
	if got != want {
		t.Fatalf("edit request = %#v, want %#v", got, want)
	}
}

func TestEditRequestValidateRejectsUnknownFields(t *testing.T) {
	cases := []struct {
		name string
		req  EditRequest
		want string
	}{
		{name: "format", req: EditRequest{Format: "square", KillEffect: KillEffectPunchIn, Transition: TransitionFlash}, want: "unknown render format"},
		{name: "effect", req: EditRequest{Format: FormatShort9x16, KillEffect: "glitch", Transition: TransitionFlash}, want: "unknown kill effect"},
		{name: "transition", req: EditRequest{Format: FormatShort9x16, KillEffect: KillEffectPunchIn, Transition: "spin"}, want: "unknown transition"},
		{
			name: "intro text too long",
			req: EditRequest{
				Format: FormatShort9x16, KillEffect: KillEffectPunchIn, Transition: TransitionFlash,
				IntroText: strings.Repeat("a", 81),
			},
			want: "intro text exceeds 80 characters",
		},
		{
			name: "outro text too long",
			req: EditRequest{
				Format: FormatShort9x16, KillEffect: KillEffectPunchIn, Transition: TransitionFlash,
				OutroText: strings.Repeat("a", 81),
			},
			want: "outro text exceeds 80 characters",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.req.Validate()
			if err == nil {
				t.Fatal("Validate error = nil, want error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %q, want %q", err.Error(), tc.want)
			}
		})
	}
}

func TestEditRequestValidateAcceptsTextAtMaxLength(t *testing.T) {
	req := EditRequest{
		Format: FormatShort9x16, KillEffect: KillEffectPunchIn, Transition: TransitionFlash,
		IntroText: strings.Repeat("a", 80),
		OutroText: strings.Repeat("b", 80),
	}
	if err := req.Validate(); err != nil {
		t.Fatalf("Validate error = %v, want nil at the 80-char limit", err)
	}
}

func TestNormalizeEditRequestTrimsBookendTextWithoutEnablingBookends(t *testing.T) {
	got := NormalizeEditRequest(EditRequest{IntroText: "  Watch this ace  ", OutroText: "  follow for more  "})
	if got.IntroText != "Watch this ace" || got.OutroText != "follow for more" {
		t.Fatalf("bookend text = %q / %q, want trimmed", got.IntroText, got.OutroText)
	}
	if got.Intro || got.Outro {
		t.Fatalf("bookend bools = %v / %v, want false: setting text must not enable the bookend", got.Intro, got.Outro)
	}
}
