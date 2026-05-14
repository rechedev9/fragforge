package job

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestStatusStringMapping(t *testing.T) {
	cases := map[Status]string{
		StatusQueued:  "queued",
		StatusParsing: "parsing",
		StatusParsed:  "parsed",
		StatusFailed:  "failed",
	}
	for s, want := range cases {
		if got := s.String(); got != want {
			t.Errorf("Status(%d).String() = %q, want %q", s, got, want)
		}
	}
}

func TestParseStatusValid(t *testing.T) {
	s, err := ParseStatus("parsed")
	if err != nil {
		t.Fatalf("ParseStatus(parsed) error = %v", err)
	}
	if s != StatusParsed {
		t.Errorf("ParseStatus(parsed) = %v, want %v", s, StatusParsed)
	}
}

func TestParseStatusInvalid(t *testing.T) {
	if _, err := ParseStatus("bogus"); err == nil {
		t.Error("ParseStatus(bogus) error = nil, want error")
	}
}

func TestJobMarshalsToExpectedShape(t *testing.T) {
	j := Job{
		ID:            uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		Status:        StatusQueued,
		DemoPath:      "/tmp/x.dem",
		DemoSHA256:    "abc",
		TargetSteamID: "76561198000000000",
		CreatedAt:     time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC),
		UpdatedAt:     time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC),
	}
	b, err := json.Marshal(j)
	if err != nil {
		t.Fatalf("Marshal error = %v", err)
	}
	out := string(b)
	if !contains(out, `"status":"queued"`) {
		t.Errorf("status not rendered as string: %s", out)
	}
	if !contains(out, `"id":"11111111-1111-1111-1111-111111111111"`) {
		t.Errorf("id not rendered as UUID string: %s", out)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0)
}
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
