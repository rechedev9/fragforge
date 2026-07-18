package streamclips

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type CaptionCandidateStatus string

const (
	CaptionCandidatesNone           CaptionCandidateStatus = "none"
	CaptionCandidatesQueued         CaptionCandidateStatus = "queued"
	CaptionCandidatesGenerating     CaptionCandidateStatus = "generating"
	CaptionCandidatesReviewRequired CaptionCandidateStatus = "review_required"
	CaptionCandidatesReady          CaptionCandidateStatus = "ready"
	CaptionCandidatesFailed         CaptionCandidateStatus = "failed"
)

type CaptionCandidateClipStatus string

const (
	CaptionClipReviewRequired CaptionCandidateClipStatus = "review_required"
	CaptionClipNoSpeech       CaptionCandidateClipStatus = "no_speech"
	CaptionClipReady          CaptionCandidateClipStatus = "ready"
	CaptionClipFailed         CaptionCandidateClipStatus = "failed"
)

// CaptionCandidateState is the durable, reviewable output of cloud speech
// recognition. CandidateWords are never rendered until a review request copies
// them into the edit plan and marks the clip CaptionReviewed.
type CaptionCandidateState struct {
	JobID        uuid.UUID              `json:"job_id"`
	GenerationID uuid.UUID              `json:"generation_id"`
	Status       CaptionCandidateStatus `json:"status"`
	Clips        []CaptionCandidateClip `json:"clips"`
	Warnings     []string               `json:"warnings,omitempty"`
	Error        string                 `json:"error,omitempty"`
	UpdatedAt    time.Time              `json:"updated_at"`
}

type CaptionCandidateClip struct {
	ClipID           string                     `json:"clip_id"`
	StartSeconds     float64                    `json:"start_seconds"`
	EndSeconds       float64                    `json:"end_seconds"`
	Fingerprint      string                     `json:"fingerprint"`
	Status           CaptionCandidateClipStatus `json:"status"`
	SourceWords      []CaptionWord              `json:"source_words,omitempty"`
	CandidateWords   []CaptionWord              `json:"candidate_words,omitempty"`
	Provider         string                     `json:"provider,omitempty"`
	STTModel         string                     `json:"stt_model,omitempty"`
	STTEndpoint      string                     `json:"stt_endpoint,omitempty"`
	TranslationModel string                     `json:"translation_model,omitempty"`
	Error            string                     `json:"error,omitempty"`
}

// CaptionClipFingerprint binds candidates to the immutable source and the
// audio-affecting clip settings used to produce them.
func CaptionClipFingerprint(sourceSHA256 string, clip ClipRange) (string, error) {
	payload := struct {
		SourceSHA256 string   `json:"source_sha256"`
		ClipID       string   `json:"clip_id"`
		StartSeconds float64  `json:"start_seconds"`
		EndSeconds   float64  `json:"end_seconds"`
		Speed        float64  `json:"speed"`
		SourceVolume *float64 `json:"source_volume,omitempty"`
	}{
		SourceSHA256: sourceSHA256,
		ClipID:       clip.ID,
		StartSeconds: clip.StartSeconds,
		EndSeconds:   clip.EndSeconds,
		Speed:        clip.EffectiveSpeed(),
	}
	if clip.Edit != nil && clip.Edit.SourceVolume != nil {
		volume := *clip.Edit.SourceVolume
		payload.SourceVolume = &volume
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode caption clip fingerprint: %w", err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}
