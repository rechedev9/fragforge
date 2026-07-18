package voiceprofile

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const maxValidatedAudioBytes = 25 << 20

// ValidateAudio verifies the container and codec structure of a supported
// reference, not merely its extension or leading magic bytes. It deliberately
// accepts Opus streams from recorders that omit the optional OGG EOS flag.
func ValidateAudio(source io.ReadSeeker) (string, error) {
	if source == nil {
		return "", errors.New("voice profile: missing audio reader")
	}
	size, err := source.Seek(0, io.SeekEnd)
	if err != nil {
		return "", fmt.Errorf("voice profile: measure audio: %w", err)
	}
	if size <= 0 {
		return "", errors.New("voice profile: reference audio is empty")
	}
	if size > maxValidatedAudioBytes {
		return "", ErrTooLarge
	}
	if _, err := source.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("voice profile: rewind audio: %w", err)
	}
	body, err := io.ReadAll(io.LimitReader(source, maxValidatedAudioBytes+1))
	if err != nil {
		return "", fmt.Errorf("voice profile: read audio: %w", err)
	}
	if _, err := source.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("voice profile: rewind audio: %w", err)
	}

	switch {
	case bytes.HasPrefix(body, []byte("OggS")):
		if err := validateOgg(body); err != nil {
			return "", err
		}
		return "audio/ogg", nil
	case len(body) >= 12 && bytes.Equal(body[:4], []byte("RIFF")) && bytes.Equal(body[8:12], []byte("WAVE")):
		if err := validateWAV(body); err != nil {
			return "", err
		}
		return "audio/wav", nil
	default:
		return "", errors.New("voice profile: reference must be valid OGG Opus or classic PCM/IEEE-float WAV audio")
	}
}

type oggStream struct {
	nextSequence     uint32
	packet           []byte
	opusState        int
	opusAudioPackets int
}

func validateOgg(body []byte) error {
	offset := 0
	pages := 0
	streams := make(map[uint32]*oggStream)
	for offset < len(body) {
		if len(body)-offset < 27 || !bytes.Equal(body[offset:offset+4], []byte("OggS")) || body[offset+4] != 0 {
			return errors.New("voice profile: invalid or truncated OGG page")
		}
		segments := int(body[offset+26])
		if len(body)-offset < 27+segments {
			return errors.New("voice profile: truncated OGG segment table")
		}
		payloadBytes := 0
		for _, length := range body[offset+27 : offset+27+segments] {
			payloadBytes += int(length)
		}
		payloadStart := offset + 27 + segments
		pageEnd := payloadStart + payloadBytes
		if pageEnd > len(body) {
			return errors.New("voice profile: truncated OGG payload")
		}
		page := body[offset:pageEnd]
		expectedCRC := binary.LittleEndian.Uint32(page[22:26])
		if expectedCRC == 0 || oggCRC(page) != expectedCRC {
			return errors.New("voice profile: invalid OGG page checksum")
		}

		headerType := body[offset+5]
		serial := binary.LittleEndian.Uint32(body[offset+14 : offset+18])
		sequence := binary.LittleEndian.Uint32(body[offset+18 : offset+22])
		stream := streams[serial]
		if stream == nil {
			if headerType&0x02 == 0 || sequence != 0 {
				return errors.New("voice profile: OGG logical stream has no beginning")
			}
			stream = &oggStream{}
			streams[serial] = stream
		}
		if sequence != stream.nextSequence {
			return errors.New("voice profile: OGG page sequence is incomplete")
		}
		continuing := len(stream.packet) > 0
		if (headerType&0x01 != 0) != continuing {
			return errors.New("voice profile: invalid OGG packet continuation")
		}

		payloadOffset := payloadStart
		for _, lace := range body[offset+27 : offset+27+segments] {
			length := int(lace)
			stream.packet = append(stream.packet, body[payloadOffset:payloadOffset+length]...)
			payloadOffset += length
			if length < 255 {
				acceptOggPacket(stream, stream.packet)
				stream.packet = stream.packet[:0]
			}
		}
		stream.nextSequence++
		pages++
		offset = pageEnd
	}
	if pages < 2 {
		return errors.New("voice profile: incomplete OGG audio stream")
	}
	for _, stream := range streams {
		if len(stream.packet) != 0 {
			return errors.New("voice profile: truncated OGG packet")
		}
		if stream.opusState == 2 && stream.opusAudioPackets > 0 {
			return nil
		}
	}
	return errors.New("voice profile: OGG has no valid Opus audio stream")
}

