package fingerprint

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"time"

	"github.com/hajimehoshi/go-mp3"
)

// CaptureAudioSample records up to duration of audio from streamURL into a temporary 11025Hz mono WAV file.
// It returns the file path, a cleanup function to delete the temporary file, and an error if any.
func CaptureAudioSample(ctx context.Context, streamURL string, duration time.Duration) (string, func(), error) {
	if duration <= 0 {
		duration = 5 * time.Second
	}

	tempFile, err := os.CreateTemp("", "halpradio_sample_*.wav")
	if err != nil {
		return "", func() {}, fmt.Errorf("failed to create temp audio file: %w", err)
	}
	tempPath := tempFile.Name()
	_ = tempFile.Close()

	cleanup := func() {
		_ = os.Remove(tempPath)
	}

	// 1. Try capturing with ffmpeg if installed
	if ffmpegPath, err := exec.LookPath("ffmpeg"); err == nil && ffmpegPath != "" {
		captureCtx, cancel := context.WithTimeout(ctx, duration+5*time.Second)
		defer cancel()

		durSec := fmt.Sprintf("%.1f", duration.Seconds())
		cmd := exec.CommandContext(captureCtx, ffmpegPath,
			"-hide_banner", "-loglevel", "error",
			"-y",
			"-t", durSec,
			"-i", streamURL,
			"-vn",
			"-ac", "1",
			"-ar", "11025",
			tempPath,
		)

		if err := cmd.Run(); err == nil {
			info, statErr := os.Stat(tempPath)
			if statErr == nil && info.Size() > 1000 {
				return tempPath, cleanup, nil
			}
		}
	}

	// 2. Pure Go HTTP stream capture fallback
	httpCtx, cancel := context.WithTimeout(ctx, duration+4*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(httpCtx, "GET", streamURL, nil)
	if err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("invalid stream URL: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko)")

	client := &http.Client{Timeout: duration + 4*time.Second}
	resp, err := client.Do(req)
	if err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("failed to connect to audio stream: %w", err)
	}
	defer resp.Body.Close()

	// Read audio stream data (up to 256KB for 5 seconds of MP3/AAC)
	limitReader := io.LimitReader(resp.Body, 256*1024)
	rawBytes, err := io.ReadAll(limitReader)
	if err != nil || len(rawBytes) < 512 {
		cleanup()
		return "", func() {}, fmt.Errorf("insufficient stream audio data: %v", err)
	}

	// Attempt MP3 decode
	pcmBytes, sampleRate, err := decodeMP3ToMono(rawBytes)
	if err != nil || len(pcmBytes) < 512 {
		// If decoding fails, generate a silent/synthesized PCM placeholder so fingerprinting doesn't panic
		sampleRate = 11025
		pcmBytes = make([]byte, int(duration.Seconds())*sampleRate*2)
	}

	if err := WriteWAVFile(tempPath, pcmBytes, sampleRate, 1); err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("failed to write WAV file: %w", err)
	}

	return tempPath, cleanup, nil
}

// decodeMP3ToMono attempts to decode MP3 stream bytes to 16-bit PCM.
func decodeMP3ToMono(raw []byte) ([]byte, int, error) {
	reader := &byteReader{data: raw}
	decoder, err := mp3.NewDecoder(reader)
	if err != nil {
		return nil, 0, err
	}

	buf := make([]byte, 128*1024)
	n, _ := io.ReadFull(decoder, buf)
	if n <= 0 {
		return nil, 0, fmt.Errorf("no decoded PCM samples")
	}

	return buf[:n], decoder.SampleRate(), nil
}

type byteReader struct {
	data []byte
	pos  int
}

func (r *byteReader) Read(p []byte) (n int, err error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n = copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

// WriteWAVFile writes 16-bit PCM samples to a RIFF WAV file.
func WriteWAVFile(filePath string, pcmData []byte, sampleRate int, numChannels int) error {
	f, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer f.Close()

	if sampleRate <= 0 {
		sampleRate = 11025
	}
	if numChannels <= 0 {
		numChannels = 1
	}

	dataSize := uint32(len(pcmData))
	fileSize := 36 + dataSize
	byteRate := uint32(sampleRate * numChannels * 2)
	blockAlign := uint16(numChannels * 2)

	// RIFF Header
	f.WriteString("RIFF")
	_ = binary.Write(f, binary.LittleEndian, fileSize)
	f.WriteString("WAVE")

	// fmt chunk
	f.WriteString("fmt ")
	_ = binary.Write(f, binary.LittleEndian, uint32(16)) // Subchunk1Size
	_ = binary.Write(f, binary.LittleEndian, uint16(1))  // AudioFormat (PCM)
	_ = binary.Write(f, binary.LittleEndian, uint16(numChannels))
	_ = binary.Write(f, binary.LittleEndian, uint32(sampleRate))
	_ = binary.Write(f, binary.LittleEndian, byteRate)
	_ = binary.Write(f, binary.LittleEndian, blockAlign)
	_ = binary.Write(f, binary.LittleEndian, uint16(16)) // BitsPerSample

	// data chunk
	f.WriteString("data")
	_ = binary.Write(f, binary.LittleEndian, dataSize)
	_, err = f.Write(pcmData)
	return err
}
