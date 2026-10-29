package ai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestVoiceModelCatalogAndSpecs(t *testing.T) {
	models := DefaultVoiceModels()
	if len(models) < 5 {
		t.Fatalf("expected at least 5 voice models in catalog, got %d", len(models))
	}

	spec, ok := GetVoiceModelSpec("en_US-lessac-medium")
	if !ok {
		t.Fatalf("expected en_US-lessac-medium in catalog")
	}
	if spec.Engine != "piper" || spec.PersonaID != "late-night-chill" {
		t.Errorf("unexpected spec metadata: %+v", spec)
	}

	if got := VoiceModelForPersona("bbc-presenter"); got != "en_GB-alan-medium" {
		t.Errorf("expected en_GB-alan-medium for bbc-presenter, got %q", got)
	}
	if got := VoiceModelForPersona("unknown"); got != "en_US-lessac-medium" {
		t.Errorf("expected default en_US-lessac-medium, got %q", got)
	}
}

func TestDownloadVoiceModelToDirAndIsModelInstalled(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".json") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"audio":{"sample_rate":22050}}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("FAKE-ONNX-WEIGHTS-BINARY-PAYLOAD"))
	}))
	defer ts.Close()

	// Temporarily override catalog URLs for test model
	orig := voiceModelCatalog[0]
	voiceModelCatalog[0].ONNXURL = ts.URL + "/model.onnx"
	voiceModelCatalog[0].ConfigURL = ts.URL + "/model.onnx.json"
	defer func() { voiceModelCatalog[0] = orig }()

	tempDir := t.TempDir()
	modelsDir := tempDir + "/models"

	if IsModelInstalledInDir(modelsDir, "en_US-lessac-medium") {
		t.Fatalf("expected model NOT installed before download")
	}

	var progressCalls int
	err := DownloadVoiceModelToDir(context.Background(), ts.Client(), modelsDir, "en_US-lessac-medium", func(downloaded, total int64) {
		progressCalls++
	})
	if err != nil {
		t.Fatalf("DownloadVoiceModelToDir failed: %v", err)
	}
	if progressCalls == 0 {
		t.Errorf("expected progressFn to be invoked at least once")
	}
	if !IsModelInstalledInDir(modelsDir, "en_US-lessac-medium") {
		t.Errorf("expected IsModelInstalledInDir true after download")
	}

	onnxPath, cfgPath := ModelPaths(modelsDir, "en_US-lessac-medium")
	if _, err := os.Stat(onnxPath); err != nil {
		t.Errorf("expected onnx file at %s: %v", onnxPath, err)
	}
	if _, err := os.Stat(cfgPath); err != nil {
		t.Errorf("expected config file at %s: %v", cfgPath, err)
	}
}

func TestLocalTTSEngineSilentAndCancellation(t *testing.T) {
	eng := &LocalTTSEngine{
		Preferred:     "silent",
		ModelsDir:     t.TempDir(),
		ModelID:       "en_US-lessac-medium",
		MinSilentWait: 30 * time.Millisecond,
	}

	if eng.Name() != "silent" {
		t.Errorf("expected engine Name 'silent', got %q", eng.Name())
	}
	if !strings.Contains(eng.Label(), "Visual Broadcast") {
		t.Errorf("expected Visual Broadcast label, got %q", eng.Label())
	}

	// Short silent speech
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err := eng.Speak(ctx, "Welcome to halpradio.", "late-night-chill"); err != nil && err != context.DeadlineExceeded {
		t.Errorf("unexpected Speak error: %v", err)
	}

	// Immediate cancellation
	cancelCtx, cancelNow := context.WithCancel(context.Background())
	cancelNow()
	if err := eng.Speak(cancelCtx, "Long intro that gets cancelled right away.", "late-night-chill"); err == nil {
		t.Errorf("expected canceled error when context is already canceled")
	}
}
