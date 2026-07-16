package main

import "testing"

func TestValidateMusicVolume(t *testing.T) {
	cases := []struct {
		name    string
		volume  float64
		wantErr bool
	}{
		{"default", 1.0, false},
		{"low bound", 0.01, false},
		{"midrange", 0.35, false},
		{"zero rejected", 0, true},
		{"negative rejected", -0.5, true},
		{"above one rejected", 1.5, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateMusicVolume(tc.volume)
			if (err != nil) != tc.wantErr {
				t.Fatalf("validateMusicVolume(%v) error = %v, wantErr %v", tc.volume, err, tc.wantErr)
			}
		})
	}
}