func oggCRC(page []byte) uint32 {
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

func acceptOggPacket(stream *oggStream, packet []byte) {
	switch stream.opusState {
	case 0:
		if validOpusHead(packet) {
			stream.opusState = 1
		} else {
			stream.opusState = -1
		}
	case 1:
		if validOpusTags(packet) {
			stream.opusState = 2
		} else {
			stream.opusState = -1
		}
	case 2:
		if validOpusPacket(packet) {
			stream.opusAudioPackets++
		} else {
			stream.opusState = -1
		}
	}
}

func validOpusHead(head []byte) bool {
	if len(head) < 19 || !bytes.Equal(head[:8], []byte("OpusHead")) {
		return false
	}
	if head[8] == 0 || head[8] > 15 || head[9] == 0 {
		return false
	}
	if head[18] == 0 {
		if len(head) != 19 || head[9] > 2 {
			return false
		}
	} else {
		return false
	}
	return true
}

func validOpusTags(packet []byte) bool {
	if len(packet) < 16 || !bytes.Equal(packet[:8], []byte("OpusTags")) {
		return false
	}
	vendorLength := int(binary.LittleEndian.Uint32(packet[8:12]))
	commentsOffset := 12 + vendorLength
	if commentsOffset < 12 || commentsOffset+4 > len(packet) {
		return false
	}
	comments := int(binary.LittleEndian.Uint32(packet[commentsOffset : commentsOffset+4]))
	offset := commentsOffset + 4
	for range comments {
		if offset+4 > len(packet) {
			return false
		}
		length := int(binary.LittleEndian.Uint32(packet[offset : offset+4]))
		offset += 4
		if length < 0 || offset+length < offset || offset+length > len(packet) {
			return false
		}
		offset += length
	}
	return true
}

func validOpusPacket(packet []byte) bool {
	if len(packet) < 1 {
		return false
	}
	duration := opusFrameDurationUnits(packet[0] >> 3)
	switch packet[0] & 0x03 {
	case 0:
		return len(packet)-1 <= 1275
	case 1:
		return duration*2 <= 480 && (len(packet)-1)%2 == 0 && (len(packet)-1)/2 <= 1275
	case 2:
		if len(packet) < 3 {
			return false
		}
		frameLength, sizeBytes := opusFrameLength(packet[1:])
		remaining := len(packet) - 1 - sizeBytes
		return duration*2 <= 480 && sizeBytes > 0 && frameLength <= 1275 && frameLength <= remaining && remaining-frameLength <= 1275
	case 3:
		return validOpusCode3(packet, duration)
	default:
		return false
	}
}

func validOpusCode3(packet []byte, duration int) bool {
	if len(packet) < 2 {
		return false
	}
	control := packet[1]
	frames := int(control & 0x3f)
	if frames == 0 || frames > 48 || duration*frames > 480 {
		return false
	}
	offset := 2
	padding := 0
	if control&0x40 != 0 {
		for {
			if offset >= len(packet) {
				return false
			}
			value := int(packet[offset])
			offset++
			padding += value
			if value != 255 {
				break
			}
			padding--
		}
	}
	payloadEnd := len(packet) - padding
	if payloadEnd < offset {
		return false
	}
	if control&0x80 == 0 {
		payloadBytes := payloadEnd - offset
		return payloadBytes%frames == 0 && payloadBytes/frames <= 1275
	}
	for range frames - 1 {
		frameLength, sizeBytes := opusFrameLength(packet[offset:payloadEnd])
		if sizeBytes == 0 || frameLength > 1275 {
			return false
		}
		offset += sizeBytes + frameLength
		if offset > payloadEnd {
			return false
		}
	}
	return payloadEnd-offset <= 1275
}

func opusFrameDurationUnits(config byte) int {
	switch {
	case config < 12:
		return [...]int{40, 80, 160, 240}[config%4]
	case config < 16:
		return [...]int{40, 80}[config%2]
	default:
		return [...]int{10, 20, 40, 80}[config%4]
	}
}

func opusFrameLength(body []byte) (int, int) {
	if len(body) == 0 {
		return 0, 0
	}
	if body[0] < 252 {
		return int(body[0]), 1
	}
	if len(body) < 2 {
		return 0, 0
	}
	return int(body[0]) + 4*int(body[1]), 2
}

func validateWAV(body []byte) error {
	if len(body) < 44 {
		return errors.New("voice profile: truncated WAV header")
	}
	declaredEnd := int64(binary.LittleEndian.Uint32(body[4:8])) + 8
	if declaredEnd > int64(len(body)) || declaredEnd < 12 {
		return errors.New("voice profile: truncated WAV container")
	}
	hasFormat := false
	hasAudio := false
	var formatBlockAlign uint16
	dataBytes := 0
	for offset := 12; offset+8 <= int(declaredEnd); {
		chunkSize := int(binary.LittleEndian.Uint32(body[offset+4 : offset+8]))
		chunkStart := offset + 8
		chunkEnd := chunkStart + chunkSize
		if chunkEnd > int(declaredEnd) || chunkEnd < chunkStart {
			return errors.New("voice profile: truncated WAV chunk")
		}
		switch string(body[offset : offset+4]) {
		case "fmt ":
			if chunkSize < 16 {
				return errors.New("voice profile: invalid WAV format chunk")
			}
			format := binary.LittleEndian.Uint16(body[chunkStart : chunkStart+2])
			channels := binary.LittleEndian.Uint16(body[chunkStart+2 : chunkStart+4])
			sampleRate := binary.LittleEndian.Uint32(body[chunkStart+4 : chunkStart+8])
			byteRate := binary.LittleEndian.Uint32(body[chunkStart+8 : chunkStart+12])
			blockAlign := binary.LittleEndian.Uint16(body[chunkStart+12 : chunkStart+14])
			bitsPerSample := binary.LittleEndian.Uint16(body[chunkStart+14 : chunkStart+16])
			validBits := (format == 1 && (bitsPerSample == 8 || bitsPerSample == 16 || bitsPerSample == 24 || bitsPerSample == 32)) ||
				(format == 3 && (bitsPerSample == 32 || bitsPerSample == 64))
			expectedBlockAlign := uint32(channels) * uint32(bitsPerSample) / 8
			expectedByteRate := uint64(sampleRate) * uint64(expectedBlockAlign)
			if !validBits || channels == 0 || sampleRate == 0 || expectedBlockAlign == 0 || expectedBlockAlign > 0xffff ||
				uint32(blockAlign) != expectedBlockAlign || expectedByteRate > 0xffffffff || uint64(byteRate) != expectedByteRate {
				return errors.New("voice profile: unsupported WAV audio format")
			}
			formatBlockAlign = blockAlign
			hasFormat = true
		case "data":
			hasAudio = hasAudio || chunkSize > 0
			dataBytes += chunkSize
		}
		offset = chunkEnd + chunkSize%2
	}
	if !hasFormat || !hasAudio || formatBlockAlign == 0 || dataBytes%int(formatBlockAlign) != 0 {
		return errors.New("voice profile: WAV has no decodable audio data")
	}
	return nil
}
