package ai

import (
	"archive/tar"
	"bytes"
	"compress/bzip2"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
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

// artifactPin is the expected SHA-256 digest and byte size of a downloadable
// archive. Digests are compiled into the binary; nothing that is not listed
// in pinnedArtifacts is ever downloaded.
type artifactPin struct {
	SHA256 string
	Size   int64
}

// pinnedArtifacts maps every download URL the AI DJ may fetch to its pinned
// digest. The values were taken from the GitHub release API "digest" field
// of k2-fsa/sherpa-onnx (tags v1.13.8 and tts-models). Assets on the rolling
// "tts-models" tag can be replaced upstream; when that happens the digest no
// longer matches and the install is refused until this table is updated.
var pinnedArtifacts = map[string]artifactPin{
	// sherpa-onnx v1.13.8 runtime (native executable + shared libraries).
	"https://github.com/k2-fsa/sherpa-onnx/releases/download/v1.13.8/sherpa-onnx-v1.13.8-osx-arm64-shared.tar.bz2":            {"b10e5c7e2c30ea03de9c442655d14860d9edc475c6251d58a8f5f06e913a1d56", 20314448},
	"https://github.com/k2-fsa/sherpa-onnx/releases/download/v1.13.8/sherpa-onnx-v1.13.8-osx-x64-shared.tar.bz2":              {"54aad64acee9d2d596535a6080d6f22602a720af5460e1d50461b1e1b06bee40", 22846038},
	"https://github.com/k2-fsa/sherpa-onnx/releases/download/v1.13.8/sherpa-onnx-v1.13.8-linux-x64-shared.tar.bz2":            {"c0bdb7907d3a74bba1d55d22bf4d9fa75586cf1530614ebe88a27b9118e015c4", 28156791},
	"https://github.com/k2-fsa/sherpa-onnx/releases/download/v1.13.8/sherpa-onnx-v1.13.8-linux-aarch64-shared-cpu.tar.bz2":    {"4e3734f82bc1379fd91f219f5869c7e9d03b7a4f7561907d8abca4849c51a789", 28091778},
	"https://github.com/k2-fsa/sherpa-onnx/releases/download/v1.13.8/sherpa-onnx-v1.13.8-win-x64-shared-MT-Release.tar.bz2":   {"6dffdc715a4465b989446a6105265d2cb345e7101591a17d35534b6758f6e8df", 24805859},
	"https://github.com/k2-fsa/sherpa-onnx/releases/download/v1.13.8/sherpa-onnx-v1.13.8-win-arm64-shared-MT-Release.tar.bz2": {"455ea18dc44aea188b4f7976037dea3d77e3f8d7afe0760f34244a7a0db53c4c", 23341583},
	// Voice bundles (rolling "tts-models" tag; asset updated_at in comments).
	"https://github.com/k2-fsa/sherpa-onnx/releases/download/tts-models/vits-piper-en_US-lessac-medium-int8.tar.bz2": {"f1c6d0295cf16087b05f80fdca5b44daca5cd78e2c425d419a42ba34929805f9", 20969179},  // 2025-12-02
	"https://github.com/k2-fsa/sherpa-onnx/releases/download/tts-models/vits-piper-en_US-ryan-medium-int8.tar.bz2":   {"376eb489d42e98cb49f0e13a633e8d88580bd247ed8aacc298a733210404f771", 21083446},  // 2025-12-02
	"https://github.com/k2-fsa/sherpa-onnx/releases/download/tts-models/vits-piper-en_US-amy-medium-int8.tar.bz2":    {"bd23c0aa629eb3719448582f45ede49e8fa6a679061fed5eab16a6a6fd8e7e82", 21028122},  // 2026-04-22
	"https://github.com/k2-fsa/sherpa-onnx/releases/download/tts-models/vits-piper-en_GB-alan-medium-int8.tar.bz2":   {"f0b2526c9600657141ccbbe412f6159dceedab5bab4ba4e8857d9443acb50de4", 21103831},  // 2025-06-30
	"https://github.com/k2-fsa/sherpa-onnx/releases/download/tts-models/kokoro-int8-en-v0_19.tar.bz2":                {"c9f0dd393615805b0bab050c340834d5e684e732aec91c0e860cd30e982c08bd", 103248205}, // 2025-08-10
}

// allowedDownloadHosts are the only hosts a model/runtime download (or any
// redirect it follows) may reach. GitHub release assets redirect from
// github.com to one of the githubusercontent.com hosts.
var allowedDownloadHosts = map[string]bool{
	"github.com":                           true,
	"objects.githubusercontent.com":        true,
	"release-assets.githubusercontent.com": true,
}

// isAllowedDownloadURL reports whether u may be requested. It is a variable so
// tests can point downloads at an httptest server.
var isAllowedDownloadURL = func(u *url.URL) bool {
	return u != nil && u.Scheme == "https" && u.User == nil && allowedDownloadHosts[strings.ToLower(u.Hostname())] &&
		(u.Port() == "" || u.Port() == "443")
}

const (
	maxDownloadRedirects = 5

	// Extraction budgets (AI-5). The runtime archive unpacks to well under
	// 100 MB of kept files; voice bundles carry espeak-ng-data (hundreds of
	// small files) plus one model of up to ~110 MB (Kokoro).
	maxRuntimeExtractBytes = 400 * 1024 * 1024
	maxVoiceExtractBytes   = 300 * 1024 * 1024
	maxArchiveEntries      = 4000

	runtimeManifestName = ".halpradio-manifest.json"
)

var errUnpinnedArtifact = errors.New("download refused: no pinned SHA-256 digest for this asset")

// sherpaRuntimeArchiveURL returns the prebuilt sherpa-onnx runtime archive URL for the current OS and architecture.
func sherpaRuntimeArchiveURL() (url string, topDir string, ok bool) {
	return sherpaRuntimeArchiveURLFor(runtime.GOOS, runtime.GOARCH)
}

func sherpaRuntimeArchiveURLFor(goos, goarch string) (string, string, bool) {
	const base = "https://github.com/k2-fsa/sherpa-onnx/releases/download/v1.13.8/"
	var top string
	switch goos + "/" + goarch {
	case "darwin/arm64":
		top = "sherpa-onnx-v1.13.8-osx-arm64-shared"
	case "darwin/amd64":
		top = "sherpa-onnx-v1.13.8-osx-x64-shared"
	case "linux/amd64":
		top = "sherpa-onnx-v1.13.8-linux-x64-shared"
	case "linux/arm64":
		top = "sherpa-onnx-v1.13.8-linux-aarch64-shared-cpu"
	case "windows/amd64":
		top = "sherpa-onnx-v1.13.8-win-x64-shared-MT-Release"
	case "windows/arm64":
		top = "sherpa-onnx-v1.13.8-win-arm64-shared-MT-Release"
	default:
		return "", "", false
	}
	return base + top + ".tar.bz2", top, true
}

// DownloadVoiceModel downloads an opt-in ONNX voice model (and the lightweight
// local sherpa-onnx TTS runner if not already present) into util.GetTTSModelsDir().
// This is only invoked on explicit user action.
func DownloadVoiceModel(ctx context.Context, modelID string, progressFn func(downloaded, total int64)) error {
	client := &http.Client{Timeout: 5 * time.Minute}
	return DownloadVoiceModelToDir(ctx, client, util.GetTTSModelsDir(), modelID, progressFn)
}

// DownloadVoiceModelToDir downloads a voice model into modelsDir.
//
// Every archive is checked against its pinned SHA-256 digest before anything
// is extracted; an unpinned or mismatching download is refused and leaves
// nothing behind. modelsDir must be absolute.
func DownloadVoiceModelToDir(ctx context.Context, client *http.Client, modelsDir, modelID string, progressFn func(downloaded, total int64)) error {
	spec, ok := GetVoiceModelSpec(modelID)
	if !ok {
		return fmt.Errorf("unknown voice model %q", modelID)
	}
	if modelsDir == "" {
		return errors.New("models directory not configured")
	}
	if !filepath.IsAbs(modelsDir) {
		return errors.New("models directory must be an absolute path")
	}
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Minute}
	}
	if err := os.MkdirAll(modelsDir, 0o700); err != nil {
		return fmt.Errorf("create models dir: %w", err)
	}
	_ = os.Chmod(modelsDir, 0o700)

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

	// Ensure the local sherpa-onnx TTS runtime binary is installed alongside
	// modelsDir. A bundled runtime that fails its manifest check (tampered,
	// or installed by an older version without a manifest) is reinstalled.
	if !hasStandalonePiperBinary() {
		bin := resolveSherpaBinaryInDir(modelsDir)
		if bin == "" || (bin == bundledSherpaPath(modelsDir) && verifyBundledRuntime(modelsDir) != nil) {
			if err := ensureSherpaRuntimeInDir(ctx, client, modelsDir); err != nil {
				return fmt.Errorf("install local ONNX TTS runtime: %w", err)
			}
		}
	}

	// Download and extract the self-contained ONNX voice bundle (.tar.bz2)
	targetDir := filepath.Join(modelsDir, sanitizeModelID(spec.ID))
	policy := extractPolicy{
		stripPrefix: spec.BundleDir,
		maxTotal:    maxVoiceExtractBytes,
		maxEntries:  maxArchiveEntries,
		maxFile:     maxModelFileBytes,
		required:    []string{spec.ONNXFile, "tokens.txt"},
	}
	if err := downloadAndExtractTarBz2(ctx, client, spec.BundleURL, targetDir, policy, progressFn); err != nil {
		return fmt.Errorf("download voice bundle %s: %w", spec.ID, err)
	}
	return nil
}

