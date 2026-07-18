// Package voiceprofile stores reusable local voice references for narration.
package voiceprofile

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/rechedev9/fragforge/internal/storage"
)

const (
	metadataName = "profile.json"
	audioName    = "reference.audio"
)

var ErrNotFound = errors.New("voice profile not found")
var ErrTooLarge = errors.New("voice reference is too large")

// Profile describes one locally stored voice reference. AudioKey is a logical
// storage key, never an external URL or a path outside FragForge's data root.
type Profile struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Channel        string    `json:"channel"`
	Locale         string    `json:"locale"`
	SourceFileName string    `json:"source_file_name"`
	ContentType    string    `json:"content_type"`
	AudioKey       string    `json:"audio_key"`
	SizeBytes      int64     `json:"size_bytes"`
	SHA256         string    `json:"sha256"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// Store persists profiles inside the existing local artifact store.
type Store struct {
	storage   storage.Storage
	now       func() time.Time
	mutations sync.Mutex
}

func New(store storage.Storage) *Store {
	return &Store{storage: store, now: time.Now}
}

// ValidID accepts stable URL-safe profile identifiers.
func ValidID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			continue
		}
		return false
	}
	return true
}

// Save atomically replaces the reference audio, then publishes metadata. A
// reader never observes metadata pointing at a partial audio file because the
// underlying local storage Put is atomic.
func (s *Store) Save(profile Profile, audio io.Reader) (Profile, error) {
	return s.SaveLimited(profile, audio, 0)
}

// SaveLimited stages a new audio object and publishes metadata only after the
// complete reference has passed the optional size limit. The previous profile
// remains readable if staging, validation, or metadata publication fails.
func (s *Store) SaveLimited(profile Profile, audio io.Reader, maxBytes int64) (Profile, error) {
	if s == nil || s.storage == nil {
		return Profile{}, errors.New("voice profile: storage is not configured")
	}
	if _, ok := s.storage.(interface{ Delete(string) error }); !ok {
		return Profile{}, errors.New("voice profile: storage does not support audio cleanup")
	}
	s.mutations.Lock()
	defer s.mutations.Unlock()

	if !ValidID(profile.ID) {
		return Profile{}, fmt.Errorf("voice profile: invalid id %q", profile.ID)
	}

	now := s.now().UTC()
	createdAt := now
	var existing Profile
	if loaded, err := s.Get(profile.ID); err == nil {
		existing = loaded
		createdAt = existing.CreatedAt
	} else if !errors.Is(err, ErrNotFound) {
		return Profile{}, err
	}

	stagedKey, err := newAudioKey(profile.ID)
	if err != nil {
		return Profile{}, err
	}
	hash := sha256.New()
	counter := &byteCounter{}
	profile.AudioKey = stagedKey
	var source io.Reader = audio
	if maxBytes > 0 {
		source = io.LimitReader(audio, maxBytes+1)
	}
	if err := s.storage.Put(profile.AudioKey, io.TeeReader(source, io.MultiWriter(hash, counter))); err != nil {
		return Profile{}, fmt.Errorf("voice profile: store audio: %w", err)
	}
	if counter.n == 0 {
		baseErr := errors.New("voice profile: reference audio is empty")
		return Profile{}, joinCleanupError(baseErr, s.deleteAudio(profile.AudioKey))
	}
	if maxBytes > 0 && counter.n > maxBytes {
		return Profile{}, joinCleanupError(ErrTooLarge, s.deleteAudio(profile.AudioKey))
	}

	profile.SizeBytes = counter.n
	profile.SHA256 = hex.EncodeToString(hash.Sum(nil))
	profile.CreatedAt = createdAt
	profile.UpdatedAt = now
	encoded, err := json.MarshalIndent(profile, "", "  ")
	if err != nil {
		return Profile{}, fmt.Errorf("voice profile: encode metadata: %w", err)
	}
	if err := s.storage.Put(metadataKey(profile.ID), bytes.NewReader(encoded)); err != nil {
		baseErr := fmt.Errorf("voice profile: store metadata: %w", err)
		return Profile{}, joinCleanupError(baseErr, s.deleteAudio(profile.AudioKey))
	}
	if existing.AudioKey != "" && existing.AudioKey != profile.AudioKey {
		if err := s.deleteAudio(existing.AudioKey); err != nil {
			return Profile{}, fmt.Errorf("voice profile: remove replaced audio: %w", err)
		}
	}
	return profile, nil
}

func (s *Store) Get(id string) (Profile, error) {
	if s == nil || s.storage == nil {
		return Profile{}, errors.New("voice profile: storage is not configured")
	}
	if !ValidID(id) {
		return Profile{}, fmt.Errorf("voice profile: invalid id %q", id)
	}
	rc, err := s.storage.Open(metadataKey(id))
	if storage.IsNotExist(err) {
		return Profile{}, ErrNotFound
	}
	if err != nil {
		return Profile{}, fmt.Errorf("voice profile: open metadata: %w", err)
	}
	defer rc.Close()

	var profile Profile
	if err := json.NewDecoder(io.LimitReader(rc, 64<<10)).Decode(&profile); err != nil {
		return Profile{}, fmt.Errorf("voice profile: decode metadata: %w", err)
	}
	if profile.ID != id || !validAudioKey(id, profile.AudioKey) {
		return Profile{}, errors.New("voice profile: metadata does not match requested profile")
	}
	return profile, nil
}

func (s *Store) OpenAudio(id string) (io.ReadCloser, Profile, error) {
	profile, err := s.Get(id)
	if err != nil {
		return nil, Profile{}, err
	}
	rc, err := s.storage.Open(profile.AudioKey)
	if storage.IsNotExist(err) {
		// A concurrent replacement may publish new metadata and remove the old
		// object between Get and Open. Retry once when the version changed.
		reloaded, reloadErr := s.Get(id)
		if reloadErr == nil && reloaded.AudioKey != profile.AudioKey {
			rc, err = s.storage.Open(reloaded.AudioKey)
			if err == nil {
				return rc, reloaded, nil
			}
			if !storage.IsNotExist(err) {
				return nil, Profile{}, fmt.Errorf("voice profile: open replacement audio: %w", err)
			}
		}
		return nil, Profile{}, ErrNotFound
	}
	if err != nil {
		return nil, Profile{}, fmt.Errorf("voice profile: open audio: %w", err)
	}
	return rc, profile, nil
}

func (s *Store) Delete(id string) error {
	if s == nil || s.storage == nil {
		return errors.New("voice profile: storage is not configured")
	}
	s.mutations.Lock()
	defer s.mutations.Unlock()

	if !ValidID(id) {
		return fmt.Errorf("voice profile: invalid id %q", id)
	}
	deleter, ok := s.storage.(interface{ DeleteTree(string) error })
	if !ok {
		return errors.New("voice profile: storage does not support delete")
	}
	if err := deleter.DeleteTree(profilePrefix(id)); err != nil {
		return fmt.Errorf("voice profile: delete: %w", err)
	}
	return nil
}

func (s *Store) deleteAudio(key string) error {
	deleter, ok := s.storage.(interface{ Delete(string) error })
	if !ok {
		return errors.New("storage does not support delete")
	}
	return deleter.Delete(key)
}

func joinCleanupError(baseErr, cleanupErr error) error {
	if cleanupErr == nil {
		return baseErr
	}
	return errors.Join(baseErr, fmt.Errorf("voice profile: clean staged audio: %w", cleanupErr))
}

func profilePrefix(id string) string {
	return "voice-profiles/" + id
}

func metadataKey(id string) string {
	return profilePrefix(id) + "/" + metadataName
}

func newAudioKey(id string) (string, error) {
	var token [12]byte
	if _, err := rand.Read(token[:]); err != nil {
		return "", fmt.Errorf("voice profile: create audio version: %w", err)
	}
	return profilePrefix(id) + "/audio/" + hex.EncodeToString(token[:]) + ".audio", nil
}

func validAudioKey(id, key string) bool {
	// Accept the original fixed key so profiles created before versioned audio
	// remain readable and are migrated on their next successful replacement.
	if key == profilePrefix(id)+"/"+audioName {
		return true
	}
	prefix := profilePrefix(id) + "/audio/"
	if !strings.HasPrefix(key, prefix) || !strings.HasSuffix(key, ".audio") {
		return false
	}
	version := strings.TrimSuffix(strings.TrimPrefix(key, prefix), ".audio")
	if len(version) != 24 {
		return false
	}
	for _, r := range version {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') {
			continue
		}
		return false
	}
	return true
}

type byteCounter struct{ n int64 }

func (c *byteCounter) Write(p []byte) (int, error) {
	c.n += int64(len(p))
	return len(p), nil
}

// NormalizeText trims a user-facing metadata field and caps it by runes.
func NormalizeText(value string, maxRunes int) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) > maxRunes {
		value = strings.TrimSpace(string(runes[:maxRunes]))
	}
	return value
}
