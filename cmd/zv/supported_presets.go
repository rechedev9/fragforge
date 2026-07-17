package main

import "github.com/rechedev9/fragforge/internal/editor"

func supportedPresetNames() []string {
	return []string{editor.DefaultPreset().Name}
}

func supportedPresetByName(name string) (editor.RenderPreset, bool) {
	preset := editor.DefaultPreset()
	if name != preset.Name {
		return editor.RenderPreset{}, false
	}
	return preset, true
}
