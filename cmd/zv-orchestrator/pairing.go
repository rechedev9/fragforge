package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// pairingTokenFile is the basename, under the data dir, where the effective
// pairing token is persisted with 0600 perms so the user can copy it into the
// web UI on any run.
const pairingTokenFile = "agent-pairing.token"

// ensurePairingToken resolves the effective pairing token for hosted mode and
// persists it. Precedence: a configuredToken (ZV_MUTATION_TOKEN) always wins;
// otherwise an existing <dataDir>/agent-pairing.token is reused; otherwise a new
// 32-byte base64url (no padding) token is generated. The effective token is
// always (re)written to the token file with 0600 perms.
func ensurePairingToken(dataDir, configuredToken string) (string, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return "", fmt.Errorf("creating data dir %q: %w", dataDir, err)
	}
	tokenPath := filepath.Join(dataDir, pairingTokenFile)

	token := strings.TrimSpace(configuredToken)
	if token == "" {
		if existing, err := os.ReadFile(tokenPath); err == nil {
			token = strings.TrimSpace(string(existing))
		}
	}
	if token == "" {
		var err error
		token, err = generatePairingToken()
		if err != nil {
			return "", err
		}
	}

	if err := os.WriteFile(tokenPath, []byte(token), 0o600); err != nil {
		return "", fmt.Errorf("writing pairing token %q: %w", tokenPath, err)
	}
	return token, nil
}

// generatePairingToken returns 32 random bytes encoded as base64url without
// padding (a 43-character string).
func generatePairingToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generating pairing token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// printPairingToken logs the FULL effective pairing token to stdout at startup
// so the user can copy it into the web UI.
func printPairingToken(token string) {
	fmt.Printf("agent: pairing token (paste into the web UI): %s\n", token)
}
