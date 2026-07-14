// Package killfeedvision reads a CS2 stream killfeed crop into structured kill
// notices using xAI's Grok multimodal chat API. It is the vision counterpart
// to internal/captions' xAI speech-to-text client: one bounded request, no
// retries, and it never logs or returns the API key.
package killfeedvision

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/rechedev9/fragforge/internal/streamclips"
)

// defaultBaseURL is the xAI API base used when Client.BaseURL is empty.
const defaultBaseURL = "https://api.x.ai/v1"

// DefaultModel is the cheap non-reasoning multimodal Grok tier used to read a
// killfeed crop when Client.Model is empty. Reading a kill-notice column is a
// simple perception task, so the reasoning tiers are not worth their cost: on a
// real three-kill AWP burst the reasoning tier misread the awp icon as an
// m4a1_silencer, which this tier reads correctly. Both tiers split names on the
// killfeed font's wide letter spacing; sanitizeName repairs that.
const DefaultModel = "grok-4.20-0309-non-reasoning"

// readTemperature pins sampling off. Reading a killfeed is perception, not
// composition: the same crop must always yield the same kills. Left to the API
// default, repeated reads of one verified frame disagreed with each other — four
// runs returned the victim as both "bek667" and "bek657", and one flipped every
// side and called an awp a deagle. There is nothing here to be creative about.
const readTemperature = 0

// defaultHTTPTimeout bounds a single killfeed read. The payload is one small
// PNG crop and a short JSON reply, so the round trip is quick.
const defaultHTTPTimeout = 60 * time.Second

// errorBodyMax bounds how much of a non-2xx response body is echoed in an
// error, so an unexpected large/HTML error page cannot bloat the message.
const errorBodyMax = 512

// successBodyMax bounds the JSON reply held in memory. A killfeed column holds
// only a handful of notices, so a well-formed reply stays far below this.
const successBodyMax int64 = 64 << 10

// maxKills caps how many notices a single read returns, matching the visible
// height of a CS2 killfeed column and guarding against a runaway reply.
const maxKills = 8

// Client reads a killfeed crop through xAI's multimodal chat API. A zero-value
// BaseURL, Model, or HTTPClient falls back to the package defaults.
type Client struct {
	APIKey     string
	BaseURL    string
	Model      string
	HTTPClient *http.Client
}

type chatRequest struct {
	Model          string         `json:"model"`
	ResponseFormat responseFormat `json:"response_format"`
	Temperature    float64        `json:"temperature"`
	Messages       []chatMessage  `json:"messages"`
}

type responseFormat struct {
	Type string `json:"type"`
}

type chatMessage struct {
	Role    string        `json:"role"`
	Content []contentPart `json:"content"`
}

type contentPart struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *imageURL `json:"image_url,omitempty"`
}

type imageURL struct {
	URL string `json:"url"`
}

// ReadKillfeed sends framePNG (the kill-notice crop of a stream frame) to xAI
// and returns the parsed, normalized kill notices top-to-bottom. It makes one
// request with no retries. Entries with an empty attacker/victim name, a side
// outside {CT,T}, or a weapon not in streamclips.WeaponKeys are dropped, and
// the result is capped at maxKills.
func (c *Client) ReadKillfeed(ctx context.Context, framePNG []byte) ([]streamclips.KillfeedKill, error) {
	if strings.TrimSpace(c.APIKey) == "" {
		return nil, fmt.Errorf("killfeedvision: xai api key not configured")
	}

	dataURI := "data:image/png;base64," + base64.StdEncoding.EncodeToString(framePNG)
	reqBody := chatRequest{
		Model:          c.model(),
		ResponseFormat: responseFormat{Type: "json_object"},
		Temperature:    readTemperature,
		Messages: []chatMessage{{
			Role: "user",
			Content: []contentPart{
				{Type: "image_url", ImageURL: &imageURL{URL: dataURI}},
				{Type: "text", Text: killfeedPrompt()},
			},
		}},
	}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("killfeedvision: building request: %w", err)
	}

	url := strings.TrimRight(c.baseURL(), "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("killfeedvision: building request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)

	client := c.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: defaultHTTPTimeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("killfeedvision: request failed: %w", err)
	}
	defer resp.Body.Close()

	maxBody := successBodyMax
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		maxBody = errorBodyMax
	}
	body, exceeded, err := readLimited(resp.Body, maxBody)
	if err != nil {
		return nil, fmt.Errorf("killfeedvision: reading response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if exceeded {
			body = body[:int(maxBody)]
		}
		return nil, chatError(resp.StatusCode, body)
	}
	if exceeded {
		return nil, fmt.Errorf("killfeedvision: response exceeds %d bytes", maxBody)
	}
	return parseKillfeed(body)
}

func (c *Client) baseURL() string {
	if b := strings.TrimSpace(c.BaseURL); b != "" {
		return b
	}
	return defaultBaseURL
}

func (c *Client) model() string {
	if m := strings.TrimSpace(c.Model); m != "" {
		return m
	}
	return DefaultModel
}

