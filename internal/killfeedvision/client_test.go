package killfeedvision

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rechedev9/fragforge/internal/streamclips"
)

// validWeaponKey returns a weapon key the client will accept, taken from the
// live catalog so the test does not hardcode the icon set.
func validWeaponKey(t *testing.T) string {
	t.Helper()
	keys := streamclips.WeaponKeys()
	if len(keys) == 0 {
		t.Fatal("streamclips.WeaponKeys() returned no keys")
	}
	return keys[0]
}

// capturingServer records the last request body and headers, and replies with
// the given status and body.
type capturedRequest struct {
	authorization string
	body          []byte
}

func newServer(t *testing.T, status int, reply string, captured *capturedRequest) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("got request path %q, want /chat/completions", r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("reading request body: %v", err)
		}
		if captured != nil {
			captured.authorization = r.Header.Get("Authorization")
			captured.body = body
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, reply)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func chatReply(t *testing.T, content string) string {
	t.Helper()
	envelope := map[string]any{
		"choices": []any{
			map[string]any{
				"message": map[string]any{"content": content},
			},
		},
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshaling reply: %v", err)
	}
	return string(data)
}

func TestReadKillfeedHappyPath(t *testing.T) {
	weapon := validWeaponKey(t)
	kills := `{"kills":[{"attacker_side":"ct","attacker_name":"  s1mple  ","victim_side":"t","victim_name":"ZywOo","weapon":"` + strings.ToUpper(weapon) + `","headshot":true}]}`
	var captured capturedRequest
	srv := newServer(t, http.StatusOK, chatReply(t, kills), &captured)

	client := &Client{APIKey: "test-key", BaseURL: srv.URL}
	framePNG := []byte("fake-png-bytes")
	got, err := client.ReadKillfeed(context.Background(), framePNG)
	if err != nil {
		t.Fatalf("ReadKillfeed: %v", err)
	}

	if want := "Bearer test-key"; captured.authorization != want {
		t.Errorf("got authorization %q, want %q", captured.authorization, want)
	}

	var reqBody struct {
		Model          string `json:"model"`
		ResponseFormat struct {
			Type string `json:"type"`
		} `json:"response_format"`
		Messages []struct {
			Role    string `json:"role"`
			Content []struct {
				Type     string `json:"type"`
				Text     string `json:"text"`
				ImageURL struct {
					URL string `json:"url"`
				} `json:"image_url"`
			} `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(captured.body, &reqBody); err != nil {
		t.Fatalf("unmarshaling request body: %v", err)
	}
	if reqBody.Model != DefaultModel {
		t.Errorf("got model %q, want %q", reqBody.Model, DefaultModel)
	}
	if reqBody.ResponseFormat.Type != "json_object" {
		t.Errorf("got response_format.type %q, want json_object", reqBody.ResponseFormat.Type)
	}
	if len(reqBody.Messages) != 1 || len(reqBody.Messages[0].Content) != 2 {
		t.Fatalf("got %d messages, want 1 with 2 content parts", len(reqBody.Messages))
	}
	wantURI := "data:image/png;base64," + base64.StdEncoding.EncodeToString(framePNG)
	var sawImage, sawText bool
	for _, part := range reqBody.Messages[0].Content {
		switch part.Type {
		case "image_url":
			sawImage = true
			if part.ImageURL.URL != wantURI {
				t.Errorf("got image url %q, want %q", part.ImageURL.URL, wantURI)
			}
		case "text":
			sawText = true
			if part.Text == "" {
				t.Error("text part is empty")
			}
		}
	}
	if !sawImage || !sawText {
		t.Errorf("missing content parts: image=%v text=%v", sawImage, sawText)
	}

	if len(got) != 1 {
		t.Fatalf("got %d kills, want 1", len(got))
	}
	k := got[0]
	if k.AttackerName != "s1mple" {
		t.Errorf("got attacker name %q, want s1mple", k.AttackerName)
	}
	if k.AttackerSide != "CT" || k.VictimSide != "T" {
		t.Errorf("got sides %q/%q, want CT/T", k.AttackerSide, k.VictimSide)
	}
	if k.Weapon != weapon {
		t.Errorf("got weapon %q, want %q", k.Weapon, weapon)
	}
	if !k.Headshot {
		t.Error("expected headshot true")
	}
}

func TestReadKillfeedFencedJSON(t *testing.T) {
	weapon := validWeaponKey(t)
	content := "```json\n{\"kills\":[{\"attacker_side\":\"T\",\"attacker_name\":\"donk\",\"victim_side\":\"CT\",\"victim_name\":\"m0nesy\",\"weapon\":\"" + weapon + "\"}]}\n```"
	srv := newServer(t, http.StatusOK, chatReply(t, content), nil)

	client := &Client{APIKey: "k", BaseURL: srv.URL}
	got, err := client.ReadKillfeed(context.Background(), []byte("png"))
	if err != nil {
		t.Fatalf("ReadKillfeed: %v", err)
	}
	if len(got) != 1 || got[0].AttackerName != "donk" {
		t.Fatalf("got %+v, want one kill by donk", got)
	}
}

func TestReadKillfeedInvalidWeaponDropped(t *testing.T) {
	weapon := validWeaponKey(t)
	kills := `{"kills":[` +
		`{"attacker_side":"CT","attacker_name":"keeper","victim_side":"T","victim_name":"dead","weapon":"not-a-real-weapon-xyz"},` +
		`{"attacker_side":"CT","attacker_name":"valid","victim_side":"T","victim_name":"gone","weapon":"` + weapon + `"}` +
		`]}`
	srv := newServer(t, http.StatusOK, chatReply(t, kills), nil)

	client := &Client{APIKey: "k", BaseURL: srv.URL}
	got, err := client.ReadKillfeed(context.Background(), []byte("png"))
	if err != nil {
		t.Fatalf("ReadKillfeed: %v", err)
	}
	if len(got) != 1 || got[0].AttackerName != "valid" {
		t.Fatalf("got %+v, want only the valid-weapon kill", got)
	}
}

func TestReadKillfeedNon2xxError(t *testing.T) {
	srv := newServer(t, http.StatusInternalServerError, `{"error":"boom"}`, nil)

	client := &Client{APIKey: "k", BaseURL: srv.URL}
	_, err := client.ReadKillfeed(context.Background(), []byte("png"))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error %q should include status 500", err.Error())
	}
}

func TestReadKillfeedEmptyKills(t *testing.T) {
	srv := newServer(t, http.StatusOK, chatReply(t, `{"kills":[]}`), nil)

	client := &Client{APIKey: "k", BaseURL: srv.URL}
	got, err := client.ReadKillfeed(context.Background(), []byte("png"))
	if err != nil {
		t.Fatalf("ReadKillfeed: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %d kills, want 0", len(got))
	}
}

func TestReadKillfeedMissingKey(t *testing.T) {
	client := &Client{BaseURL: "http://127.0.0.1:0"}
	_, err := client.ReadKillfeed(context.Background(), []byte("png"))
	if err == nil {
		t.Fatal("expected error for missing api key")
	}
	if !strings.Contains(err.Error(), "api key") {
		t.Errorf("error %q should mention api key", err.Error())
	}
}
