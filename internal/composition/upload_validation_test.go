package composition

import (
	"strings"
	"testing"
)

func TestValidateUploadResultAcceptsSuccessfulOutput(t *testing.T) {
	err := ValidateUploadResult(Result{Output: "final.mp4"})
	if err != nil {
		t.Fatalf("ValidateUploadResult error = %v", err)
	}
}

func TestValidateUploadResultRejectsFailedResult(t *testing.T) {
	err := ValidateUploadResult(Result{Output: "final.mp4", Error: "compose failed"})
	if err == nil {
		t.Fatal("ValidateUploadResult error = nil, want composition result error")
	}
	if !strings.Contains(err.Error(), "composition result error: compose failed") {
		t.Fatalf("error = %q, want composition result error", err.Error())
	}
}

func TestValidateUploadResultRejectsMissingOutput(t *testing.T) {
	err := ValidateUploadResult(Result{})
	if err == nil {
		t.Fatal("ValidateUploadResult error = nil, want missing output")
	}
	if !strings.Contains(err.Error(), "composition result has no output") {
		t.Fatalf("error = %q, want missing output", err.Error())
	}
}
