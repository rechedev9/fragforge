package voiceprofile

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/rechedev9/fragforge/internal/storage"
)

func TestStoreSaveGetOpenAndDelete(t *testing.T) {
	local, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal error = %v", err)
	}
	store := New(local)
	wantAudio := []byte("OggS-local-voice-reference")

	saved, err := store.Save(Profile{
		ID:             "raizerinhocs2",
		Name:           "Mi voz",
		Channel:        "RaizerinhoCS2",
		Locale:         "es-ES",
		SourceFileName: "reference.ogg",
		ContentType:    "audio/ogg",
	}, bytes.NewReader(wantAudio))
	if err != nil {
		t.Fatalf("Save error = %v", err)
	}
	if saved.SizeBytes != int64(len(wantAudio)) || saved.SHA256 == "" {
		t.Fatalf("saved profile = %#v", saved)
	}
	if !validAudioKey("raizerinhocs2", saved.AudioKey) || saved.AudioKey == "voice-profiles/raizerinhocs2/reference.audio" {
		t.Fatalf("AudioKey = %q", saved.AudioKey)
	}

	got, err := store.Get("raizerinhocs2")
	if err != nil {
		t.Fatalf("Get error = %v", err)
	}
	if got.Channel != "RaizerinhoCS2" || got.CreatedAt.IsZero() || got.UpdatedAt.IsZero() {
		t.Fatalf("got profile = %#v", got)
	}

	rc, _, err := store.OpenAudio("raizerinhocs2")
	if err != nil {
		t.Fatalf("OpenAudio error = %v", err)
	}
	gotAudio, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll error = %v", err)
	}
	_ = rc.Close()
	if !bytes.Equal(gotAudio, wantAudio) {
		t.Fatalf("audio = %q, want %q", gotAudio, wantAudio)
	}

	if err := store.Delete("raizerinhocs2"); err != nil {
		t.Fatalf("Delete error = %v", err)
	}
	if _, err := store.Get("raizerinhocs2"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after Delete error = %v, want ErrNotFound", err)
	}
}

func TestValidID(t *testing.T) {
	for _, id := range []string{"", "RaizerinhoCS2", "../voice", "voice_profile", "áudio"} {
		if ValidID(id) {
			t.Errorf("ValidID(%q) = true", id)
		}
	}
	for _, id := range []string{"raizerinhocs2", "voice-2"} {
		if !ValidID(id) {
			t.Errorf("ValidID(%q) = false", id)
		}
	}
}

func TestStoreRejectsEmptyAudio(t *testing.T) {
	local, _ := storage.NewLocal(t.TempDir())
	store := New(local)
	_, err := store.Save(Profile{ID: "voice"}, bytes.NewReader(nil))
	if err == nil {
		t.Fatal("Save error = nil, want empty-audio error")
	}
	if _, err := store.Get("voice"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get error = %v, want ErrNotFound", err)
	}
}

func TestSaveLimitedPreservesExistingProfile(t *testing.T) {
	local, _ := storage.NewLocal(t.TempDir())
	store := New(local)
	before, err := store.Save(Profile{ID: "voice", ContentType: "audio/ogg"}, bytes.NewReader([]byte("OggS-original")))
	if err != nil {
		t.Fatalf("initial Save error = %v", err)
	}

	_, err = store.SaveLimited(Profile{ID: "voice", ContentType: "audio/wav"}, bytes.NewReader(bytes.Repeat([]byte{0x42}, 20)), 10)
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("SaveLimited error = %v, want ErrTooLarge", err)
	}
	after, err := store.Get("voice")
	if err != nil {
		t.Fatalf("Get error = %v", err)
	}
	if after.AudioKey != before.AudioKey || after.SHA256 != before.SHA256 {
		t.Fatalf("profile changed after rejected replacement: before=%#v after=%#v", before, after)
	}
	rc, _, err := store.OpenAudio("voice")
	if err != nil {
		t.Fatalf("OpenAudio error = %v", err)
	}
	got, _ := io.ReadAll(rc)
	_ = rc.Close()
	if string(got) != "OggS-original" {
		t.Fatalf("audio = %q", got)
	}
}

func TestDeleteWaitsForProfileSave(t *testing.T) {
	local, _ := storage.NewLocal(t.TempDir())
	blocked := &metadataBlockingStorage{
		Local:   local,
		entered: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
	store := New(blocked)
	saveDone := make(chan error, 1)
	go func() {
		_, err := store.Save(Profile{ID: "voice", ContentType: "audio/ogg"}, bytes.NewReader([]byte("OggS-reference")))
		saveDone <- err
	}()
	<-blocked.entered

	deleteDone := make(chan error, 1)
	go func() { deleteDone <- store.Delete("voice") }()
	select {
	case err := <-deleteDone:
		t.Fatalf("Delete completed during Save: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(blocked.release)
	if err := <-saveDone; err != nil {
		t.Fatalf("Save error = %v", err)
	}
	if err := <-deleteDone; err != nil {
		t.Fatalf("Delete error = %v", err)
	}
	if _, err := store.Get("voice"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get error = %v, want ErrNotFound", err)
	}
}

type metadataBlockingStorage struct {
	*storage.Local
	entered chan struct{}
	release chan struct{}
}

func (s *metadataBlockingStorage) Put(key string, reader io.Reader) error {
	if strings.HasSuffix(key, "/"+metadataName) {
		s.entered <- struct{}{}
		<-s.release
	}
	return s.Local.Put(key, reader)
}
