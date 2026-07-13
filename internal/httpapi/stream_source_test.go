package httpapi

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGetStreamSourceDetectsUploadedContainerInsteadOfTrustingStorageKey(t *testing.T) {
	tests := []struct {
		name            string
		filename        string
		body            []byte
		wantContentType string
	}{
		{
			name:            "mp4",
			filename:        "stream.mp4",
			body:            []byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm', 0, 0, 2, 0, 'm', 'p', '4', '2', 'i', 's', 'o', 'm'},
			wantContentType: "video/mp4",
		},
		{
			name:            "webm",
			filename:        "stream.webm",
			body:            []byte{0x1a, 0x45, 0xdf, 0xa3, 0x9f, 0x42, 0x86, 0x81},
			wantContentType: "video/webm",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testGetStreamSourceContentType(t, tt.filename, tt.body, tt.wantContentType)
		})
	}
}

func testGetStreamSourceContentType(t *testing.T, filename string, contents []byte, wantContentType string) {
	t.Helper()
	streamRepo := newFakeStreamRepo()
	store := newFakeStorage()
	h := NewHandlers(newFakeRepo(), store, &fakeQueue{}, WithStreamRepository(streamRepo))
	router := Routes(h)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("video", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(contents); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteField("config", `{}`); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/stream-jobs", body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d; body=%s", response.Code, http.StatusCreated, response.Body.String())
	}
	createdID := strings.TrimSpace(jsonStringField(t, response.Body.Bytes(), "id"))

	request = httptest.NewRequest(http.MethodGet, "/api/stream-jobs/"+createdID+"/source", nil)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("source status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	if got := response.Header().Get("Content-Type"); got != wantContentType {
		t.Fatalf("Content-Type = %q, want %q", got, wantContentType)
	}
}

func jsonStringField(t *testing.T, document []byte, field string) string {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal(document, &value); err != nil {
		t.Fatal(err)
	}
	result, ok := value[field].(string)
	if !ok {
		t.Fatalf("%s is not a string", field)
	}
	return result
}
