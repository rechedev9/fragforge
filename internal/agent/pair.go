package agent

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
)

// GenerateLoopbackToken returns a 32-byte crypto/rand token encoded as
// URL-safe base64 with no padding. The agent persists it and requires it as a
// Bearer credential on every loopback data-plane request.
func GenerateLoopbackToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate loopback token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

// Pair exchanges a one-time pairing code for a durable agent token. It also
// registers the agent's loopback token and proxy port so the control plane can
// hand them to the browser via GET /api/pc/status.
func Pair(ctx context.Context, baseURL, code, name, loopbackToken string, loopbackPort int) (string, string, error) {
	c := NewClient(baseURL, "")
	var out struct {
		Token   string `json:"token"`
		AgentID string `json:"agentId"`
	}
	body := map[string]any{
		"code":          code,
		"name":          name,
		"loopbackToken": loopbackToken,
		"loopbackPort":  loopbackPort,
	}
	if _, err := c.Do(ctx, "POST", "/api/agent/pair", body, &out); err != nil {
		return "", "", err
	}
	return out.Token, out.AgentID, nil
}
