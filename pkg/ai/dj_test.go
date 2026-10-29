package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDJPersonasAndOfflineTemplates(t *testing.T) {
	personas := DefaultPersonas()
	if len(personas) != 4 {
		t.Fatalf("expected 4 personas, got %d", len(personas))
	}

	dj := NewDJ(DJConfig{
		PersonaID: "late-night-chill",
		TTSEngine: "silent",
		DuckLevel: 20,
	})

	if dj.Persona().ID != "late-night-chill" {
		t.Errorf("expected late-night-chill, got %s", dj.Persona().ID)
	}
	if dj.DuckRatio() != 0.20 {
		t.Errorf("expected DuckRatio 0.20, got %f", dj.DuckRatio())
	}

	tc := TrackContext{
		StationName: "SomaFM Groove Salad",
		Genre:       "Ambient / Downtempo",
		Artist:      "Tycho",
		Title:       "A Walk",
		Album:       "Dive",
		ReleaseYear: "2011",
		OriginCity:  "San Francisco",
	}

	script, label, err := dj.GenerateIntro(context.Background(), tc)
	if err != nil {
		t.Fatalf("GenerateIntro failed: %v", err)
	}
	if !strings.Contains(label, "Visual Broadcast") {
		t.Errorf("expected Visual Broadcast engine label, got %q", label)
	}
	expected := "You're locked into halpradio on SomaFM Groove Salad. Up next is Tycho with 'A Walk', from the 2011 album Dive out of San Francisco. Sit back and enjoy the flow."
	if script != expected {
		t.Errorf("script mismatch:\n got: %q\nwant: %q", script, expected)
	}

	// Cycle through all personas and verify distinct scripts
	for _, id := range []string{"retro-80s", "cyberpunk", "bbc-presenter"} {
		dj.SetPersona(id)
		s, _, err := dj.GenerateIntro(context.Background(), tc)
		if err != nil {
			t.Fatalf("GenerateIntro(%s) failed: %v", id, err)
		}
		if !strings.Contains(s, "Tycho") || !strings.Contains(s, "A Walk") {
			t.Errorf("persona %s script missing artist/title: %q", id, s)
		}
	}

	// Test NextPersonaID cycle
	if next := NextPersonaID("late-night-chill"); next != "retro-80s" {
		t.Errorf("expected retro-80s after late-night-chill, got %s", next)
	}
	if next := NextPersonaID("bbc-presenter"); next != "late-night-chill" {
		t.Errorf("expected wrap around to late-night-chill, got %s", next)
	}
}

func TestDJLocalOllamaAndFallback(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/generate" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ollamaGenerateResponse{
			Response: `"Locked into Night City FM on halpradio. Here is Perturbator with 'Future Club' from Dangerous Days."`,
		})
	}))
	defer ts.Close()

	dj := NewDJ(DJConfig{
		PersonaID:   "cyberpunk",
		TTSEngine:   "silent",
		OllamaURL:   ts.URL,
		OllamaModel: "llama3.2:1b",
		HTTPClient:  ts.Client(),
	})

	tc := TrackContext{
		StationName: "Nightride FM",
		Artist:      "Perturbator",
		Title:       "Future Club",
		Album:       "Dangerous Days",
	}

	script, _, err := dj.GenerateIntro(context.Background(), tc)
	if err != nil {
		t.Fatalf("GenerateIntro with Ollama failed: %v", err)
	}
	if !strings.Contains(script, "Perturbator") || strings.HasPrefix(script, `"`) {
		t.Errorf("unexpected sanitized Ollama script: %q", script)
	}

	// Shut down test server and verify seamless fallback to offline templates
	ts.Close()
	fallbackScript, _, err := dj.GenerateIntro(context.Background(), tc)
	if err != nil {
		t.Fatalf("GenerateIntro fallback failed: %v", err)
	}
	if !strings.Contains(fallbackScript, "Perturbator with 'Future Club'") {
		t.Errorf("expected offline fallback script to contain artist and title, got %q", fallbackScript)
	}
}

func TestDJSpeakAndCancelLifecycle(t *testing.T) {
	eng := &LocalTTSEngine{
		Preferred:     "silent",
		ModelsDir:     t.TempDir(),
		MinSilentWait: 500 * time.Millisecond,
	}
	dj := NewDJ(DJConfig{
		PersonaID: "late-night-chill",
		Engine:    eng,
	})

	done := make(chan error, 1)
	go func() {
		done <- dj.Speak(context.Background(), "Testing live DJ speech lifecycle.")
	}()

	// Wait briefly for Speak to enter active state
	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		if dj.IsSpeaking() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !dj.IsSpeaking() {
		t.Errorf("expected IsSpeaking() true while speech is in flight")
	}

	dj.Cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("expected nil error on cancelled DJ speech, got %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for Speak to return after Cancel()")
	}

	if dj.IsSpeaking() {
		t.Errorf("expected IsSpeaking() false after Cancel()")
	}
}

func TestDJConcurrentSpeakSupersedesPreviousCall(t *testing.T) {
	eng := &LocalTTSEngine{
		Preferred:     "silent",
		ModelsDir:     t.TempDir(),
		MinSilentWait: 250 * time.Millisecond,
	}
	dj := NewDJ(DJConfig{
		PersonaID: "late-night-chill",
		Engine:    eng,
	})

	errCh1 := make(chan error, 1)
	go func() {
		errCh1 <- dj.Speak(context.Background(), "First announcement that will be superseded.")
	}()

	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		if dj.IsSpeaking() {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Start a second Speak call while the first is in flight
	errCh2 := make(chan error, 1)
	go func() {
		errCh2 <- dj.Speak(context.Background(), "Second announcement.")
	}()

	// First call should return quickly without clearing the second call's speaking state
	select {
	case err := <-errCh1:
		if err != nil {
			t.Errorf("expected nil error for superseded Speak #1, got %v", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for superseded Speak #1 to return")
	}

	// Give the first goroutine's defer a moment to run, then verify second call is still speaking
	time.Sleep(20 * time.Millisecond)
	if !dj.IsSpeaking() {
		t.Errorf("expected IsSpeaking() to remain true while second Speak call is still active")
	}

	dj.Cancel()
	<-errCh2
}
