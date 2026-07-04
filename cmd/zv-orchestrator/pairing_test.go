package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestEnsurePairingTokenGeneratesAndPersists(t *testing.T) {
	dir := t.TempDir()

	token, err := ensurePairingToken(dir, "")
	if err != nil {
		t.Fatalf("ensurePairingToken error = %v", err)
	}
	// 32 bytes base64url without padding is 43 characters.
	if len(token) != 43 {
		t.Fatalf("token length = %d (%q), want 43", len(token), token)
	}

	tokenPath := filepath.Join(dir, pairingTokenFile)
	info, err := os.Stat(tokenPath)
	if err != nil {
		t.Fatalf("stat token file: %v", err)
	}
	if runtime.GOOS != "windows" {
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Fatalf("token file perm = %o, want 600", perm)
		}
	}

	data, err := os.ReadFile(tokenPath)
	if err != nil {
		t.Fatalf("read token file: %v", err)
	}
	if string(data) != token {
		t.Fatalf("persisted token = %q, want %q", string(data), token)
	}
}

func TestEnsurePairingTokenStableOnReread(t *testing.T) {
	dir := t.TempDir()

	first, err := ensurePairingToken(dir, "")
	if err != nil {
		t.Fatalf("first ensurePairingToken error = %v", err)
	}
	second, err := ensurePairingToken(dir, "")
	if err != nil {
		t.Fatalf("second ensurePairingToken error = %v", err)
	}
	if first != second {
		t.Fatalf("token changed on re-read: %q then %q", first, second)
	}
}

func TestEnsurePairingTokenConfiguredTakesPrecedence(t *testing.T) {
	dir := t.TempDir()

	// Seed an existing generated token, then confirm a configured token overrides
	// it and is written to disk.
	if _, err := ensurePairingToken(dir, ""); err != nil {
		t.Fatalf("seed ensurePairingToken error = %v", err)
	}
	const configured = "configured-token-value"
	token, err := ensurePairingToken(dir, configured)
	if err != nil {
		t.Fatalf("ensurePairingToken error = %v", err)
	}
	if token != configured {
		t.Fatalf("token = %q, want configured %q", token, configured)
	}
	data, err := os.ReadFile(filepath.Join(dir, pairingTokenFile))
	if err != nil {
		t.Fatalf("read token file: %v", err)
	}
	if string(data) != configured {
		t.Fatalf("persisted token = %q, want %q", string(data), configured)
	}
}
