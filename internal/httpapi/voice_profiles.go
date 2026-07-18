package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/rechedev9/fragforge/internal/voiceprofile"
)

const (
	maxVoiceReferenceBytes = 25 << 20
	maxVoiceMultipartBytes = maxVoiceReferenceBytes + 1<<20
	voiceMultipartMemory   = 4 << 20
)

type voiceProfileResponse struct {
	voiceprofile.Profile
	AudioURL string `json:"audio_url"`
}

// PutVoiceProfile handles PUT /api/voice-profiles/{id}. The uploaded reference
// stays in FragForge's local data store and is never sent to a TTS provider.
func (h *Handlers) PutVoiceProfile(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !voiceprofile.ValidID(id) {
		writeError(w, http.StatusBadRequest, "invalid voice profile id")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxVoiceMultipartBytes)
	parseErr := r.ParseMultipartForm(voiceMultipartMemory)
	if r.MultipartForm != nil {
		defer func() { _ = r.MultipartForm.RemoveAll() }()
	}
	if parseErr != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(parseErr, &maxBytesErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "voice reference is too large")
			return
		}
		writeError(w, http.StatusBadRequest, "parsing voice profile upload: "+parseErr.Error())
		return
	}

	file, header, err := r.FormFile("voice")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing voice reference: "+err.Error())
		return
	}
	defer file.Close()

	contentType, err := voiceprofile.ValidateAudio(file)
	if errors.Is(err, voiceprofile.ErrTooLarge) {
		writeError(w, http.StatusRequestEntityTooLarge, "voice reference is too large")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	fileName := "voice-reference"
	if header != nil {
		fileName = sanitizeDemoFileName(header.Filename)
	}
	profile, err := h.voiceProfiles.SaveLimited(voiceprofile.Profile{
		ID:             id,
		Name:           defaultText(voiceprofile.NormalizeText(r.FormValue("name"), 80), "Mi voz"),
		Channel:        defaultText(voiceprofile.NormalizeText(r.FormValue("channel"), 80), "RaizerinhoCS2"),
		Locale:         defaultText(voiceprofile.NormalizeText(r.FormValue("locale"), 20), "es-ES"),
		SourceFileName: fileName,
		ContentType:    contentType,
	}, file, maxVoiceReferenceBytes)
	if errors.Is(err, voiceprofile.ErrTooLarge) {
		writeError(w, http.StatusRequestEntityTooLarge, "voice reference is too large")
		return
	}
	if err != nil {
		internalError(w, "save voice profile", err)
		return
	}
	writeJSON(w, http.StatusOK, newVoiceProfileResponse(profile))
}

// GetVoiceProfile returns metadata without exposing an absolute filesystem
// path. The audio URL remains authenticated by the same local API boundary.
func (h *Handlers) GetVoiceProfile(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	profile, err := h.voiceProfiles.Get(id)
	if errors.Is(err, voiceprofile.ErrNotFound) {
		writeError(w, http.StatusNotFound, "voice profile not found")
		return
	}
	if err != nil {
		if !voiceprofile.ValidID(id) {
			writeError(w, http.StatusBadRequest, "invalid voice profile id")
			return
		}
		internalError(w, "load voice profile", err)
		return
	}
	writeJSON(w, http.StatusOK, newVoiceProfileResponse(profile))
}

func (h *Handlers) GetVoiceProfileAudio(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	rc, profile, err := h.voiceProfiles.OpenAudio(id)
	if errors.Is(err, voiceprofile.ErrNotFound) {
		writeError(w, http.StatusNotFound, "voice profile not found")
		return
	}
	if err != nil {
		if !voiceprofile.ValidID(id) {
			writeError(w, http.StatusBadRequest, "invalid voice profile id")
			return
		}
		internalError(w, "open voice profile audio", err)
		return
	}
	serveArtifact(w, r, profile.ContentType, rc)
}

func (h *Handlers) DeleteVoiceProfile(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !voiceprofile.ValidID(id) {
		writeError(w, http.StatusBadRequest, "invalid voice profile id")
		return
	}
	if err := h.voiceProfiles.Delete(id); err != nil {
		if strings.Contains(err.Error(), "does not support delete") {
			writeError(w, http.StatusNotImplemented, "storage backend does not support voice profile deletion")
			return
		}
		internalError(w, "delete voice profile", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func newVoiceProfileResponse(profile voiceprofile.Profile) voiceProfileResponse {
	return voiceProfileResponse{
		Profile:  profile,
		AudioURL: fmt.Sprintf("/api/voice-profiles/%s/audio", profile.ID),
	}
}

func defaultText(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
