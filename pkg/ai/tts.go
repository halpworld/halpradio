package ai

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/halpworld/halpradio/pkg/util"
)

const (
	// maxModelFileBytes caps any single downloaded ONNX model file at 250 MB.
	maxModelFileBytes = 250 * 1024 * 1024
	defaultUserAgent  = "halpradio/0.7 (+https://github.com/halpworld/halpradio)"
)

// VoiceModelSpec describes an opt-in local ONNX voice model hosted on HuggingFace.
type VoiceModelSpec struct {
	ID        string
	Name      string
	Engine    string // "piper" or "sherpa-onnx"
	PersonaID string
	SizeLabel string
	ONNXURL   string
	ConfigURL string
}

var voiceModelCatalog = []VoiceModelSpec{
	{
		ID:        "en_US-lessac-medium",
		Name:      "Lessac (US Medium — Late Night Warm)",
		Engine:    "piper",
		PersonaID: "late-night-chill",
		SizeLabel: "63 MB",
		ONNXURL:   "https://huggingface.co/rhasspy/piper-voices/resolve/main/en/en_US/lessac/medium/en_US-lessac-medium.onnx",
		ConfigURL: "https://huggingface.co/rhasspy/piper-voices/resolve/main/en/en_US/lessac/medium/en_US-lessac-medium.onnx.json",
	},
	{
		ID:        "en_US-ryan-medium",
		Name:      "Ryan (US Medium — Retro 80s Broadcaster)",
		Engine:    "piper",
		PersonaID: "retro-80s",
		SizeLabel: "63 MB",
		ONNXURL:   "https://huggingface.co/rhasspy/piper-voices/resolve/main/en/en_US/ryan/medium/en_US-ryan-medium.onnx",
		ConfigURL: "https://huggingface.co/rhasspy/piper-voices/resolve/main/en/en_US/ryan/medium/en_US-ryan-medium.onnx.json",
	},
	{
		ID:        "en_US-amy-medium",
		Name:      "Amy (US Medium — Cyberpunk Synth)",
		Engine:    "piper",
		PersonaID: "cyberpunk",
		SizeLabel: "63 MB",
		ONNXURL:   "https://huggingface.co/rhasspy/piper-voices/resolve/main/en/en_US/amy/medium/en_US-amy-medium.onnx",
		ConfigURL: "https://huggingface.co/rhasspy/piper-voices/resolve/main/en/en_US/amy/medium/en_US-amy-medium.onnx.json",
	},
	{
		ID:        "en_GB-alan-medium",
		Name:      "Alan (UK Medium — BBC Radio 6 Curator)",
		Engine:    "piper",
		PersonaID: "bbc-presenter",
		SizeLabel: "63 MB",
		ONNXURL:   "https://huggingface.co/rhasspy/piper-voices/resolve/main/en/en_GB/alan/medium/en_GB-alan-medium.onnx",
		ConfigURL: "https://huggingface.co/rhasspy/piper-voices/resolve/main/en/en_GB/alan/medium/en_GB-alan-medium.onnx.json",
	},
	{
		ID:        "kokoro-en-v0_19",
		Name:      "Kokoro-82M (Local Neural ONNX)",
		Engine:    "sherpa-onnx",
		PersonaID: "late-night-chill",
		SizeLabel: "82 MB",
		ONNXURL:   "https://huggingface.co/hexgrad/Kokoro-82M/resolve/main/kokoro-v0_19.onnx",
		ConfigURL: "https://huggingface.co/hexgrad/Kokoro-82M/resolve/main/config.json",
	},
}

// DefaultVoiceModels returns a copy of the built-in ONNX voice model catalog.
func DefaultVoiceModels() []VoiceModelSpec {
	out := make([]VoiceModelSpec, len(voiceModelCatalog))
	copy(out, voiceModelCatalog)
	return out
}

