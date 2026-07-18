package httpapi

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/rechedev9/fragforge/internal/storage"
	"github.com/rechedev9/fragforge/internal/voiceprofile"
)

func TestVoiceProfileHTTPRoundTrip(t *testing.T) {
	local, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal error = %v", err)
	}
	h := NewHandlers(nil, local, nil)
	r := chi.NewRouter()
	r.Put("/api/voice-profiles/{id}", h.PutVoiceProfile)
	r.Get("/api/voice-profiles/{id}", h.GetVoiceProfile)
	r.Get("/api/voice-profiles/{id}/audio", h.GetVoiceProfileAudio)
	r.Delete("/api/voice-profiles/{id}", h.DeleteVoiceProfile)

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("voice", "sample.ogg")
	if err != nil {
		t.Fatalf("CreateFormFile error = %v", err)
	}
	wantAudio := validOggOpus()
	if _, err := part.Write(wantAudio); err != nil {
		t.Fatalf("write audio error = %v", err)
	}
	_ = mw.WriteField("name", "Mi voz")
	_ = mw.WriteField("channel", "RaizerinhoCS2")
	_ = mw.WriteField("locale", "es-ES")
	if err := mw.Close(); err != nil {
		t.Fatalf("multipart close error = %v", err)
	}

	put := httptest.NewRequest(http.MethodPut, "/api/voice-profiles/raizerinhocs2", &body)
	put.Header.Set("Content-Type", mw.FormDataContentType())
	putRec := httptest.NewRecorder()
	r.ServeHTTP(putRec, put)
	if putRec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d body=%s", putRec.Code, putRec.Body.String())
	}
	var profile voiceProfileResponse
	if err := json.Unmarshal(putRec.Body.Bytes(), &profile); err != nil {
		t.Fatalf("decode profile error = %v", err)
	}
	if profile.Channel != "RaizerinhoCS2" || profile.ContentType != "audio/ogg" || profile.AudioURL == "" {
		t.Fatalf("profile = %#v", profile)
	}

	getRec := httptest.NewRecorder()
	r.ServeHTTP(getRec, httptest.NewRequest(http.MethodGet, "/api/voice-profiles/raizerinhocs2", nil))
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET status = %d body=%s", getRec.Code, getRec.Body.String())
	}

	audioRec := httptest.NewRecorder()
	r.ServeHTTP(audioRec, httptest.NewRequest(http.MethodGet, "/api/voice-profiles/raizerinhocs2/audio", nil))
	if audioRec.Code != http.StatusOK || !bytes.Equal(audioRec.Body.Bytes(), wantAudio) {
		t.Fatalf("audio status=%d body=%q", audioRec.Code, audioRec.Body.Bytes())
	}
	if got := audioRec.Header().Get("Content-Type"); got != "audio/ogg" {
		t.Fatalf("Content-Type = %q", got)
	}

	deleteRec := httptest.NewRecorder()
	r.ServeHTTP(deleteRec, httptest.NewRequest(http.MethodDelete, "/api/voice-profiles/raizerinhocs2", nil))
	if deleteRec.Code != http.StatusNoContent {
		t.Fatalf("DELETE status = %d body=%s", deleteRec.Code, deleteRec.Body.String())
	}
	_, err = voiceprofile.New(local).Get("raizerinhocs2")
	if err == nil {
		t.Fatal("profile still exists after DELETE")
	}
}

func TestPutVoiceProfileRejectsNonAudio(t *testing.T) {
	local, _ := storage.NewLocal(t.TempDir())
	h := NewHandlers(nil, local, nil)
	r := chi.NewRouter()
	r.Put("/api/voice-profiles/{id}", h.PutVoiceProfile)

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, _ := mw.CreateFormFile("voice", "not-audio.txt")
	_, _ = part.Write([]byte("this is not audio"))
	_ = mw.Close()

	req := httptest.NewRequest(http.MethodPut, "/api/voice-profiles/raizerinhocs2", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestPutVoiceProfileRejectsOversizedMultipart(t *testing.T) {
	local, _ := storage.NewLocal(t.TempDir())
	h := NewHandlers(nil, local, nil)
	r := chi.NewRouter()
	r.Put("/api/voice-profiles/{id}", h.PutVoiceProfile)

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, _ := mw.CreateFormFile("voice", "too-large.ogg")
	_, _ = part.Write(append([]byte("OggS"), bytes.Repeat([]byte{0x42}, maxVoiceMultipartBytes)...))
	_ = mw.Close()

	req := httptest.NewRequest(http.MethodPut, "/api/voice-profiles/raizerinhocs2", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestPutVoiceProfileCleansMultipartTempFilesOnParseError(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("TMP", tempDir)
	t.Setenv("TEMP", tempDir)
	local, _ := storage.NewLocal(t.TempDir())
	h := NewHandlers(nil, local, nil)
	r := chi.NewRouter()
	r.Put("/api/voice-profiles/{id}", h.PutVoiceProfile)

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, _ := mw.CreateFormFile("voice", "large.ogg")
	_, _ = part.Write(bytes.Repeat([]byte{0x42}, voiceMultipartMemory+1))
	_ = mw.Close()

	req := httptest.NewRequest(http.MethodPut, "/api/voice-profiles/raizerinhocs2?bad=one;two", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	entries, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatalf("ReadDir error = %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("temporary multipart files remain: %v", entries)
	}
}

func validOggOpus() []byte {
	page := func(headerType byte, sequence uint32, payload []byte) []byte {
		header := make([]byte, 28)
		copy(header[:4], "OggS")
		header[4] = 0
		header[5] = headerType
		binary.LittleEndian.PutUint32(header[14:18], 1)
		binary.LittleEndian.PutUint32(header[18:22], sequence)
		header[26] = 1
		header[27] = byte(len(payload))
		body := append(header, payload...)
		binary.LittleEndian.PutUint32(body[22:26], testOggCRC(body))
		return body
	}
	head := make([]byte, 19)
	copy(head, "OpusHead")
	head[8] = 1
	head[9] = 1
	binary.LittleEndian.PutUint16(head[10:12], 312)
	binary.LittleEndian.PutUint32(head[12:16], 48000)
	tags := make([]byte, 16)
	copy(tags, "OpusTags")
	body := append(page(0x02, 0, head), page(0x00, 1, tags)...)
	return append(body, page(0x04, 2, []byte{0xf8, 0xff, 0xfe})...)
}

func testOggCRC(page []byte) uint32 {
	var crc uint32
	for index, value := range page {
		if index >= 22 && index < 26 {
			value = 0
		}
		crc ^= uint32(value) << 24
		for range 8 {
			if crc&0x80000000 != 0 {
				crc = crc<<1 ^ 0x04c11db7
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}
