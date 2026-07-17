package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunMissingDemoReturnsMachineReadableUsageError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--format", "json"}, &stdout, &stderr)
	if code != 2 || stderr.Len() != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
	var result struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.OK || result.Error != "--demo is required" {
		t.Fatalf("result = %#v", result)
	}
}

func TestRunHelpReturnsSuccess(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--help"}, &stdout, &stderr)
	if code != 0 || stderr.Len() != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
	if got := stdout.String(); got != usage {
		t.Fatalf("stdout = %q, want %q", got, usage)
	}
}

func TestWriteRosterJSONPersistsStructuredPlayerData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "players.json")
	want := rosterResult{
		SchemaVersion: "1.0", Demo: "match.dem", Map: "de_nuke", Tickrate: 64,
		Players: []playerStats{{SteamID64: 76561198377256168, Name: "Joey-", Team: "CT", Kills: 31, Deaths: 18}},
	}
	if err := writeRosterJSON(path, want); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got rosterResult
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(body, []byte(`"steamid64": "76561198377256168"`)) {
		t.Fatalf("steamid64 must be a lossless JSON string: %s", body)
	}
	if len(got.Players) != 1 || got.Players[0].Name != "Joey-" || got.Players[0].SteamID64 == 0 {
		t.Fatalf("got = %#v", got)
	}
}

func TestRunRefusesRosterOutputThatAliasesDemo(t *testing.T) {
	demoPath := filepath.Join(t.TempDir(), "match.dem")
	if err := os.WriteFile(demoPath, []byte("irreplaceable-demo"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := run([]string{"--demo", demoPath, "--out", demoPath, "--format", "json"}, &stdout, &stderr)
	if code != 2 || stderr.Len() != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
	var result struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Error, "must not overwrite --demo") {
		t.Fatalf("result = %#v", result)
	}
	body, err := os.ReadFile(demoPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(body), "irreplaceable-demo"; got != want {
		t.Fatalf("demo = %q, want %q", got, want)
	}
}
