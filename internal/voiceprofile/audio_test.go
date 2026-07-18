package voiceprofile

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
)

func TestValidateAudioOGG(t *testing.T) {
	body := validTestOgg(0x04)
	got, err := ValidateAudio(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("ValidateAudio error = %v", err)
	}
	if got != "audio/ogg" {
		t.Fatalf("content type = %q", got)
	}
}

func TestValidateAudioOGGWithoutEOSFlag(t *testing.T) {
	body := validTestOgg(0x00)
	if _, err := ValidateAudio(bytes.NewReader(body)); err != nil {
		t.Fatalf("ValidateAudio error = %v", err)
	}
}

func TestValidateAudioRejectsFabricatedOgg(t *testing.T) {
	body := validTestOgg(0x04)
	body[len(body)-1] ^= 0xff
	if _, err := ValidateAudio(bytes.NewReader(body)); err == nil {
		t.Fatal("ValidateAudio error = nil")
	}
}

func TestValidateAudioRejectsMalformedLaterOpusPacket(t *testing.T) {
	body := validTestOgg(0x00)
	body = append(body, oggTestPage(0x04, 3, nil)...)
	if _, err := ValidateAudio(bytes.NewReader(body)); err == nil {
		t.Fatal("ValidateAudio error = nil")
	}
}

func TestValidateAudioRejectsExcessiveOpusPacketDuration(t *testing.T) {
	body := validTestOgg(0x00)
	body = append(body, oggTestPage(0x04, 3, []byte{0xfb, 0x30})...)
	if _, err := ValidateAudio(bytes.NewReader(body)); err == nil {
		t.Fatal("ValidateAudio error = nil")
	}
}

func TestValidateAudioWAV(t *testing.T) {
	data := bytes.Repeat([]byte{0x01, 0x00}, 64)
	body := make([]byte, 44+len(data))
	copy(body[:4], "RIFF")
	binary.LittleEndian.PutUint32(body[4:8], uint32(len(body)-8))
	copy(body[8:12], "WAVE")
	copy(body[12:16], "fmt ")
	binary.LittleEndian.PutUint32(body[16:20], 16)
	binary.LittleEndian.PutUint16(body[20:22], 1)
	binary.LittleEndian.PutUint16(body[22:24], 1)
	binary.LittleEndian.PutUint32(body[24:28], 24000)
	binary.LittleEndian.PutUint32(body[28:32], 48000)
	binary.LittleEndian.PutUint16(body[32:34], 2)
	binary.LittleEndian.PutUint16(body[34:36], 16)
	copy(body[36:40], "data")
	binary.LittleEndian.PutUint32(body[40:44], uint32(len(data)))
	copy(body[44:], data)

	got, err := ValidateAudio(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("ValidateAudio error = %v", err)
	}
	if got != "audio/wav" {
		t.Fatalf("content type = %q", got)
	}
}

func TestValidateAudioRejectsInconsistentWAVFormat(t *testing.T) {
	body := make([]byte, 45)
	copy(body[:4], "RIFF")
	binary.LittleEndian.PutUint32(body[4:8], uint32(len(body)-8))
	copy(body[8:12], "WAVE")
	copy(body[12:16], "fmt ")
	binary.LittleEndian.PutUint32(body[16:20], 16)
	binary.LittleEndian.PutUint16(body[20:22], 1)
	binary.LittleEndian.PutUint16(body[22:24], 2)
	binary.LittleEndian.PutUint32(body[24:28], 24000)
	binary.LittleEndian.PutUint32(body[28:32], 24000)
	binary.LittleEndian.PutUint16(body[32:34], 1)
	copy(body[36:40], "data")
	binary.LittleEndian.PutUint32(body[40:44], 1)
	body[44] = 1
	if _, err := ValidateAudio(bytes.NewReader(body)); err == nil {
		t.Fatal("ValidateAudio error = nil")
	}
}

func TestValidateAudioRejectsTruncatedAndNonAudioContainers(t *testing.T) {
	for _, body := range [][]byte{
		{0xff, 0xe0},
		[]byte("OggS"),
		append([]byte{0, 0, 0, 16, 'f', 't', 'y', 'p'}, []byte("isom0000")...),
	} {
		_, err := ValidateAudio(bytes.NewReader(body))
		if err == nil {
			t.Fatalf("ValidateAudio(%x) error = nil", body)
		}
		if !strings.Contains(err.Error(), "voice profile") {
			t.Fatalf("error = %v", err)
		}
	}
}

func oggTestPage(headerType byte, sequence uint32, payload []byte) []byte {
	header := make([]byte, 28)
	copy(header[:4], "OggS")
	header[5] = headerType
	binary.LittleEndian.PutUint32(header[14:18], 1)
	binary.LittleEndian.PutUint32(header[18:22], sequence)
	header[26] = 1
	header[27] = byte(len(payload))
	page := append(header, payload...)
	binary.LittleEndian.PutUint32(page[22:26], oggCRC(page))
	return page
}

func validTestOgg(finalHeaderType byte) []byte {
	head := make([]byte, 19)
	copy(head, "OpusHead")
	head[8] = 1
	head[9] = 1
	binary.LittleEndian.PutUint16(head[10:12], 312)
	binary.LittleEndian.PutUint32(head[12:16], 48000)
	tags := make([]byte, 16)
	copy(tags, "OpusTags")
	body := append(oggTestPage(0x02, 0, head), oggTestPage(0x00, 1, tags)...)
	return append(body, oggTestPage(finalHeaderType, 2, []byte{0xf8, 0xff, 0xfe})...)
}
