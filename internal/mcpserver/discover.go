package mcpserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/rechedev9/fragforge/internal/tuiclient"
)

// The orchestrator URL is resolved with the precedence order documented in the
// zv mcp spec: an explicit flag, then $ORCHESTRATOR_URL, then the Electron
// desktop app's per-install ports.json, then the dev default. The source is
// tracked so Run can log how the connection was found (stderr only).
const (
	sourceFlag    = "flag"
	sourceEnv     = "env"
	sourcePorts   = "ports.json"
	sourceDefault = "default"

	// appName is the Electron app.name (desktop/package.json "name"), which
	// determines the userData directory that holds ports.json.
	appName = "fragforge-studio"

	// userDataDirEnv overrides the resolved Electron userData directory. It is a
	// test-only seam so ports.json discovery can be exercised on any OS without
	// mocking platform paths; production never sets it.
	userDataDirEnv = "FRAGFORGE_USERDATA_DIR"
)

// Resolution is the outcome of URL discovery: the orchestrator base URL and how
// it was found.
type Resolution struct {
	URL    string
	Source string
}

// Resolve applies the precedence order and returns the orchestrator base URL.
func Resolve(flagURL string) Resolution {
	if flagURL != "" {
		return Resolution{URL: strings.TrimRight(flagURL, "/"), Source: sourceFlag}
	}
	if env := strings.TrimRight(os.Getenv("ORCHESTRATOR_URL"), "/"); env != "" {
		return Resolution{URL: env, Source: sourceEnv}
	}
	if port := portFromPortsFile(); port > 0 {
		return Resolution{URL: fmt.Sprintf("http://127.0.0.1:%d", port), Source: sourcePorts}
	}
	return Resolution{URL: tuiclient.DefaultBaseURL, Source: sourceDefault}
}

// portFromPortsFile reads the Electron desktop app's ports.json and returns the
// orchestrator port, or 0 when the file is missing, unreadable, malformed, or
// carries no positive orchestrator port (any of which falls through to the
// default in Resolve).
func portFromPortsFile() int {
	dir, err := userDataDir()
	if err != nil {
		return 0
	}
	// #nosec G304 -- fixed filename inside a per-user config directory.
	b, err := os.ReadFile(filepath.Join(dir, "ports.json"))
	if err != nil {
		return 0
	}
	var ports struct {
		Orchestrator int `json:"orchestrator"`
	}
	if err := json.Unmarshal(b, &ports); err != nil {
		return 0
	}
	if ports.Orchestrator <= 0 {
		return 0
	}
	return ports.Orchestrator
}

// userDataDir returns the Electron app.getPath('userData') directory for the
// FragForge Studio app, per OS. The userDataDirEnv override wins first so tests
// can point discovery at a temporary directory.
func userDataDir() (string, error) {
	if override := os.Getenv(userDataDirEnv); override != "" {
		return override, nil
	}
	switch runtime.GOOS {
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData == "" {
			return "", errors.New("APPDATA is not set")
		}
		return filepath.Join(appData, appName), nil
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "Application Support", appName), nil
	default:
		if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
			return filepath.Join(xdg, appName), nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".config", appName), nil
	}
}
