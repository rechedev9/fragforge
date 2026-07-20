package renderplan

import (
	"strings"
	"testing"

	"github.com/rechedev9/fragforge/internal/editor"
)

func TestGenerateIntentNormalizeDefaultsEdit(t *testing.T) {
	got := GenerateIntent{Variant: editor.PresetViral60Clean, Edit: EditRequest{Intro: true}}.Normalize()
	want := GenerateIntent{
		Variant: editor.PresetViral60Clean,
		Edit: EditRequest{
			Format:        FormatShort9x16,
			KillEffect:    KillEffectPunchIn,
			Transition:    TransitionFlash,
			Intro:         true,
			CoverStrategy: CoverStrategyGenerated,
		},
	}
	if got != want {
		t.Fatalf("intent = %#v, want %#v", got, want)
	}
}

func TestGenerateIntentValidate(t *testing.T) {
	valid := func() EditRequest {
		return EditRequest{Format: FormatShort9x16, KillEffect: KillEffectPunchIn, Transition: TransitionFlash}
	}
	cases := []struct {
		name    string
		intent  GenerateIntent
		wantErr string
	}{
		{
			name:   "valid",
			intent: GenerateIntent{Variant: editor.PresetCleanPOV60, Edit: valid()},
		},
		{
			name:    "unknown variant",
			intent:  GenerateIntent{Variant: "no-such-preset", Edit: valid()},
			wantErr: "unknown render variant",
		},
		{
			name:    "invalid edit",
			intent:  GenerateIntent{Variant: editor.PresetFullHUD60, Edit: EditRequest{Format: "square", KillEffect: KillEffectPunchIn, Transition: TransitionFlash}},
			wantErr: "unknown render format",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.intent.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate error = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate error = nil, want %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %q, want %q", err.Error(), tc.wantErr)
			}
		})
	}
}
