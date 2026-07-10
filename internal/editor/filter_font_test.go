package editor

import (
	"path/filepath"
	"testing"

	"github.com/rechedev9/fragforge/internal/mediafont"
)

func TestDefaultDrawtextFontsUseEmbeddedMontserrat(t *testing.T) {
	regular := drawtextFontFile()
	bold := boldDrawtextFontFile()
	if filepath.Base(regular) != mediafont.FileName {
		t.Fatalf("regular drawtext font = %q, want %s", regular, mediafont.FileName)
	}
	if bold != regular {
		t.Fatalf("bold drawtext font = %q, want deterministic font %q", bold, regular)
	}
}
