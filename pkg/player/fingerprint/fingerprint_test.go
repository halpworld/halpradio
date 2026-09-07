package fingerprint

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"
)

func TestResultDisplayAndBadges(t *testing.T) {
	res := &Result{
		Artist:     "Daft Punk",
		Title:      "Voyager",
		Album:      "Discovery",
		Year:       2001,
		Confidence: 0.94,
		Source:     "AcoustID",
	}

	if b := res.Badge(); b != "[✨ Identified via AcoustID (94%)]" {
		t.Errorf("Unexpected badge: %q", b)
	}

	if sb := res.ShortBadge(); sb != "[✨ AcoustID 94%]" {
		t.Errorf("Unexpected short badge: %q", sb)
	}

	if fd := res.FullDisplay(); fd != "Daft Punk — \"Voyager\" [Discovery, 2001]" {
		t.Errorf("Unexpected FullDisplay: %q", fd)
	}

	if st := res.SimpleTitle(); st != "Daft Punk - Voyager" {
		t.Errorf("Unexpected SimpleTitle: %q", st)
	}

	bar := res.ConfidenceBar(10)
	if bar != "█████████░ 94%" {
		t.Errorf("Unexpected ConfidenceBar: %q", bar)
	}
}

func TestLRUCache_BasicAndEviction(t *testing.T) {
	cache := NewLRUCache(2, 5*time.Minute)

	res1 := &Result{Artist: "Artist 1", Title: "Track 1"}
	res2 := &Result{Artist: "Artist 2", Title: "Track 2"}
	res3 := &Result{Artist: "Artist 3", Title: "Track 3"}

	cache.Put("k1", res1)
	cache.Put("k2", res2)

	if val, ok := cache.Get("k1"); !ok || val.Artist != "Artist 1" {
		t.Errorf("Expected to retrieve k1")
	}

	// Insert third item, should evict least recently used (which is k2 now, since k1 was accessed)
	cache.Put("k3", res3)

	if _, ok := cache.Get("k2"); ok {
		t.Errorf("Expected k2 to have been evicted")
	}
	if _, ok := cache.Get("k1"); !ok {
		t.Errorf("Expected k1 to remain in cache")
	}
	if _, ok := cache.Get("k3"); !ok {
		t.Errorf("Expected k3 to be in cache")
	}
}

func TestLRUCache_ConcurrentAccess(t *testing.T) {
	cache := NewLRUCache(50, time.Minute)
	var wg sync.WaitGroup

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			r := &Result{Artist: "Artist", Title: "Title"}
			cache.Put("key", r)
			_, _ = cache.Get("key")
		}(i)
	}
	wg.Wait()
}

func TestAcoustIDClient_MockSuccess(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := `{
			"status": "ok",
			"results": [
				{
					"id": "result-123",
					"score": 0.94,
					"recordings": [
						{
							"id": "rec-456",
							"title": "Voyager",
							"artists": [{"name": "Daft Punk"}],
							"releasegroups": [{"id": "rg-789", "title": "Discovery", "type": "Album"}]
						}
					]
				}
			]
		}`
		w.Write([]byte(resp))
	}))
	defer ts.Close()

	client := NewClient("test-key")
	client.AcoustIDURL = ts.URL

	res, err := client.queryAcoustID(context.Background(), "mock-fp", 120, "station-1", "Tokyo FM")
	if err != nil {
		t.Fatalf("Expected successful query, got: %v", err)
	}
	if res.Artist != "Daft Punk" || res.Title != "Voyager" || res.Album != "Discovery" {
		t.Errorf("Unexpected result: %+v", res)
	}
	if res.Confidence != 0.94 {
		t.Errorf("Expected confidence 0.94, got %f", res.Confidence)
	}
}

func TestWriteWAVAndPureGoFingerprint(t *testing.T) {
	tempFile, err := os.CreateTemp("", "test_sample_*.wav")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	tempPath := tempFile.Name()
	_ = tempFile.Close()
	defer os.Remove(tempPath)

	// Generate 1 second of dummy 16-bit PCM at 11025Hz
	sampleCount := 11025
	pcmData := make([]byte, sampleCount*2)
	for i := 0; i < sampleCount; i++ {
		pcmData[i*2] = byte(i & 0xFF)
		pcmData[i*2+1] = byte((i >> 8) & 0xFF)
	}

	err = WriteWAVFile(tempPath, pcmData, 11025, 1)
	if err != nil {
		t.Fatalf("Failed to write WAV file: %v", err)
	}

	fp, dur, err := ComputeFingerprint(context.Background(), tempPath)
	if err != nil {
		t.Fatalf("ComputeFingerprint failed: %v", err)
	}
	if fp == "" {
		t.Errorf("Expected non-empty fingerprint string")
	}
	if dur <= 0 {
		t.Errorf("Expected positive duration, got %f", dur)
	}
}
