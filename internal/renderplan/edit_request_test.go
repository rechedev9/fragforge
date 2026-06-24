package renderplan

import (
	"strings"
	"testing"
)

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
