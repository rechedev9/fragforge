package main

import (
	"reflect"
	"testing"
)

func TestParseDroppedPaths(t *testing.T) {
	tests := []struct {
		name string
		text string
		want []string
	}{
		{"empty", "  ", nil},
		{"bare path", `C:\demos\match.dem`, []string{`C:\demos\match.dem`}},
		{"quoted path with spaces", `"C:\my demos\match.dem"`, []string{`C:\my demos\match.dem`}},
		{"two quoted paths", `"C:\a.dem" "C:\b.dem"`, []string{`C:\a.dem`, `C:\b.dem`}},
		{"single quotes", `'/tmp/match one.dem'`, []string{`/tmp/match one.dem`}},
		{"trailing newline", "C:\\demos\\match.dem\r\n", []string{`C:\demos\match.dem`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseDroppedPaths(tt.text)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseDroppedPaths(%q) got %v, want %v", tt.text, got, tt.want)
			}
		})
	}
}

func TestClassifyDrop(t *testing.T) {
	tests := []struct {
		path string
		want dropKind
	}{
		{`C:\demos\match.dem`, dropDemo},
		{`C:\demos\MATCH.DEM`, dropDemo},
		{`clip.mp4`, dropStream},
		{`clip.MOV`, dropStream},
		{`clip.mkv`, dropStream},
		{`clip.webm`, dropStream},
		{`notes.txt`, dropUnknown},
		{`noext`, dropUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := classifyDrop(tt.path); got != tt.want {
				t.Errorf("classifyDrop(%q) got %d, want %d", tt.path, got, tt.want)
			}
		})
	}
}