func sherpaExeName() string {
	if runtime.GOOS == "windows" {
		return "sherpa-onnx-offline-tts.exe"
	}
	return "sherpa-onnx-offline-tts"
}

func bundledSherpaPath(modelsDir string) string {
	return filepath.Join(modelsDir, "_runtime", "bin", sherpaExeName())
}

// runtimeExtractPolicy returns the extraction rules for the runtime archive:
// keep the TTS executable and the shared libraries, and give the exec bit to
// the executable only. On Windows the DLLs are placed next to the .exe so the
// loader resolves them from the runtime directory (AI-9).
func runtimeExtractPolicy(goos, topDir string) extractPolicy {
	exe := "bin/sherpa-onnx-offline-tts"
	if goos == "windows" {
		exe += ".exe"
	}
	isDLL := func(rel string) bool { return strings.HasSuffix(strings.ToLower(rel), ".dll") }
	return extractPolicy{
		stripPrefix: topDir,
		maxTotal:    maxRuntimeExtractBytes,
		maxEntries:  maxArchiveEntries,
		maxFile:     maxModelFileBytes,
		keep: func(rel string) bool {
			if rel == exe || strings.HasPrefix(rel, "lib/") {
				return true
			}
			return goos == "windows" && strings.HasPrefix(rel, "bin/") && !strings.Contains(strings.TrimPrefix(rel, "bin/"), "/") && isDLL(rel)
		},
		mapPath: func(rel string) string {
			if goos == "windows" && strings.HasPrefix(rel, "lib/") && isDLL(rel) && !strings.Contains(strings.TrimPrefix(rel, "lib/"), "/") {
				return "bin/" + strings.TrimPrefix(rel, "lib/")
			}
			return rel
		},
		executable: func(rel string) bool { return rel == exe },
		required:   []string{exe},
		manifest:   true,
	}
}

