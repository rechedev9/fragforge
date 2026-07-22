package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"
)

// withURLParam injects a chi route param so a handler can be unit-tested without
// routing the request through the full router.
func withURLParam(r *http.Request, key, val string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, val)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func writeMusicDir(t *testing.T, catalog string, files map[string][]byte) string {
	t.Helper()
	dir := t.TempDir()
	if catalog != "" {
		if err := os.WriteFile(filepath.Join(dir, "catalog.json"), []byte(catalog), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func decodeSongs(t *testing.T, body []byte) []song {
	t.Helper()
	var doc struct {
		Songs []song `json:"songs"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("decode songs: %v (body=%s)", err, body)
	}
	return doc.Songs
}

func TestListSongsCuratedFromCatalog(t *testing.T) {
	catalog := `{"tracks":[
		{"id":"edm-1","title":"Neon Drive","artist":"FreePD","genre":"edm","durationSec":300,"ext":"mp3","license":"CC0"},
		{"id":"missing-1","title":"Gone","artist":"x","genre":"edm","durationSec":10,"ext":"mp3","license":"CC0"}
	]}`
	dir := writeMusicDir(t, catalog, map[string][]byte{
		"edm-1.mp3":            []byte("ID3fakeaudio"),
		"song-placeholder.m4a": []byte("placeholder bed"), // not in catalog -> excluded
	})
	h := NewHandlers(nil, nil, nil, WithMusicDir(dir))

	rec := httptest.NewRecorder()
	h.ListSongs(rec, httptest.NewRequest(http.MethodGet, "/api/songs", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	songs := decodeSongs(t, rec.Body.Bytes())
	// Only edm-1 has a backing file AND a catalog entry; missing-1 and the
	// placeholder bed are excluded.
	if len(songs) != 1 {
		t.Fatalf("got %d songs, want 1: %+v", len(songs), songs)
	}
	got := songs[0]
	if got.ID != "edm-1" || got.Title != "Neon Drive" || got.Genre != "edm" || got.License != "CC0" {
		t.Fatalf("song = %+v, want curated edm-1 metadata", got)
	}
	if got.AudioURL != "/api/songs/edm-1/audio" {
		t.Fatalf("audioUrl = %q, want /api/songs/edm-1/audio", got.AudioURL)
	}
}

func TestListSongsShippedCatalogIncludesViralPack(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "data", "music", "catalog.json"))
	if err != nil {
		t.Fatalf("read shipped music catalog: %v", err)
	}
	var catalog struct {
		Tracks []struct {
			ID  string `json:"id"`
			Ext string `json:"ext"`
		} `json:"tracks"`
	}
	if err := json.Unmarshal(raw, &catalog); err != nil {
		t.Fatalf("decode shipped music catalog: %v", err)
	}
	files := make(map[string][]byte, len(catalog.Tracks))
	for _, track := range catalog.Tracks {
		files[track.ID+"."+track.Ext] = []byte("provisioned audio")
	}
	dir := writeMusicDir(t, string(raw), files)
	h := NewHandlers(nil, nil, nil, WithMusicDir(dir))

	rec := httptest.NewRecorder()
	h.ListSongs(rec, httptest.NewRequest(http.MethodGet, "/api/songs", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	songs := decodeSongs(t, rec.Body.Bytes())
	if len(songs) != len(catalog.Tracks) {
		t.Fatalf("songs = %d, want all %d shipped tracks", len(songs), len(catalog.Tracks))
	}

	got := make(map[string]bool, len(songs))
	for _, song := range songs {
		got[song.ID] = true
	}
	viralIDs := []string{
		"pop-hook",
		"club-jump-beat",
		"dark-electroshuffle",
		"percussive-party",
		"hard-rap-loop",
		"acid-beat",
		"urban-funk",
		"retro-fireworks",
	}
	for _, id := range viralIDs {
		if !got[id] {
			t.Errorf("shipped catalog API is missing viral track %q", id)
		}
	}
}

func TestListSongsScanFallbackWithoutCatalog(t *testing.T) {
	dir := writeMusicDir(t, "", map[string][]byte{
		"trap-1.mp3": []byte("audio"),
		"epic-1.wav": []byte("audio"),
		"notes.txt":  []byte("ignore me"),
	})
	h := NewHandlers(nil, nil, nil, WithMusicDir(dir))

	rec := httptest.NewRecorder()
	h.ListSongs(rec, httptest.NewRequest(http.MethodGet, "/api/songs", nil))
	songs := decodeSongs(t, rec.Body.Bytes())
	if len(songs) != 2 {
		t.Fatalf("got %d songs, want 2 (txt ignored): %+v", len(songs), songs)
	}
	// Sorted by id: epic-1 before trap-1.
	if songs[0].ID != "epic-1" || songs[1].ID != "trap-1" {
		t.Fatalf("ids = %q,%q, want epic-1,trap-1", songs[0].ID, songs[1].ID)
	}
	if songs[0].Title != "Epic 1" {
		t.Fatalf("fallback title = %q, want humanized 'Epic 1'", songs[0].Title)
	}
}

func TestListSongsEmptyWithoutMusicDir(t *testing.T) {
	h := NewHandlers(nil, nil, nil)
	rec := httptest.NewRecorder()
	h.ListSongs(rec, httptest.NewRequest(http.MethodGet, "/api/songs", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if songs := decodeSongs(t, rec.Body.Bytes()); len(songs) != 0 {
		t.Fatalf("got %d songs, want 0", len(songs))
	}
}

func TestGetSongAudioStreamsAndValidates(t *testing.T) {
	dir := writeMusicDir(t, "", map[string][]byte{"edm-1.mp3": []byte("ID3audiobytes")})
	h := NewHandlers(nil, nil, nil, WithMusicDir(dir))

	t.Run("ok", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/songs/edm-1/audio", nil)
		req = withURLParam(req, "id", "edm-1")
		h.GetSongAudio(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "audio/mpeg" {
			t.Fatalf("content-type = %q, want audio/mpeg", ct)
		}
		if rec.Body.String() != "ID3audiobytes" {
			t.Fatalf("body = %q, want the file bytes", rec.Body.String())
		}
	})

	t.Run("missing", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := withURLParam(httptest.NewRequest(http.MethodGet, "/api/songs/nope/audio", nil), "id", "nope")
		h.GetSongAudio(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rec.Code)
		}
	})

	t.Run("traversal rejected", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := withURLParam(httptest.NewRequest(http.MethodGet, "/api/songs/x/audio", nil), "id", "../secret")
		h.GetSongAudio(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400 for unsafe id", rec.Code)
		}
	})
}
