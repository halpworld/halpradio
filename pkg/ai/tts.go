package ai

import (
	"archive/tar"
	"bytes"
	"compress/bzip2"
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
	"sync"
	"time"

	"github.com/halpworld/halpradio/pkg/util"
)

const (
	// maxModelFileBytes caps any single downloaded ONNX model archive at 250 MB.
	maxModelFileBytes = 250 * 1024 * 1024
	defaultUserAgent  = "halpradio/0.8 (+https://github.com/halpworld/halpradio)"
)

// VoiceModelSpec describes an opt-in local ONNX voice model.
type VoiceModelSpec struct {
	ID        string
	Name      string
	Engine    string // "piper" or "sherpa-onnx"
	PersonaID string
	SizeLabel string
	BundleURL string // Self-contained sherpa-onnx VITS/Kokoro bundle (.tar.bz2 with model, tokens.txt, espeak-ng-data)
	BundleDir string // Top-level directory inside BundleURL archive
	ONNXFile  string // Primary .onnx filename inside BundleDir
	ONNXURL   string // Direct single-file fallback URL (also used by unit tests)
	ConfigURL string // Direct config fallback URL
}

var voiceModelCatalog = []VoiceModelSpec{
	{
		ID:        "en_US-lessac-medium",
		Name:      "Lessac (US Medium — Late Night Warm)",
		Engine:    "piper",
		PersonaID: "late-night-chill",
		SizeLabel: "21 MB",
		BundleURL: "https://github.com/k2-fsa/sherpa-onnx/releases/download/tts-models/vits-piper-en_US-lessac-medium-int8.tar.bz2",
		BundleDir: "vits-piper-en_US-lessac-medium-int8",
		ONNXFile:  "en_US-lessac-medium.onnx",
		ONNXURL:   "https://huggingface.co/rhasspy/piper-voices/resolve/main/en/en_US/lessac/medium/en_US-lessac-medium.onnx",
		ConfigURL: "https://huggingface.co/rhasspy/piper-voices/resolve/main/en/en_US/lessac/medium/en_US-lessac-medium.onnx.json",
	},
	{
		ID:        "en_US-ryan-medium",
		Name:      "Ryan (US Medium — Retro 80s Broadcaster)",
		Engine:    "piper",
		PersonaID: "retro-80s",
		SizeLabel: "21 MB",
		BundleURL: "https://github.com/k2-fsa/sherpa-onnx/releases/download/tts-models/vits-piper-en_US-ryan-medium-int8.tar.bz2",
		BundleDir: "vits-piper-en_US-ryan-medium-int8",
		ONNXFile:  "en_US-ryan-medium.onnx",
		ONNXURL:   "https://huggingface.co/rhasspy/piper-voices/resolve/main/en/en_US/ryan/medium/en_US-ryan-medium.onnx",
		ConfigURL: "https://huggingface.co/rhasspy/piper-voices/resolve/main/en/en_US/ryan/medium/en_US-ryan-medium.onnx.json",
	},
	{
		ID:        "en_US-amy-medium",
		Name:      "Amy (US Medium — Cyberpunk Synth)",
		Engine:    "piper",
		PersonaID: "cyberpunk",
		SizeLabel: "21 MB",
		BundleURL: "https://github.com/k2-fsa/sherpa-onnx/releases/download/tts-models/vits-piper-en_US-amy-medium-int8.tar.bz2",
		BundleDir: "vits-piper-en_US-amy-medium-int8",
		ONNXFile:  "en_US-amy-medium.onnx",
		ONNXURL:   "https://huggingface.co/rhasspy/piper-voices/resolve/main/en/en_US/amy/medium/en_US-amy-medium.onnx",
		ConfigURL: "https://huggingface.co/rhasspy/piper-voices/resolve/main/en/en_US/amy/medium/en_US-amy-medium.onnx.json",
	},
	{
		ID:        "en_GB-alan-medium",
		Name:      "Alan (UK Medium — BBC Radio 6 Curator)",
		Engine:    "piper",
		PersonaID: "bbc-presenter",
		SizeLabel: "21 MB",
		BundleURL: "https://github.com/k2-fsa/sherpa-onnx/releases/download/tts-models/vits-piper-en_GB-alan-medium-int8.tar.bz2",
		BundleDir: "vits-piper-en_GB-alan-medium-int8",
		ONNXFile:  "en_GB-alan-medium.onnx",
		ONNXURL:   "https://huggingface.co/rhasspy/piper-voices/resolve/main/en/en_GB/alan/medium/en_GB-alan-medium.onnx",
		ConfigURL: "https://huggingface.co/rhasspy/piper-voices/resolve/main/en/en_GB/alan/medium/en_GB-alan-medium.onnx.json",
	},
	{
		ID:        "kokoro-en-v0_19",
		Name:      "Kokoro-82M (Local Neural ONNX)",
		Engine:    "sherpa-onnx",
		PersonaID: "late-night-chill",
		SizeLabel: "98 MB",
		BundleURL: "https://github.com/k2-fsa/sherpa-onnx/releases/download/tts-models/kokoro-int8-en-v0_19.tar.bz2",
		BundleDir: "kokoro-int8-en-v0_19",
		ONNXFile:  "model.int8.onnx",
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

// ModelPaths returns the on-disk paths for a model's .onnx and .onnx.json (or tokens.txt) files inside modelsDir.
func ModelPaths(modelsDir, modelID string) (onnxPath, configPath string) {
	safeID := sanitizeModelID(modelID)
	bundleDir := filepath.Join(modelsDir, safeID)
	if spec, ok := GetVoiceModelSpec(safeID); ok && spec.ONNXFile != "" {
		bOnnx := filepath.Join(bundleDir, spec.ONNXFile)
		bTokens := filepath.Join(bundleDir, "tokens.txt")
		if fileExistsNonEmpty(bOnnx) && fileExistsNonEmpty(bTokens) {
			return bOnnx, bTokens
		}
	}
	onnxPath = filepath.Join(modelsDir, safeID+".onnx")
	configPath = filepath.Join(modelsDir, safeID+".onnx.json")
	return onnxPath, configPath
}

func fileExistsNonEmpty(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Size() > 0
}

// IsModelInstalledInDir reports whether a complete runnable ONNX voice bundle
// (or direct .onnx + .onnx.json pair) exists in modelsDir.
func IsModelInstalledInDir(modelsDir, modelID string) bool {
	if modelsDir == "" || modelID == "" {
		return false
	}
	safeID := sanitizeModelID(modelID)
	bundleDir := filepath.Join(modelsDir, safeID)
	if spec, ok := GetVoiceModelSpec(safeID); ok && spec.ONNXFile != "" {
		if fileExistsNonEmpty(filepath.Join(bundleDir, spec.ONNXFile)) &&
			fileExistsNonEmpty(filepath.Join(bundleDir, "tokens.txt")) {
			return true
		}
	}
	// Direct two-file fallback (used when user has standalone `piper` binary on PATH or in tests)
	onnxPath := filepath.Join(modelsDir, safeID+".onnx")
	configPath := filepath.Join(modelsDir, safeID+".onnx.json")
	if fileExistsNonEmpty(onnxPath) && fileExistsNonEmpty(configPath) {
		// If neither standalone piper nor the extracted bundle tokens exist, and the spec
		// has a BundleURL, require the full bundle so sherpa-onnx can actually synthesize it.
		if spec, ok := GetVoiceModelSpec(safeID); ok && strings.HasPrefix(spec.ONNXURL, "https://huggingface.co/") && !hasStandalonePiperBinary() {
			return false
		}
		return true
	}
	return false
}

// IsModelInstalled reports whether the given ONNX voice model is installed in util.GetTTSModelsDir().
func IsModelInstalled(modelID string) bool {
	return IsModelInstalledInDir(util.GetTTSModelsDir(), modelID)
}

// sherpaRuntimeArchiveURL returns the prebuilt sherpa-onnx runtime archive URL for the current OS and architecture.
func sherpaRuntimeArchiveURL() (url string, topDir string, ok bool) {
	switch runtime.GOOS {
	case "darwin":
		if runtime.GOARCH == "arm64" {
			return "https://github.com/k2-fsa/sherpa-onnx/releases/download/v1.13.8/sherpa-onnx-v1.13.8-osx-arm64-shared.tar.bz2",
				"sherpa-onnx-v1.13.8-osx-arm64-shared", true
		}
		if runtime.GOARCH == "amd64" {
			return "https://github.com/k2-fsa/sherpa-onnx/releases/download/v1.13.8/sherpa-onnx-v1.13.8-osx-x64-shared.tar.bz2",
				"sherpa-onnx-v1.13.8-osx-x64-shared", true
		}
	case "linux":
		if runtime.GOARCH == "amd64" {
			return "https://github.com/k2-fsa/sherpa-onnx/releases/download/v1.13.8/sherpa-onnx-v1.13.8-linux-x64-shared.tar.bz2",
				"sherpa-onnx-v1.13.8-linux-x64-shared", true
		}
		if runtime.GOARCH == "arm64" {
			return "https://github.com/k2-fsa/sherpa-onnx/releases/download/v1.13.8/sherpa-onnx-v1.13.8-linux-aarch64-shared-cpu.tar.bz2",
				"sherpa-onnx-v1.13.8-linux-aarch64-shared-cpu", true
		}
	case "windows":
		if runtime.GOARCH == "amd64" {
			return "https://github.com/k2-fsa/sherpa-onnx/releases/download/v1.13.8/sherpa-onnx-v1.13.8-win-x64-shared-MT-Release.tar.bz2",
				"sherpa-onnx-v1.13.8-win-x64-shared-MT-Release", true
		}
		if runtime.GOARCH == "arm64" {
			return "https://github.com/k2-fsa/sherpa-onnx/releases/download/v1.13.8/sherpa-onnx-v1.13.8-win-arm64-shared-MT-Release.tar.bz2",
				"sherpa-onnx-v1.13.8-win-arm64-shared-MT-Release", true
		}
	}
	return "", "", false
}

// DownloadVoiceModel downloads an opt-in ONNX voice model (and the lightweight
// local sherpa-onnx TTS runner if not already present) into util.GetTTSModelsDir().
// This is only invoked on explicit user action.
func DownloadVoiceModel(ctx context.Context, modelID string, progressFn func(downloaded, total int64)) error {
	client := &http.Client{Timeout: 5 * time.Minute}
	return DownloadVoiceModelToDir(ctx, client, util.GetTTSModelsDir(), modelID, progressFn)
}

// DownloadVoiceModelToDir downloads a voice model into modelsDir.
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

	// When ONNXURL is overridden (e.g. in unit tests using httptest.Server), use direct file download.
	isCustomTestURL := !strings.HasPrefix(spec.ONNXURL, "https://huggingface.co/")
	if isCustomTestURL || spec.BundleURL == "" {
		safeID := sanitizeModelID(spec.ID)
		onnxPath := filepath.Join(modelsDir, safeID+".onnx")
		configPath := filepath.Join(modelsDir, safeID+".onnx.json")
		if err := downloadFileAtomic(ctx, client, spec.ConfigURL, configPath, nil); err != nil {
			return fmt.Errorf("download model config: %w", err)
		}
		if err := downloadFileAtomic(ctx, client, spec.ONNXURL, onnxPath, progressFn); err != nil {
			_ = os.Remove(configPath)
			return fmt.Errorf("download model weights: %w", err)
		}
		return nil
	}

	// Ensure the local sherpa-onnx TTS runtime binary is installed alongside modelsDir
	if resolveSherpaBinaryInDir(modelsDir) == "" && !hasStandalonePiperBinary() {
		if err := ensureSherpaRuntimeInDir(ctx, client, modelsDir); err != nil {
			return fmt.Errorf("install local ONNX TTS runtime: %w", err)
		}
	}

	// Download and extract the self-contained ONNX voice bundle (.tar.bz2)
	targetDir := filepath.Join(modelsDir, sanitizeModelID(spec.ID))
	if err := downloadAndExtractTarBz2(ctx, client, spec.BundleURL, spec.BundleDir, targetDir, nil, progressFn); err != nil {
		return fmt.Errorf("download voice bundle %s: %w", spec.ID, err)
	}
	return nil
}

func ensureSherpaRuntimeInDir(ctx context.Context, client *http.Client, modelsDir string) error {
	archiveURL, topDir, ok := sherpaRuntimeArchiveURL()
	if !ok {
		return errors.New("no prebuilt ONNX runtime available for this OS/architecture")
	}
	runtimeDir := filepath.Join(modelsDir, "_runtime")
	keepFilter := func(relPath string) bool {
		if strings.HasPrefix(relPath, "lib/") {
			return true
		}
		if relPath == "bin/sherpa-onnx-offline-tts" || relPath == "bin/sherpa-onnx-offline-tts.exe" {
			return true
		}
		return false
	}
	return downloadAndExtractTarBz2(ctx, client, archiveURL, topDir, runtimeDir, keepFilter, nil)
}

func downloadAndExtractTarBz2(
	ctx context.Context,
	client *http.Client,
	rawURL string,
	stripPrefixDir string,
	destDir string,
	keepFilter func(relPath string) bool,
	progressFn func(downloaded, total int64),
) error {
	tmpArchive, err := os.CreateTemp(filepath.Dir(destDir), "onnx-bundle-*.tar.bz2")
	if err != nil {
		return err
	}
	tmpArchivePath := tmpArchive.Name()
	_ = tmpArchive.Close()
	defer os.Remove(tmpArchivePath)

	if err := downloadFileAtomic(ctx, client, rawURL, tmpArchivePath, progressFn); err != nil {
		return err
	}

	f, err := os.Open(tmpArchivePath)
	if err != nil {
		return err
	}
	defer f.Close()

	stagingDir := destDir + ".tmp"
	_ = os.RemoveAll(stagingDir)
	if err := os.MkdirAll(stagingDir, 0o700); err != nil {
		return err
	}
	cleanupStaging := func() {
		_ = os.RemoveAll(stagingDir)
	}

	bzReader := bzip2.NewReader(f)
	tr := tar.NewReader(bzReader)

	prefix := strings.Trim(stripPrefixDir, "/") + "/"
	for {
		select {
		case <-ctx.Done():
			cleanupStaging()
			return ctx.Err()
		default:
		}

		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			cleanupStaging()
			return err
		}

		name := filepath.ToSlash(strings.TrimPrefix(hdr.Name, "./"))
		if stripPrefixDir != "" && strings.HasPrefix(name, prefix) {
			name = strings.TrimPrefix(name, prefix)
		}
		name = strings.TrimPrefix(name, "/")
		if name == "" || strings.Contains(name, "..") {
			continue
		}
		if keepFilter != nil && !keepFilter(name) {
			continue
		}

		outPath := filepath.Join(stagingDir, filepath.FromSlash(name))
		// Guard against zip-slip
		if !strings.HasPrefix(filepath.Clean(outPath)+string(os.PathSeparator), filepath.Clean(stagingDir)+string(os.PathSeparator)) {
			continue
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(outPath, 0o700); err != nil {
				cleanupStaging()
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(outPath), 0o700); err != nil {
				cleanupStaging()
				return err
			}
			mode := os.FileMode(hdr.Mode) & 0o777
			if mode == 0 {
				mode = 0o600
			}
			if strings.HasPrefix(name, "bin/") || strings.HasSuffix(name, ".dylib") || strings.Contains(name, ".so") || strings.HasSuffix(name, ".dll") {
				mode |= 0o700
			}
			outFile, err := os.OpenFile(outPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
			if err != nil {
				cleanupStaging()
				return err
			}
			_, copyErr := io.Copy(outFile, io.LimitReader(tr, maxModelFileBytes))
			closeErr := outFile.Close()
			if copyErr != nil {
				cleanupStaging()
				return copyErr
			}
			if closeErr != nil {
				cleanupStaging()
				return closeErr
			}
		}
	}

	_ = os.RemoveAll(destDir)
	if err := os.Rename(stagingDir, destDir); err != nil {
		cleanupStaging()
		return err
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

	hasONNXRunner := hasStandalonePiperBinary() || resolveSherpaBinaryInDir(modelsDir) != ""
	modelReady := IsModelInstalledInDir(modelsDir, modelID)

	switch pref {
	case "silent", "visual", "none":
		return "silent"
	case "piper":
		if hasONNXRunner && modelReady {
			return "piper"
		}
	case "sherpa-onnx", "kokoro":
		if resolveSherpaBinaryInDir(modelsDir) != "" && modelReady {
			if strings.HasPrefix(modelID, "kokoro") {
				return "sherpa-onnx"
			}
			return "piper"
		}
	case "native", "say", "espeak":
		if detectNativeTTSBinary() != "" {
			return "native"
		}
		return "silent"
	}

	// Auto-detect priority:
	// 1. Local ONNX (Piper VITS or Kokoro-82M) if runtime + model are installed
	if hasONNXRunner && modelReady {
		if strings.HasPrefix(modelID, "kokoro") {
			return "sherpa-onnx"
		}
		return "piper"
	}
	// 2. Native zero-download OS speech synthesis
	if detectNativeTTSBinary() != "" {
		return "native"
	}
	// 3. Visual-only silent broadcast
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

func hasStandalonePiperBinary() bool {
	_, err := exec.LookPath("piper")
	return err == nil
}

func resolveSherpaBinaryInDir(modelsDir string) string {
	if modelsDir != "" {
		exe := "sherpa-onnx-offline-tts"
		if runtime.GOOS == "windows" {
			exe += ".exe"
		}
		bundled := filepath.Join(modelsDir, "_runtime", "bin", exe)
		if fileExistsNonEmpty(bundled) {
			return bundled
		}
	}
	if p, err := exec.LookPath("sherpa-onnx-offline-tts"); err == nil {
		return p
	}
	return ""
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
		if detectNativeTTSBinary() != "" {
			if err := e.speakNative(ctx, text, personaID); err == nil || errors.Is(err, context.Canceled) {
				return err
			}
		}
	case "sherpa-onnx":
		if err := e.speakSherpa(ctx, text, personaID); err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		if detectNativeTTSBinary() != "" {
			if err := e.speakNative(ctx, text, personaID); err == nil || errors.Is(err, context.Canceled) {
				return err
			}
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
	safeID := sanitizeModelID(e.ModelID)
	bundleDir := filepath.Join(modelsDir, safeID)

	tmpWav, err := os.CreateTemp("", "halpradio-dj-*.wav")
	if err != nil {
		return err
	}
	wavPath := tmpWav.Name()
	_ = tmpWav.Close()
	defer os.Remove(wavPath)

	// Prefer the self-contained sherpa-onnx VITS runner when the voice bundle is present
	if sherpaBin := resolveSherpaBinaryInDir(modelsDir); sherpaBin != "" {
		spec, _ := GetVoiceModelSpec(safeID)
		onnxFile := spec.ONNXFile
		if onnxFile == "" {
			onnxFile = safeID + ".onnx"
		}
		vitsModel := filepath.Join(bundleDir, onnxFile)
		vitsTokens := filepath.Join(bundleDir, "tokens.txt")
		vitsDataDir := filepath.Join(bundleDir, "espeak-ng-data")
		if fileExistsNonEmpty(vitsModel) && fileExistsNonEmpty(vitsTokens) {
			cmd := exec.CommandContext(ctx, sherpaBin,
				"--num-threads=4",
				"--vits-model="+vitsModel,
				"--vits-tokens="+vitsTokens,
				"--vits-data-dir="+vitsDataDir,
				"--output-filename="+wavPath,
				text,
			)
			if err := cmd.Run(); err == nil {
				return playAudioFile(ctx, wavPath)
			}
		}
	}

	if hasStandalonePiperBinary() {
		onnxPath, _ := ModelPaths(modelsDir, e.ModelID)
		cmd := exec.CommandContext(ctx, "piper", "--model", onnxPath, "--output_file", wavPath)
		cmd.Stdin = bytes.NewReader([]byte(text))
		if err := cmd.Run(); err != nil {
			return err
		}
		return playAudioFile(ctx, wavPath)
	}
	return errors.New("no ONNX runner available for piper model")
}

func (e *LocalTTSEngine) speakSherpa(ctx context.Context, text string, personaID string) error {
	modelsDir := e.ModelsDir
	if modelsDir == "" {
		modelsDir = util.GetTTSModelsDir()
	}
	sherpaBin := resolveSherpaBinaryInDir(modelsDir)
	if sherpaBin == "" {
		return errors.New("sherpa-onnx-offline-tts not installed")
	}

	safeID := sanitizeModelID(e.ModelID)
	bundleDir := filepath.Join(modelsDir, safeID)
	spec, _ := GetVoiceModelSpec(safeID)
	onnxFile := spec.ONNXFile
	if onnxFile == "" {
		onnxFile = "model.int8.onnx"
	}
	kokoroModel := filepath.Join(bundleDir, onnxFile)
	kokoroVoices := filepath.Join(bundleDir, "voices.bin")
	kokoroTokens := filepath.Join(bundleDir, "tokens.txt")
	kokoroDataDir := filepath.Join(bundleDir, "espeak-ng-data")

	tmpWav, err := os.CreateTemp("", "halpradio-dj-*.wav")
	if err != nil {
		return err
	}
	wavPath := tmpWav.Name()
	_ = tmpWav.Close()
	defer os.Remove(wavPath)

	// Map persona to distinct Kokoro speaker IDs (0=af_bella, 3=af_nicole, 6=am_michael, 9=bm_george)
	sid := "0"
	switch personaID {
	case "retro-80s":
		sid = "6"
	case "cyberpunk":
		sid = "3"
	case "bbc-presenter":
		sid = "9"
	}

	var args []string
	if fileExistsNonEmpty(kokoroVoices) && fileExistsNonEmpty(kokoroTokens) {
		args = []string{
			"--num-threads=4",
			"--kokoro-model=" + kokoroModel,
			"--kokoro-voices=" + kokoroVoices,
			"--kokoro-tokens=" + kokoroTokens,
			"--kokoro-data-dir=" + kokoroDataDir,
			"--sid=" + sid,
			"--output-filename=" + wavPath,
			text,
		}
	} else {
		onnxPath, _ := ModelPaths(modelsDir, e.ModelID)
		args = []string{
			"--num-threads=4",
			"--kokoro-model=" + onnxPath,
			"--output-filename=" + wavPath,
			text,
		}
	}

	cmd := exec.CommandContext(ctx, sherpaBin, args...)
	if err := cmd.Run(); err != nil {
		return err
	}
	return playAudioFile(ctx, wavPath)
}

func (e *LocalTTSEngine) speakNative(ctx context.Context, text string, personaID string) error {
	bin := detectNativeTTSBinary()
	switch bin {
	case "say":
		args := buildMacOSSayArgs(text, personaID, e.ModelID)
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

var (
	macOSVoicesOnce sync.Once
	macOSVoicesMap  map[string]bool
)

func availableMacOSVoices() map[string]bool {
	macOSVoicesOnce.Do(func() {
		macOSVoicesMap = make(map[string]bool)
		if runtime.GOOS != "darwin" {
			return
		}
		out, err := exec.Command("say", "-v", "?").Output()
		if err != nil {
			return
		}
		for _, line := range strings.Split(string(out), "\n") {
			if idx := strings.Index(line, "#"); idx != -1 {
				line = line[:idx]
			}
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			// Voice name precedes the locale code (e.g. "Samantha" or "Reed (English (US))")
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				locale := fields[len(fields)-1]
				name := strings.TrimSpace(strings.TrimSuffix(line, locale))
				if name != "" {
					macOSVoicesMap[name] = true
				}
			}
		}
	})
	return macOSVoicesMap
}

func pickMacOSVoice(candidates ...string) string {
	avail := availableMacOSVoices()
	for _, c := range candidates {
		if avail[c] {
			return c
		}
	}
	return ""
}

func buildMacOSSayArgs(text string, personaID string, modelID ...string) []string {
	selectedModel := ""
	if len(modelID) > 0 {
		selectedModel = strings.TrimSpace(modelID[0])
	}

	// If the user explicitly selected a voice row whose persona differs from personaID,
	// honor the voice row's character when choosing the native fallback voice.
	effectivePersona := personaID
	if spec, ok := GetVoiceModelSpec(selectedModel); ok && spec.PersonaID != "" {
		effectivePersona = spec.PersonaID
	}

	rate := "182"
	var voiceCandidates []string
	switch effectivePersona {
	case "late-night-chill":
		rate = "172"
		voiceCandidates = []string{"Samantha", "Samantha (English (US))", "Flo (English (US))", "Karen"}
	case "retro-80s":
		rate = "194"
		voiceCandidates = []string{"Reed (English (US))", "Rocko (English (US))", "Fred", "Ralph"}
	case "cyberpunk":
		rate = "186"
		voiceCandidates = []string{"Karen", "Tessa", "Moira", "Kathy", "Shelley (English (US))"}
	case "bbc-presenter":
		rate = "178"
		voiceCandidates = []string{"Daniel", "Reed (English (UK))", "Eddy (English (UK))", "Flo (English (UK))"}
	}

	var args []string
	if v := pickMacOSVoice(voiceCandidates...); v != "" {
		args = append(args, "-v", v)
	}
	args = append(args, "-r", rate, "--", text)
	return args
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
