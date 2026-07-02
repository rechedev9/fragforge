package streamclips

import (
	"strings"
	"testing"
)

func TestVariantNamesAndDefault(t *testing.T) {
	names := VariantNames()
	want := []string{VariantStreamer4060, VariantStreamerVerticalStack, VariantStreamerFullframeNoCam}
	if len(names) != len(want) {
		t.Fatalf("VariantNames() = %v, want %v", names, want)
	}
	for i, name := range want {
		if names[i] != name {
			t.Fatalf("VariantNames()[%d] = %q, want %q", i, names[i], name)
		}
	}

	def := DefaultVariant()
	if def.Name != VariantStreamer4060 {
		t.Fatalf("DefaultVariant().Name = %q, want %q", def.Name, VariantStreamer4060)
	}
}

func TestVariantByName(t *testing.T) {
	tests := []struct {
		name string
		ok   bool
	}{
		{VariantStreamer4060, true},
		{VariantStreamerVerticalStack, true},
		{VariantStreamerFullframeNoCam, true},
		{"other", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ok := VariantByName(tt.name)
			if ok != tt.ok {
				t.Fatalf("VariantByName(%q) ok = %v, want %v", tt.name, ok, tt.ok)
			}
		})
	}
}

func TestVariant4060Geometry(t *testing.T) {
	v, ok := VariantByName(VariantStreamer4060)
	if !ok {
		t.Fatal("VariantByName(streamer-vertical-stack-40-60) ok = false")
	}
	if v.OutputWidth != 1080 || v.FaceOutputHeight != 768 || v.GameOutputHeight != 1152 {
		t.Fatalf("geometry = %+v, want 1080x768 face / 1080x1152 game", v)
	}
	if v.FullFrame {
		t.Fatal("streamer-vertical-stack-40-60 must not be full frame")
	}
}

func TestVariantLegacyGeometryUnchanged(t *testing.T) {
	v, ok := VariantByName(VariantStreamerVerticalStack)
	if !ok {
		t.Fatal("VariantByName(streamer-vertical-stack) ok = false")
	}
	if v.OutputWidth != 1080 || v.FaceOutputHeight != 520 || v.GameOutputHeight != 1400 {
		t.Fatalf("geometry = %+v, want 1080x520 face / 1080x1400 game", v)
	}
}

func TestVariantFullframeNoCamGeometry(t *testing.T) {
	v, ok := VariantByName(VariantStreamerFullframeNoCam)
	if !ok {
		t.Fatal("VariantByName(streamer-fullframe-nocam) ok = false")
	}
	if !v.FullFrame {
		t.Fatal("streamer-fullframe-nocam must be full frame")
	}
	if v.OutputWidth != 1080 || v.GameOutputHeight != 1920 {
		t.Fatalf("geometry = %+v, want 1080x1920 full frame", v)
	}
}

func TestDefaultEditPlanUsesDefaultVariantAndIsValid(t *testing.T) {
	plan := DefaultEditPlan()
	if plan.Variant != VariantStreamer4060 {
		t.Fatalf("DefaultEditPlan().Variant = %q, want %q", plan.Variant, VariantStreamer4060)
	}
	if err := plan.Validate(); err != nil {
		t.Fatalf("DefaultEditPlan().Validate() error = %v", err)
	}
}

func TestEditPlanValidateRejectsUnknownVariantWithValidList(t *testing.T) {
	plan := DefaultEditPlan()
	plan.Variant = "not-a-real-variant"

	err := plan.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want error")
	}
	for _, want := range []string{
		"unsupported stream render variant",
		VariantStreamer4060,
		VariantStreamerVerticalStack,
		VariantStreamerFullframeNoCam,
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("Validate() error = %q, want it to contain %q", err.Error(), want)
		}
	}
}

func TestEditPlanValidateFullframeNoCamIgnoresFaceCrop(t *testing.T) {
	plan := EditPlan{
		Variant:      VariantStreamerFullframeNoCam,
		GameplayCrop: CropRect{X: 0, Y: 0, Width: 1, Height: 1},
	}
	if err := plan.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil (face crop optional for fullframe-nocam)", err)
	}
}

func TestNormalizeEditPlanFillsDefaultVariant(t *testing.T) {
	plan := NormalizeEditPlan(EditPlan{})
	if plan.Variant != VariantStreamer4060 {
		t.Fatalf("NormalizeEditPlan({}).Variant = %q, want %q", plan.Variant, VariantStreamer4060)
	}
}