// GetVoiceModelSpec looks up a voice model specification by ID.
func GetVoiceModelSpec(modelID string) (VoiceModelSpec, bool) {
	modelID = strings.TrimSpace(modelID)
	for _, spec := range voiceModelCatalog {
		if spec.ID == modelID {
			return spec, true
		}
	}
	return VoiceModelSpec{}, false
}

// VoiceModelForPersona returns the recommended default ONNX voice model ID for a persona.
func VoiceModelForPersona(personaID string) string {
	for _, spec := range voiceModelCatalog {
		if spec.PersonaID == personaID && spec.Engine == "piper" {
			return spec.ID
		}
	}
	return "en_US-lessac-medium"
}

// sanitizeModelID prevents directory traversal when mapping a model ID to disk.
func sanitizeModelID(modelID string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(modelID) {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		}
	}
	res := strings.Trim(b.String(), ".")
	if res == "" {
		return "en_US-lessac-medium"
	}
	return res
}

// ModelPaths returns the on-disk paths for a model's .onnx and .onnx.json files inside modelsDir.
func ModelPaths(modelsDir, modelID string) (onnxPath, configPath string) {
	safeID := sanitizeModelID(modelID)
	onnxPath = filepath.Join(modelsDir, safeID+".onnx")
	configPath = filepath.Join(modelsDir, safeID+".onnx.json")
	return onnxPath, configPath
}

// IsModelInstalledInDir reports whether both the .onnx and .onnx.json files exist and are non-empty in modelsDir.
func IsModelInstalledInDir(modelsDir, modelID string) bool {
	if modelsDir == "" || modelID == "" {
		return false
	}
	onnxPath, configPath := ModelPaths(modelsDir, modelID)
	info1, err1 := os.Stat(onnxPath)
	if err1 != nil || info1.IsDir() || info1.Size() == 0 {
		return false
	}
	info2, err2 := os.Stat(configPath)
	if err2 != nil || info2.IsDir() || info2.Size() == 0 {
		return false
	}
	return true
}

// IsModelInstalled reports whether the given ONNX voice model is installed in util.GetTTSModelsDir().
func IsModelInstalled(modelID string) bool {
	return IsModelInstalledInDir(util.GetTTSModelsDir(), modelID)
}

// DownloadVoiceModel downloads an opt-in ONNX voice model and its configuration JSON
// into util.GetTTSModelsDir(). This is only invoked on explicit user action.
func DownloadVoiceModel(ctx context.Context, modelID string, progressFn func(downloaded, total int64)) error {
	client := &http.Client{Timeout: 5 * time.Minute}
	return DownloadVoiceModelToDir(ctx, client, util.GetTTSModelsDir(), modelID, progressFn)
}

// DownloadVoiceModelToDir downloads a voice model from its spec URLs into modelsDir.
func DownloadVoiceModelToDir(ctx context.Context, client *http.Client, modelsDir, modelID string, progressFn func(downloaded, total int64)) error {
	spec, ok := GetVoiceModelSpec(modelID)
	if !ok {
		return fmt.Errorf("unknown voice model %q", modelID)
	}
	if modelsDir == "" {
		return errors.New("models directory not configured")
	}
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Minute}
	}
	if err := os.MkdirAll(modelsDir, 0o700); err != nil {
		return fmt.Errorf("create models dir: %w", err)
	}

	onnxPath, configPath := ModelPaths(modelsDir, spec.ID)
	if err := downloadFileAtomic(ctx, client, spec.ConfigURL, configPath, nil); err != nil {
		return fmt.Errorf("download model config: %w", err)
	}
	if err := downloadFileAtomic(ctx, client, spec.ONNXURL, onnxPath, progressFn); err != nil {
		_ = os.Remove(configPath)
		return fmt.Errorf("download model weights: %w", err)
	}
	return nil
}