func ensureSherpaRuntimeInDir(ctx context.Context, client *http.Client, modelsDir string) error {
	archiveURL, topDir, ok := sherpaRuntimeArchiveURL()
	if !ok {
		return errors.New("no prebuilt ONNX runtime available for this OS/architecture")
	}
	runtimeDir := filepath.Join(modelsDir, "_runtime")
	if err := downloadAndExtractTarBz2(ctx, client, archiveURL, runtimeDir, runtimeExtractPolicy(runtime.GOOS, topDir), nil); err != nil {
		return err
	}
	return verifyBundledRuntime(modelsDir)
}

// extractPolicy bounds and shapes what downloadAndExtractTarBz2 writes.
type extractPolicy struct {
	stripPrefix string                  // required top-level directory; entries outside it are skipped
	keep        func(rel string) bool   // nil keeps everything
	mapPath     func(rel string) string // optional rename of kept entries
	executable  func(rel string) bool   // files that get mode 0700; nil means none
	maxTotal    int64                   // cumulative bytes written
	maxEntries  int                     // tar headers read (all types)
	maxFile     int64                   // per-file cap; larger entries abort extraction
	required    []string                // relative paths that must exist afterwards
	manifest    bool                    // write runtimeManifestName with per-file digests
}

// runtimeManifest records what was installed from a pinned runtime archive so
// the files can be re-verified before every exec.
type runtimeManifest struct {
	Source        string            `json:"source"`
	ArchiveSHA256 string            `json:"archive_sha256"`
	Files         map[string]string `json:"files"`
}

