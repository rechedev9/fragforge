package main

import "testing"

func TestParseClipRange(t *testing.T) {
	t.Run("valid with title", func(t *testing.T) {
		c, err := parseClipRange("10 25.5 Ace on mirage")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if c.StartSeconds != 10 || c.EndSeconds != 25.5 || c.Title != "Ace on mirage" {
			t.Fatalf("bad parse: %+v", c)
		}
	})
	t.Run("valid without title", func(t *testing.T) {
		c, err := parseClipRange("  0   12  ")
		if err != nil || c.StartSeconds != 0 || c.EndSeconds != 12 || c.Title != "" {
			t.Fatalf("bad parse: %+v err=%v", c, err)
		}
	})
	for _, bad := range []string{"", "10", "abc 20", "10 xyz", "20 10", "10 10", "-5 10"} {
		if _, err := parseClipRange(bad); err == nil {
			t.Errorf("expected error for %q", bad)
		}
	}
}
