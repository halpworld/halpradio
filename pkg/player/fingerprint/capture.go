package fingerprint

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/halpworld/halpradio/pkg/player/mp3safe"
	"github.com/halpworld/halpradio/pkg/player/streamnet"
	"github.com/halpworld/halpradio/pkg/player/streamproxy"
)

// ffmpegProtocols limits what ffmpeg may open for a capture: network
// streaming only, never file:, pipe:, concat: or other local sources.
const ffmpegProtocols = "http,https,tcp,tls,crypto,httpproxy"

// startCaptureProxy starts the stream guard proxy for a strict capture;
// tests replace it.
var startCaptureProxy = func() (*streamproxy.Proxy, error) { return streamproxy.Start(streamproxy.Options{}) }

// ffmpegCaptureArgs is the ffmpeg argv for one capture.
func ffmpegCaptureArgs(streamURL string, duration time.Duration, outPath string) []string {
	return []string{
		"-hide_banner", "-loglevel", "error",
		"-y",
		"-t", fmt.Sprintf("%.1f", duration.Seconds()),
		"-protocol_whitelist", ffmpegProtocols,
		"-i", streamURL,
		"-vn",
		"-ac", "1",
		"-ar", "11025",
		outPath,
	}
}

// captureEnv is the environment for a strict ffmpeg capture: every proxy
// variable (including no_proxy, which libavformat honours even with a
// proxy set) is replaced by the guard proxy. The credential travels in the
// environment, not argv, so it does not show in the process list.
func captureEnv(base []string, proxyURL string) []string {
	out := make([]string, 0, len(base)+2)
	for _, kv := range base {
		name, _, _ := strings.Cut(kv, "=")
		switch strings.ToLower(name) {
		case "http_proxy", "https_proxy", "all_proxy", "no_proxy", "ftp_proxy":
			continue
		}
		out = append(out, kv)
	}
	return append(out, "http_proxy="+proxyURL, "https_proxy="+proxyURL)
}

// validCaptureURL accepts only absolute http/https URLs with a host. Anything
// else (including a leading "-" that ffmpeg would read as an option) is refused.
func validCaptureURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || strings.HasPrefix(raw, "-") {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https"
}

// CaptureAudioSample records up to duration of audio from streamURL into a temporary 11025Hz mono WAV file.
// It returns the file path, a cleanup function to delete the temporary file, and an error if any.
// Restricted (loopback, LAN, link-local, metadata) destinations are refused;
// see captureAudioSample for the variant that allows them for user stations.
func CaptureAudioSample(ctx context.Context, streamURL string, duration time.Duration) (string, func(), error) {
	return captureAudioSample(ctx, streamURL, duration, false, nil)
}

// captureAudioSample is CaptureAudioSample with an explicit private-host
// decision. When allowPrivate is false the host is resolved first and any
// restricted answer refuses the capture; ffmpeg then runs behind the stream
// guard proxy and the Go fallback checks every dialled address, so
// redirects and re-resolutions are covered on both paths.
func captureAudioSample(ctx context.Context, streamURL string, duration time.Duration, allowPrivate bool, lookup streamnet.LookupFunc) (string, func(), error) {
	if duration <= 0 {
		duration = 5 * time.Second
	}
	streamURL = strings.TrimSpace(streamURL)
	if !validCaptureURL(streamURL) {
		return "", func() {}, fmt.Errorf("invalid stream URL: only http/https are supported")
	}
	if !allowPrivate {
		checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := streamnet.CheckHostPublic(checkCtx, streamURL, lookup)
		cancel()
		if err != nil {
			return "", func() {}, fmt.Errorf("stream host refused: %w", err)
		}
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

	// 1. Try capturing with ffmpeg if installed. ffmpeg follows redirects
	// and resolves hosts itself, so for a strict capture every request goes
	// through the stream guard proxy; if it cannot start, ffmpeg is skipped
	// (never run direct) and the guarded Go fallback below is used.
	var env []string
	ffmpegOK := true
	if !allowPrivate {
		px, err := startCaptureProxy()
		if err != nil {
			ffmpegOK = false
		} else {
			defer px.Close()
			env = captureEnv(os.Environ(), px.URL())
		}
	}
	if ffmpegPath, err := exec.LookPath("ffmpeg"); ffmpegOK && err == nil && ffmpegPath != "" {
		captureCtx, cancel := context.WithTimeout(ctx, duration+5*time.Second)
		defer cancel()

		cmd := exec.CommandContext(captureCtx, ffmpegPath, ffmpegCaptureArgs(streamURL, duration, tempPath)...)
		cmd.Env = env // nil: inherit (user stations only)

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

	// Same redirect/destination policy as playback: http/https only, capped
	// redirects, restricted addresses only when allowed and never after a
	// public first hop. Proxy environment variables are deliberately not
	// honoured (streamnet sets Proxy: nil) so the dial check sees the real
	// destination.
	client := streamnet.NewClient(streamnet.Options{Timeout: duration + 4*time.Second, AllowPrivate: allowPrivate})
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

// decodeMP3ToMono attempts to decode MP3 stream bytes to 16-bit PCM. Stream
// bytes are untrusted: mp3safe bounds ID3 allocations and turns decoder panics
// into errors, and the recover below covers anything else on this path.
func decodeMP3ToMono(raw []byte) (pcm []byte, rate int, err error) {
	defer func() {
		if r := recover(); r != nil {
			pcm, rate, err = nil, 0, fmt.Errorf("mp3 decode panic: %v", r)
		}
	}()
	reader := &byteReader{data: raw}
	decoder, err := mp3safe.NewDecoder(reader)
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