func downloadAndExtractTarBz2(
	ctx context.Context,
	client *http.Client,
	rawURL string,
	destDir string,
	policy extractPolicy,
	progressFn func(downloaded, total int64),
) error {
	pin, ok := lookupPin(rawURL)
	if !ok {
		return errUnpinnedArtifact
	}
	if strings.Trim(policy.stripPrefix, "/") == "" {
		return errors.New("archive extraction requires a top-level directory")
	}

	tmpArchive, err := os.CreateTemp(filepath.Dir(destDir), "onnx-bundle-*.tar.bz2")
	if err != nil {
		return err
	}
	tmpArchivePath := tmpArchive.Name()
	_ = tmpArchive.Close()
	defer os.Remove(tmpArchivePath)

	// downloadFileAtomic verifies the pinned digest over the downloaded bytes;
	// nothing below runs unless it matched.
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
	fail := func(err error) error {
		_ = os.RemoveAll(stagingDir)
		return err
	}

	tr := tar.NewReader(bzip2.NewReader(f))
	prefix := strings.Trim(policy.stripPrefix, "/") + "/"
	var written int64
	entries := 0
	digests := make(map[string]string)

	for {
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fail(err)
		}
		entries++
		if policy.maxEntries > 0 && entries > policy.maxEntries {
			return fail(fmt.Errorf("archive has more than %d entries", policy.maxEntries))
		}

		name := filepath.ToSlash(strings.TrimPrefix(hdr.Name, "./"))
		if !strings.HasPrefix(name, prefix) {
			continue // outside the required top-level directory (includes absolute paths)
		}
		name = strings.TrimPrefix(name, prefix)
		if name == "" || !isCleanRelPath(name) {
			continue
		}
		if policy.keep != nil && !policy.keep(name) {
			continue
		}
		if policy.mapPath != nil {
			name = policy.mapPath(name)
			if !isCleanRelPath(name) {
				continue
			}
		}

		outPath := filepath.Join(stagingDir, filepath.FromSlash(name))
		if !strings.HasPrefix(filepath.Clean(outPath)+string(os.PathSeparator), filepath.Clean(stagingDir)+string(os.PathSeparator)) {
			continue
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(outPath, 0o700); err != nil {
				return fail(err)
			}
		case tar.TypeReg, tar.TypeRegA:
			if policy.maxFile > 0 && hdr.Size > policy.maxFile {
				return fail(fmt.Errorf("archive entry %q exceeds %d bytes", name, policy.maxFile))
			}
			if err := os.MkdirAll(filepath.Dir(outPath), 0o700); err != nil {
				return fail(err)
			}
			mode := os.FileMode(0o600)
			if policy.executable != nil && policy.executable(name) {
				mode = 0o700
			}
			_ = os.Remove(outPath)
			outFile, err := os.OpenFile(outPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
			if err != nil {
				return fail(err)
			}
			h := sha256.New()
			limit := policy.maxFile
			if limit <= 0 {
				limit = maxModelFileBytes
			}
			n, copyErr := io.Copy(io.MultiWriter(outFile, h), io.LimitReader(tr, limit+1))
			closeErr := outFile.Close()
			if copyErr != nil {
				return fail(copyErr)
			}
			if closeErr != nil {
				return fail(closeErr)
			}
			if n > limit {
				return fail(fmt.Errorf("archive entry %q exceeds %d bytes", name, limit))
			}
			written += n
			if policy.maxTotal > 0 && written > policy.maxTotal {
				return fail(fmt.Errorf("archive expands beyond %d bytes", policy.maxTotal))
			}
			if err := os.Chmod(outPath, mode); err != nil && runtime.GOOS != "windows" {
				return fail(err)
			}
			digests[name] = hex.EncodeToString(h.Sum(nil))
		default:
			// Symlinks, hard links, devices and FIFOs are never created.
		}
	}

	for _, req := range policy.required {
		if !fileExistsNonEmpty(filepath.Join(stagingDir, filepath.FromSlash(req))) {
			return fail(fmt.Errorf("archive is missing required file %q", req))
		}
	}

	if policy.manifest {
		data, err := json.MarshalIndent(runtimeManifest{Source: rawURL, ArchiveSHA256: pin.SHA256, Files: digests}, "", "  ")
		if err != nil {
			return fail(err)
		}
		if err := util.WriteFileAtomic(filepath.Join(stagingDir, runtimeManifestName), data, 0o600); err != nil {
			return fail(err)
		}
	}

	_ = os.RemoveAll(destDir)
	if err := os.Rename(stagingDir, destDir); err != nil {
		return fail(err)
	}
	return nil
}