func downloadFileAtomic(ctx context.Context, client *http.Client, rawURL, destPath string, progressFn func(downloaded, total int64)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", defaultUserAgent)

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d from %s", resp.StatusCode, rawURL)
	}

	dir := filepath.Dir(destPath)
	tmp, err := os.CreateTemp(dir, "model-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}

	total := resp.ContentLength
	var downloaded int64
	buf := make([]byte, 32*1024)
	limited := io.LimitReader(resp.Body, maxModelFileBytes+1)

	for {
		select {
		case <-ctx.Done():
			cleanup()
			return ctx.Err()
		default:
		}

		n, readErr := limited.Read(buf)
		if n > 0 {
			downloaded += int64(n)
			if downloaded > maxModelFileBytes {
				cleanup()
				return errors.New("model file exceeds maximum allowed size")
			}
			if _, wErr := tmp.Write(buf[:n]); wErr != nil {
				cleanup()
				return wErr
			}
			if progressFn != nil {
				progressFn(downloaded, total)
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			cleanup()
			return readErr
		}
	}

	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, destPath); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

// TTSEngine synthesizes and plays local DJ announcements.
type TTSEngine interface {
	Name() string
	Label() string
	Speak(ctx context.Context, text string, personaID string) error
}

// ResolveEngineKind selects the active TTS engine identifier ("piper",
// "sherpa-onnx", "native", or "silent") based on user preference and installed
// local binaries/models.
func ResolveEngineKind(preferred string, modelID string) string {
	return ResolveEngineKindInDir(preferred, util.GetTTSModelsDir(), modelID)
}

// ResolveEngineKindInDir is the testable form of ResolveEngineKind.
func ResolveEngineKindInDir(preferred string, modelsDir string, modelID string) string {
	pref := strings.ToLower(strings.TrimSpace(preferred))
	if modelID == "" {
		modelID = "en_US-lessac-medium"
	}

	switch pref {
	case "silent", "visual", "none":
		return "silent"
	case "piper":
		if hasPiperBinary() && IsModelInstalledInDir(modelsDir, modelID) {
			return "piper"
		}
	case "sherpa-onnx", "kokoro":
		if hasSherpaBinary() && IsModelInstalledInDir(modelsDir, modelID) {
			return "sherpa-onnx"
		}
	case "native", "say", "espeak":
		if detectNativeTTSBinary() != "" {
			return "native"
		}
		return "silent"
	}

	// Auto-detect priority:
	// 1. Piper ONNX if binary + model are ready
	if hasPiperBinary() && IsModelInstalledInDir(modelsDir, modelID) {
		return "piper"
	}
	// 2. Sherpa-ONNX if binary + model are ready
	if hasSherpaBinary() && IsModelInstalledInDir(modelsDir, modelID) {
		return "sherpa-onnx"
	}
	// 3. Native zero-download OS speech synthesis
	if detectNativeTTSBinary() != "" {
		return "native"
	}
	// 4. Visual-only silent broadcast
	return "silent"
}

// DetectEngine returns a human-readable status label for the active local TTS engine.
func DetectEngine(preferred string, modelID string) string {
	return DetectEngineInDir(preferred, util.GetTTSModelsDir(), modelID)
}

// DetectEngineInDir returns a human-readable status label using modelsDir.
func DetectEngineInDir(preferred string, modelsDir string, modelID string) string {
	kind := ResolveEngineKindInDir(preferred, modelsDir, modelID)
	switch kind {
	case "piper":
		return "Piper (Local ONNX, 0ms Cloud Lag)"
	case "sherpa-onnx":
		return "Sherpa-ONNX (Kokoro-82M Local)"
	case "native":
		bin := detectNativeTTSBinary()
		switch bin {
		case "say":
			return "macOS Neural Say (0ms Cloud Lag)"
		case "espeak-ng", "espeak":
			return bin + " (Local Offline)"
		case "spd-say":
			return "Speech Dispatcher (Local Offline)"
		case "powershell":
			return "Windows SAPI (Local Offline)"
		default:
			return "Native OS Speech (0ms Cloud Lag)"
		}
	default:
		return "Visual Broadcast (0ms Cloud Lag)"
	}
}

func hasPiperBinary() bool {
	_, err := exec.LookPath("piper")
	return err == nil
}

func hasSherpaBinary() bool {
	_, err := exec.LookPath("sherpa-onnx-offline-tts")
	return err == nil
}

func detectNativeTTSBinary() string {
	switch runtime.GOOS {
	case "darwin":
		if _, err := exec.LookPath("say"); err == nil {
			return "say"
		}
	case "linux":
		for _, bin := range []string{"espeak-ng", "espeak", "spd-say"} {
			if _, err := exec.LookPath(bin); err == nil {
				return bin
			}
		}
	case "windows":
		if _, err := exec.LookPath("powershell"); err == nil {
			return "powershell"
		}
	}
	return ""
}

// EstimateSpeechDuration estimates how long a DJ script takes to read aloud
// (~165 words per minute), bounded between minDur and 6s.
func EstimateSpeechDuration(text string, minDur time.Duration) time.Duration {
	if minDur <= 0 {
		minDur = 2500 * time.Millisecond
	}
	words := len(strings.Fields(text))
	if words == 0 {
		return minDur
	}
	// ~360ms per word (~166 WPM)
	dur := time.Duration(words) * 360 * time.Millisecond
	if dur < minDur {
		dur = minDur
	}
	if dur > 6*time.Second {
		dur = 6 * time.Second
	}
	return dur
}

// LocalTTSEngine implements TTSEngine across Piper ONNX, Sherpa-ONNX, Native OS TTS, and Silent modes.
type LocalTTSEngine struct {
	Preferred     string
	ModelsDir     string
	ModelID       string
	MinSilentWait time.Duration
}

// NewTTSEngine creates a LocalTTSEngine configured for the user's preferences.
func NewTTSEngine(preferred string, modelID string) *LocalTTSEngine {
	if modelID == "" {
		modelID = "en_US-lessac-medium"
	}
	return &LocalTTSEngine{
		Preferred:     preferred,
		ModelsDir:     util.GetTTSModelsDir(),
		ModelID:       modelID,
		MinSilentWait: 2500 * time.Millisecond,
	}
}

func (e *LocalTTSEngine) Name() string {
	modelsDir := e.ModelsDir
	if modelsDir == "" {
		modelsDir = util.GetTTSModelsDir()
	}
	return ResolveEngineKindInDir(e.Preferred, modelsDir, e.ModelID)
}

func (e *LocalTTSEngine) Label() string {
	modelsDir := e.ModelsDir
	if modelsDir == "" {
		modelsDir = util.GetTTSModelsDir()
	}
	return DetectEngineInDir(e.Preferred, modelsDir, e.ModelID)
}

// Speak synthesizes and plays text using the resolved engine, respecting context cancellation.
func (e *LocalTTSEngine) Speak(ctx context.Context, text string, personaID string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}

	kind := e.Name()
	switch kind {
	case "piper":
		if err := e.speakPiper(ctx, text); err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		// Fall back if piper invocation failed
		if detectNativeTTSBinary() != "" {
			if err := e.speakNative(ctx, text, personaID); err == nil || errors.Is(err, context.Canceled) {
				return err
			}
		}
	case "sherpa-onnx":
		if err := e.speakSherpa(ctx, text); err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
	case "native":
		if err := e.speakNative(ctx, text, personaID); err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
	}

	return e.speakSilent(ctx, text)
}

