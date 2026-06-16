package job

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestStatusStringMapping(t *testing.T) {
	cases := map[Status]string{
		StatusQueued:    "queued",
		StatusParsing:   "parsing",
		StatusParsed:    "parsed",
		StatusRecording: "recording",
		StatusRecorded:  "recorded",
		StatusComposing: "composing",
		StatusComposed:  "composed",
		StatusDone:      "done",
		StatusFailed:    "failed",
		StatusScanning:  "scanning",
		StatusScanned:   "scanned",
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
	if !strings.Contains(out, `"status":"queued"`) {
		t.Errorf("status not rendered as string: %s", out)
	}
	if !strings.Contains(out, `"id":"11111111-1111-1111-1111-111111111111"`) {
		t.Errorf("id not rendered as UUID string: %s", out)
	}
}

func TestStatusJSONRoundTrip(t *testing.T) {
	for _, s := range []Status{StatusQueued, StatusParsing, StatusParsed, StatusRecording, StatusRecorded, StatusComposing, StatusComposed, StatusDone, StatusFailed, StatusScanning, StatusScanned} {
		b, err := json.Marshal(s)
		if err != nil {
			t.Fatalf("marshal %v: %v", s, err)
		}
		var got Status
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatalf("unmarshal %s: %v", b, err)
		}
		if got != s {
			t.Errorf("round-trip: got %v, want %v", got, s)
		}
	}
}

func TestStatusUnmarshalRejectsUnknown(t *testing.T) {
	var s Status
	if err := json.Unmarshal([]byte(`"bogus"`), &s); err == nil {
		t.Error("Unmarshal(\"bogus\") error = nil, want error")
	}
}