// isCleanRelPath accepts slash-separated relative paths with no empty, "."
// or ".." segments.
func isCleanRelPath(p string) bool {
	if p == "" || strings.HasPrefix(p, "/") || strings.Contains(p, "\\") || strings.Contains(p, ":") {
		return false
	}
	for _, seg := range strings.Split(strings.TrimSuffix(p, "/"), "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return true
}

func lookupPin(rawURL string) (artifactPin, bool) {
	pin, ok := pinnedArtifacts[rawURL]
	if !ok || len(pin.SHA256) != sha256.Size*2 {
		return artifactPin{}, false
	}
	return pin, true
}

// downloadClient returns a shallow copy of client whose redirects must stay on
// the download allow-list.
func downloadClient(client *http.Client) *http.Client {
	cl := *client
	cl.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxDownloadRedirects {
			return errors.New("too many redirects")
		}
		if !isAllowedDownloadURL(req.URL) {
			return fmt.Errorf("redirect to %s refused", req.URL.Redacted())
		}
		return nil
	}
	return &cl
}

// downloadFileAtomic fetches a pinned artifact into destPath. The URL (and any
// redirect) must be on the allow-list, and the SHA-256 and size of the bytes
// received must equal the pin before destPath is created.
func downloadFileAtomic(ctx context.Context, client *http.Client, rawURL, destPath string, progressFn func(downloaded, total int64)) error {
	pin, ok := lookupPin(rawURL)
	if !ok {
		return errUnpinnedArtifact
	}
	u, err := url.Parse(rawURL)
	if err != nil || !isAllowedDownloadURL(u) {
		return fmt.Errorf("download from %s refused: host not allowed", rawURL)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", defaultUserAgent)

	resp, err := downloadClient(client).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d from %s", resp.StatusCode, rawURL)
	}

	limit := int64(maxModelFileBytes)
	if pin.Size > 0 && pin.Size < limit {
		limit = pin.Size
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
	if pin.Size > 0 {
		total = pin.Size
	}
	var downloaded int64
	buf := make([]byte, 32*1024)
	limited := io.LimitReader(resp.Body, limit+1)
	h := sha256.New()

	for {
		if err := ctx.Err(); err != nil {
			cleanup()
			return err
		}

		n, readErr := limited.Read(buf)
		if n > 0 {
			downloaded += int64(n)
			if downloaded > limit {
				cleanup()
				return errors.New("download exceeds its pinned size")
			}
			h.Write(buf[:n])
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

	if (pin.Size > 0 && downloaded != pin.Size) || !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), pin.SHA256) {
		cleanup()
		return fmt.Errorf("integrity check failed for %s: SHA-256 does not match the pinned digest", rawURL)
	}

	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	_ = os.Chmod(tmpName, 0o600)
	if err := os.Rename(tmpName, destPath); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

// readRuntimeManifest loads the manifest of a bundled runtime and checks that
// it belongs to the pinned runtime archive for this platform.
func readRuntimeManifest(modelsDir string) (runtimeManifest, error) {
	var m runtimeManifest
	if !filepath.IsAbs(modelsDir) {
		return m, errors.New("models directory is not absolute")
	}
	path := filepath.Join(modelsDir, "_runtime", runtimeManifestName)
	info, err := os.Lstat(path)
	if err != nil {
		return m, err
	}
	if !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return m, errors.New("runtime manifest is not a regular file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return m, err
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return m, fmt.Errorf("runtime manifest: %w", err)
	}
	archiveURL, _, ok := sherpaRuntimeArchiveURL()
	pin, pinned := lookupPin(archiveURL)
	if !ok || !pinned || m.Source != archiveURL || !strings.EqualFold(m.ArchiveSHA256, pin.SHA256) {
		return m, errors.New("runtime manifest does not match the pinned runtime")
	}
	if _, listed := m.Files["bin/"+sherpaExeName()]; !listed {
		return m, errors.New("runtime manifest does not list the TTS executable")
	}
	return m, nil
}

// verifyBundledRuntime re-hashes every file listed in the runtime manifest and
// fails if any is missing, is not a regular file, or has changed. It runs
// before every exec of the bundled runtime.
func verifyBundledRuntime(modelsDir string) error {
	m, err := readRuntimeManifest(modelsDir)
	if err != nil {
		return err
	}
	runtimeDir := filepath.Join(modelsDir, "_runtime")
	for rel, want := range m.Files {
		if !isCleanRelPath(rel) {
			return fmt.Errorf("runtime manifest has invalid path %q", rel)
		}
		p := filepath.Join(runtimeDir, filepath.FromSlash(rel))
		info, err := os.Lstat(p)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("runtime file %s is not a regular file", rel)
		}
		got, err := sha256File(p)
		if err != nil {
			return err
		}
		if !strings.EqualFold(got, want) {
			return fmt.Errorf("runtime file %s does not match its install manifest", rel)
		}
	}
	return nil
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
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

// resolveSherpaBinaryInDir returns the sherpa-onnx TTS executable to use: the
// bundled runtime under an absolute modelsDir when its install manifest
// lists it (the full digest check runs in verifiedSherpaBinary right before
// exec), otherwise one installed on PATH.
func resolveSherpaBinaryInDir(modelsDir string) string {
	if modelsDir != "" && filepath.IsAbs(modelsDir) {
		bundled := bundledSherpaPath(modelsDir)
		if fileExistsNonEmpty(bundled) {
			if _, err := readRuntimeManifest(modelsDir); err == nil {
				return bundled
			}
		}
	}
	if p, err := exec.LookPath("sherpa-onnx-offline-tts"); err == nil && filepath.IsAbs(p) {
		return p
	}
	return ""
}

// verifiedSherpaBinary resolves the sherpa-onnx executable and, when it is the
// bundled runtime, re-verifies every installed runtime file against the
// manifest before it may be executed.
func verifiedSherpaBinary(modelsDir string) (string, error) {
	bin := resolveSherpaBinaryInDir(modelsDir)
	if bin == "" {
		return "", errors.New("sherpa-onnx-offline-tts not installed")
	}
	if modelsDir != "" && bin == bundledSherpaPath(modelsDir) {
		if err := verifyBundledRuntime(modelsDir); err != nil {
			return "", fmt.Errorf("bundled TTS runtime failed verification: %w", err)
		}
	}
	return bin, nil
}

// maxSpokenRunes caps the text handed to any TTS process.
const maxSpokenRunes = 600

// sanitizeSpokenText makes DJ text safe to pass as a single argv element:
// control and format characters are removed, length is capped, and leading
// dashes (ASCII and Unicode) are stripped so the text can never be parsed as
// a command-line option by the TTS runner.
func sanitizeSpokenText(text string) string {
	text = util.SanitizeDisplay(text, maxSpokenRunes)
	return strings.TrimSpace(strings.TrimLeft(text, leadingDashCutset))
}

// leadingDashCutset holds ASCII hyphen-minus plus Unicode dash and minus
// characters, and whitespace, all trimmed from the start of spoken text.
const leadingDashCutset = "-\u2010\u2011\u2012\u2013\u2014\u2015\u2212\ufe58\ufe63\uff0d \t"

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
	}
	// Windows has no native speech backend wired up (there is no SAPI
	// branch in speakNative), so it falls back to the visual broadcast.
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
	text = sanitizeSpokenText(text)
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
	if sherpaBin, verr := verifiedSherpaBinary(modelsDir); verr == nil {
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
			cmd.Dir = filepath.Dir(sherpaBin)
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
	sherpaBin, err := verifiedSherpaBinary(modelsDir)
	if err != nil {
		return err
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
	cmd.Dir = filepath.Dir(sherpaBin)
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