func (e *LocalTTSEngine) speakSilent(ctx context.Context, text string) error {
	wait := EstimateSpeechDuration(text, e.MinSilentWait)
	timer := time.NewTimer(wait)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (e *LocalTTSEngine) speakPiper(ctx context.Context, text string) error {
	modelsDir := e.ModelsDir
	if modelsDir == "" {
		modelsDir = util.GetTTSModelsDir()
	}
	onnxPath, _ := ModelPaths(modelsDir, e.ModelID)

	tmpWav, err := os.CreateTemp("", "halpradio-dj-*.wav")
	if err != nil {
		return err
	}
	wavPath := tmpWav.Name()
	_ = tmpWav.Close()
	defer os.Remove(wavPath)

	cmd := exec.CommandContext(ctx, "piper", "--model", onnxPath, "--output_file", wavPath)
	cmd.Stdin = bytes.NewReader([]byte(text))
	if err := cmd.Run(); err != nil {
		return err
	}
	return playAudioFile(ctx, wavPath)
}

func (e *LocalTTSEngine) speakSherpa(ctx context.Context, text string) error {
	modelsDir := e.ModelsDir
	if modelsDir == "" {
		modelsDir = util.GetTTSModelsDir()
	}
	onnxPath, _ := ModelPaths(modelsDir, e.ModelID)

	tmpWav, err := os.CreateTemp("", "halpradio-dj-*.wav")
	if err != nil {
		return err
	}
	wavPath := tmpWav.Name()
	_ = tmpWav.Close()
	defer os.Remove(wavPath)

	cmd := exec.CommandContext(ctx, "sherpa-onnx-offline-tts",
		"--kokoro-model="+onnxPath,
		"--output-filename="+wavPath,
		text,
	)
	if err := cmd.Run(); err != nil {
		return err
	}
	return playAudioFile(ctx, wavPath)
}

func (e *LocalTTSEngine) speakNative(ctx context.Context, text string, personaID string) error {
	bin := detectNativeTTSBinary()
	switch bin {
	case "say":
		args := buildMacOSSayArgs(text, personaID)
		cmd := exec.CommandContext(ctx, "say", args...)
		return cmd.Run()
	case "espeak-ng", "espeak":
		args := buildEspeakArgs(text, personaID)
		cmd := exec.CommandContext(ctx, bin, args...)
		return cmd.Run()
	case "spd-say":
		cmd := exec.CommandContext(ctx, "spd-say", "--wait", "--", text)
		return cmd.Run()
	default:
		return errors.New("no native TTS binary available")
	}
}

func buildMacOSSayArgs(text string, personaID string) []string {
	rate := "182"
	switch personaID {
	case "late-night-chill":
		rate = "172"
	case "retro-80s":
		rate = "195"
	case "cyberpunk":
		rate = "188"
	case "bbc-presenter":
		rate = "178"
	}
	return []string{"-r", rate, "--", text}
}

func buildEspeakArgs(text string, personaID string) []string {
	speed := "165"
	voice := "en-us"
	switch personaID {
	case "late-night-chill":
		speed = "155"
	case "retro-80s":
		speed = "180"
	case "cyberpunk":
		speed = "175"
	case "bbc-presenter":
		speed = "160"
		voice = "en-gb"
	}
	return []string{"-v", voice, "-s", speed, "--", text}
}

func playAudioFile(ctx context.Context, wavPath string) error {
	switch runtime.GOOS {
	case "darwin":
		if _, err := exec.LookPath("afplay"); err == nil {
			return exec.CommandContext(ctx, "afplay", wavPath).Run()
		}
	case "linux":
		for _, player := range []string{"paplay", "aplay"} {
			if _, err := exec.LookPath(player); err == nil {
				return exec.CommandContext(ctx, player, wavPath).Run()
			}
		}
	}
	for _, player := range []string{"ffplay", "mpv"} {
		if _, err := exec.LookPath(player); err == nil {
			if player == "ffplay" {
				return exec.CommandContext(ctx, "ffplay", "-nodisp", "-autoexit", "-loglevel", "quiet", "--", wavPath).Run()
			}
			return exec.CommandContext(ctx, "mpv", "--no-video", "--no-terminal", "--really-quiet", "--", wavPath).Run()
		}
	}
	return errors.New("no audio player binary found for WAV output")
}
