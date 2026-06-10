package main

import "testing"

func TestInterpretShortPrompt(t *testing.T) {
	tests := []struct {
		name   string
		prompt string
		want   shortIntent
	}{
		{
			name:   "spanish all kills with player name",
			prompt: "haz un short con todas las kills de martinez",
			want:   shortIntent{TargetName: "martinez"},
		},
		{
			name:   "english all kills with player name",
			prompt: "make a short with all kills of s1mple",
			want:   shortIntent{TargetName: "s1mple"},
		},
		{
			name:   "steamid in prompt",
			prompt: "all kills of 76561198000000000",
			want:   shortIntent{TargetSteamID: "76561198000000000"},
		},
		{
			name:   "spanish best moments",
			prompt: "un short con las mejores jugadas de donk",
			want:   shortIntent{TargetName: "donk", BestMoments: true},
		},
		{
			name:   "english highlights",
			prompt: "highlights of niko, best moments only",
			want:   shortIntent{TargetName: "niko", BestMoments: true},
		},
		{
			name:   "spanish music intent routes to beat sync",
			prompt: "todas las kills de martinez al ritmo de la musica",
			want:   shortIntent{TargetName: "martinez", BeatSync: true},
		},
		{
			name:   "english beat sync intent",
			prompt: "all kills of zywoo synced to the beat",
			want:   shortIntent{TargetName: "zywoo", BeatSync: true},
		},
		{
			name:   "explicit preset name in prompt",
			prompt: "all kills of martinez with the natural-hq2-full preset",
			want:   shortIntent{TargetName: "martinez", Preset: "natural-hq2-full"},
		},
		{
			name:   "viral-beatsync preset implies beat sync",
			prompt: "todas las kills de martinez con viral-beatsync",
			want:   shortIntent{TargetName: "martinez", Preset: "viral-beatsync", BeatSync: true},
		},
		{
			name:   "player keyword",
			prompt: "best moments, player donk",
			want:   shortIntent{TargetName: "donk", BestMoments: true},
		},
		{
			name:   "no player mentioned",
			prompt: "haz un short con todas las kills de la partida",
			want:   shortIntent{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := interpretShortPrompt(tt.prompt)
			if got != tt.want {
				t.Fatalf("interpretShortPrompt(%q) = %+v, want %+v", tt.prompt, got, tt.want)
			}
		})
	}
}