func killfeedPrompt() string {
	keys := strings.Join(streamclips.WeaponKeys(), ", ")
	return "The image is the kill-notice area of a CS2 stream frame. " +
		"Each notice reads left to right: the attacker's name, then the weapon icon, " +
		"then any modifier icons, then the victim's name on the right. " +
		"attacker_name is the LEFT name and victim_name is the RIGHT name. " +
		"Decide each side ONLY from the colour of that name's own text: " +
		"blue or cyan text means \"CT\", yellow or orange text means \"T\". " +
		"Set attacker_side from the colour of the LEFT name and victim_side from the colour of the " +
		"RIGHT name, judging each independently. Never infer a side from who killed whom, from which " +
		"side you assume the streamer plays, or from the notice's border colour. " +
		"Transcribe each name exactly as spelled, character by character, preserving case and digits. " +
		"The text is rendered with wide letter spacing: do NOT insert spaces inside a name, and do not " +
		"correct, complete, or guess at a name. " +
		"Identify the weapon from the icon's silhouette (for example a long-barrelled scoped rifle is an awp, " +
		"not an ak47), and prefer reporting nothing over a wrong guess. " +
		"List every fully visible kill notice from top to bottom as JSON of the form " +
		`{"kills":[{"attacker_side":"CT","attacker_name":"...","victim_side":"T","victim_name":"...",` +
		`"assister_side":"","assister_name":"","weapon":"ak47","headshot":false,"wallbang":false,` +
		`"noscope":false,"smoke":false,"blind":false,"in_air":false,"flash_assist":false}]}. ` +
		"Sides are exactly \"CT\" or \"T\". " +
		"weapon MUST be exactly one of these keys: " + keys + ". " +
		"Omit any notice you cannot read fully. Return only the JSON object."
}

func readLimited(r io.Reader, maxBytes int64) ([]byte, bool, error) {
	body, err := io.ReadAll(io.LimitReader(r, maxBytes+1))
	if err != nil {
		return nil, false, err
	}
	return body, int64(len(body)) > maxBytes, nil
}

// chatError builds a lowercase, actionable error from a non-2xx xAI response.
// It accepts xAI's live string-error envelope and the OpenAI-style object
// envelope, falling back to a bounded snippet of the raw body.
func chatError(status int, body []byte) error {
	msg := strings.TrimSpace(string(body))
	var stringEnvelope struct {
		Error string `json:"error"`
	}
	var objectEnvelope struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &stringEnvelope); err == nil && stringEnvelope.Error != "" {
		msg = stringEnvelope.Error
	} else if err := json.Unmarshal(body, &objectEnvelope); err == nil && objectEnvelope.Error.Message != "" {
		msg = objectEnvelope.Error.Message
	}
	msg = strings.ToLower(strings.TrimSpace(msg))
	if len(msg) > errorBodyMax {
		msg = strings.ToValidUTF8(msg[:errorBodyMax], "")
	}
	return fmt.Errorf("killfeedvision: xai request failed (status %d): %s", status, msg)
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

type killsEnvelope struct {
	Kills []streamclips.KillfeedKill `json:"kills"`
}

// parseKillfeed reads choices[0].message.content as a JSON kills object and
// returns the normalized, filtered notices.
func parseKillfeed(body []byte) ([]streamclips.KillfeedKill, error) {
	var resp chatResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("killfeedvision: invalid response json: %w", err)
	}
	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("killfeedvision: response has no choices")
	}
	content := stripJSONFences(resp.Choices[0].Message.Content)

	var envelope killsEnvelope
	if err := json.Unmarshal([]byte(content), &envelope); err != nil {
		return nil, fmt.Errorf("killfeedvision: invalid kills json: %w", err)
	}

	kills := make([]streamclips.KillfeedKill, 0, len(envelope.Kills))
	for _, k := range envelope.Kills {
		normalized, ok := normalizeKill(k)
		if !ok {
			continue
		}
		kills = append(kills, normalized)
		if len(kills) == maxKills {
			break
		}
	}
	return kills, nil
}

func normalizeKill(k streamclips.KillfeedKill) (streamclips.KillfeedKill, bool) {
	k.AttackerName = sanitizeName(k.AttackerName)
	k.VictimName = sanitizeName(k.VictimName)
	k.AssisterName = sanitizeName(k.AssisterName)
	k.AttackerSide = strings.ToUpper(strings.TrimSpace(k.AttackerSide))
	k.VictimSide = strings.ToUpper(strings.TrimSpace(k.VictimSide))
	k.AssisterSide = strings.ToUpper(strings.TrimSpace(k.AssisterSide))
	k.Weapon = strings.ToLower(strings.TrimSpace(k.Weapon))

	if k.AttackerName == "" || k.VictimName == "" {
		return streamclips.KillfeedKill{}, false
	}
	if !isSide(k.AttackerSide) || !isSide(k.VictimSide) {
		return streamclips.KillfeedKill{}, false
	}
	if !streamclips.ValidWeaponKey(k.Weapon) {
		return streamclips.KillfeedKill{}, false
	}
	return k, true
}

// sanitizeName strips every space inside a player name, not just the ends. It
// repairs an artifact of this reader, not a rule about names: the killfeed font
// renders names with wide letter spacing, and the model reads those gaps as word
// breaks — a real notice for "ZaCkk" killing "bek667" came back as "Za Ckk" and
// "be k6 67". Joining the fragments is the only repair available without a
// roster to check against. A genuinely spaced name would be collapsed too, which
// is acceptable here and only here: names typed in the editor are ground truth
// and are left alone, and the user can always correct an AI-read notice.
func sanitizeName(name string) string {
	return strings.Join(strings.Fields(name), "")
}

func isSide(side string) bool {
	return side == "CT" || side == "T"
}

// stripJSONFences removes a leading/trailing markdown code fence some models
// wrap JSON in, tolerating an optional language tag after the opening fence.
func stripJSONFences(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	s = strings.TrimPrefix(s, "```")
	if newline := strings.IndexByte(s, '\n'); newline >= 0 {
		// Drop an optional language tag (e.g. "json") on the opening fence line.
		if tag := strings.TrimSpace(s[:newline]); !strings.Contains(tag, "{") {
			s = s[newline+1:]
		}
	}
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}
