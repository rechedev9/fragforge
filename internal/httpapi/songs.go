package httpapi

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"
)

// songIDPattern matches a safe music track id; it doubles as path-traversal
// defence since a valid id can never contain a separator or "..".
var songIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// songAudioExts are the accepted track extensions, in resolution priority order
// (matches resolveMusicFile in the render worker).
var songAudioExts = []string{".m4a", ".mp3", ".ogg", ".opus", ".wav", ".aac"}

// WithMusicDir points the songs API at the directory holding music tracks named
// "<id>.<ext>" plus an optional catalog.json with metadata. Empty leaves the
// songs API serving an empty catalog.
func WithMusicDir(dir string) Option {
	return func(h *Handlers) {
		h.musicDir = dir
	}
}

// song is the UI-facing music track served by GET /api/songs.
type song struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Artist      string `json:"artist,omitempty"`
	Genre       string `json:"genre,omitempty"`
	DurationSec int    `json:"durationSec,omitempty"`
	License     string `json:"license,omitempty"`
	// AudioURL is the same-origin stream the picker plays for preview.
	AudioURL string `json:"audioUrl"`
}

// catalogTrack mirrors one entry in <musicDir>/catalog.json. Extra fields in the
// file (sourceUrl, downloadUrl, attributionRequired) are ignored here.
type catalogTrack struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Artist      string `json:"artist"`
	Genre       string `json:"genre"`
	DurationSec int    `json:"durationSec"`
	License     string `json:"license"`
}

// loadMusicCatalog reads <musicDir>/catalog.json. The bool reports whether a
// usable catalog was found, so callers can fall back to a bare directory scan.
func (h *Handlers) loadMusicCatalog() ([]catalogTrack, bool) {
	if h.musicDir == "" {
		return nil, false
	}
	raw, err := os.ReadFile(filepath.Join(h.musicDir, "catalog.json"))
	if err != nil {
		return nil, false
	}
	var doc struct {
		Tracks []catalogTrack `json:"tracks"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		// The file exists but is malformed; warn (not silent) and fall back to a
		// bare directory scan so a broken catalog doesn't hide dropped-in tracks.
		log.Printf("songs: ignoring malformed %s: %v", filepath.Join(h.musicDir, "catalog.json"), err)
		return nil, false
	}
	if len(doc.Tracks) == 0 {
		return nil, false
	}
	return doc.Tracks, true
}

// resolveSongFile returns the on-disk path of the first audio file for id, or ""
// when none exists. id must already match songIDPattern.
func (h *Handlers) resolveSongFile(id string) string {
	if h.musicDir == "" || !songIDPattern.MatchString(id) {
		return ""
	}
	for _, ext := range songAudioExts {
		p := filepath.Join(h.musicDir, id+ext)
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p
		}
	}
	return ""
}

// ListSongs handles GET /api/songs. With a catalog.json it returns the curated
// tracks (in catalog order) whose audio file is present; otherwise it falls back
// to scanning the music directory so dropped-in files still appear.
func (h *Handlers) ListSongs(w http.ResponseWriter, r *http.Request) {
	songs := []song{}
	if catalog, ok := h.loadMusicCatalog(); ok {
		for _, t := range catalog {
			if !songIDPattern.MatchString(t.ID) || h.resolveSongFile(t.ID) == "" {
				continue
			}
			title := t.Title
			if title == "" {
				title = humanizeSongID(t.ID)
			}
			songs = append(songs, song{
				ID:          t.ID,
				Title:       title,
				Artist:      t.Artist,
				Genre:       t.Genre,
				DurationSec: t.DurationSec,
				License:     t.License,
				AudioURL:    songAudioURL(t.ID),
			})
		}
	} else {
		songs = h.scanSongs()
	}
	writeJSON(w, http.StatusOK, map[string]any{"songs": songs})
}

// scanSongs lists every audio file in the music directory as a bare song.
func (h *Handlers) scanSongs() []song {
	songs := []song{}
	if h.musicDir == "" {
		return songs
	}
	entries, err := os.ReadDir(h.musicDir)
	if err != nil {
		return songs
	}
	seen := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if !slices.Contains(songAudioExts, ext) {
			continue
		}
		id := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
		if !songIDPattern.MatchString(id) || seen[id] {
			continue
		}
		seen[id] = true
		songs = append(songs, song{ID: id, Title: humanizeSongID(id), AudioURL: songAudioURL(id)})
	}
	sort.Slice(songs, func(i, j int) bool { return songs[i].ID < songs[j].ID })
	return songs
}

// GetSongAudio handles GET /api/songs/{id}/audio, streaming the track for
// in-browser preview. http.ServeContent honours Range requests so the picker can
// scrub without downloading the whole file.
func (h *Handlers) GetSongAudio(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !songIDPattern.MatchString(id) {
		writeError(w, http.StatusBadRequest, "invalid song id")
		return
	}
	path := h.resolveSongFile(id)
	if path == "" {
		writeError(w, http.StatusNotFound, "song not found")
		return
	}
	f, err := os.Open(path)
	if err != nil {
		writeError(w, http.StatusNotFound, "song not found")
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		internalError(w, "stat song", err)
		return
	}
	w.Header().Set("Content-Type", songContentType(filepath.Ext(path)))
	w.Header().Set("Cache-Control", "public, max-age=3600")
	http.ServeContent(w, r, filepath.Base(path), info.ModTime(), f)
}

func songAudioURL(id string) string {
	return "/api/songs/" + id + "/audio"
}

// humanizeSongID turns "synthwave-1" into "Synthwave 1" for a readable fallback
// title when the catalog has no entry for a track.
func humanizeSongID(id string) string {
	parts := strings.Split(id, "-")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, " ")
}

func songContentType(ext string) string {
	switch strings.ToLower(ext) {
	case ".mp3":
		return "audio/mpeg"
	case ".m4a", ".aac":
		return "audio/mp4"
	case ".ogg", ".opus":
		return "audio/ogg"
	case ".wav":
		return "audio/wav"
	default:
		return "application/octet-stream"
	}
}
