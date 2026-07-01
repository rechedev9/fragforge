package agent

import "context"

// Pair exchanges a one-time pairing code for a durable agent token.
func Pair(ctx context.Context, baseURL, code, name string) (string, string, error) {
	c := NewClient(baseURL, "")
	var out struct {
		Token   string `json:"token"`
		AgentID string `json:"agentId"`
	}
	if _, err := c.Do(ctx, "POST", "/api/agent/pair", map[string]string{"code": code, "name": name}, &out); err != nil {
		return "", "", err
	}
	return out.Token, out.AgentID, nil
}
