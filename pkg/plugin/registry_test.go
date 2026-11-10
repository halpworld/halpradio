package plugin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestRegistryDownloadAndInstallSecurity(t *testing.T) {
	tempHome, err := os.MkdirTemp("", "halpradio-reg-sec-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempHome)

	pluginsBaseDir := filepath.Join(tempHome, "plugins")
	_ = os.MkdirAll(pluginsBaseDir, 0700)

	wasmContent := minimalWasmBytes
	validChecksum := CalculateSHA256(wasmContent)

	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/plugin.wasm" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(wasmContent)
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	client := NewRegistryClient(ts.URL)
	client.httpClient = ts.Client() // the production client refuses loopback

	// 1. Success case with matching SHA-256
	validPlugin := RegistryPlugin{
		ID:          "valid-plugin",
		Name:        "Valid Plugin",
		Version:     "1.0.0",
		DownloadURL: ts.URL + "/plugin.wasm",
		Checksum:    validChecksum,
		Permissions: PermissionsConfig{
			Storage: []string{"local"},
		},
	}

	if err := client.DownloadAndInstall(context.Background(), validPlugin, pluginsBaseDir); err != nil {
		t.Fatalf("DownloadAndInstall failed on valid plugin: %v", err)
	}

	// 2. Reject empty checksum (security requirement)
	noChecksumPlugin := validPlugin
	noChecksumPlugin.ID = "no-checksum"
	noChecksumPlugin.Checksum = ""
	if err := client.DownloadAndInstall(context.Background(), noChecksumPlugin, pluginsBaseDir); err == nil {
		t.Errorf("expected error when downloading plugin without checksum")
	}

	// 3. Reject mismatched / forged checksum
	badChecksumPlugin := validPlugin
	badChecksumPlugin.ID = "bad-checksum"
	badChecksumPlugin.Checksum = "0000000000000000000000000000000000000000000000000000000000000000"
	if err := client.DownloadAndInstall(context.Background(), badChecksumPlugin, pluginsBaseDir); err == nil {
		t.Errorf("expected checksum verification failure for mismatched checksum")
	}

	// 4. Reject directory traversal plugin ID
	traversalPlugin := validPlugin
	traversalPlugin.ID = "../escape"
	if err := client.DownloadAndInstall(context.Background(), traversalPlugin, pluginsBaseDir); err == nil {
		t.Errorf("expected error for directory traversal plugin ID")
	}
}

func TestFetchRegistry(t *testing.T) {
	regData := []byte(`{
		"version": "1.0.0",
		"plugins": [
			{
				"id": "discord-rpc",
				"name": "Discord Rich Presence",
				"version": "1.0.0",
				"author": "halpradio",
				"description": "Show status on Discord",
				"download_url": "https://example.com/discord.wasm",
				"checksum": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
			}
		]
	}`)

	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(regData)
	}))
	defer ts.Close()

	client := NewRegistryClient(ts.URL)
	client.httpClient = ts.Client()
	index, err := client.FetchRegistry(context.Background())
	if err != nil {
		t.Fatalf("FetchRegistry failed: %v", err)
	}
	if len(index.Plugins) != 1 || index.Plugins[0].ID != "discord-rpc" {
		t.Errorf("unexpected registry index: %+v", index)
	}

	// A failing registry is an error: there is no built-in fallback list.
	badServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer badServer.Close()

	badClient := NewRegistryClient(badServer.URL)
	badClient.httpClient = badServer.Client()
	fallbackIndex, err := badClient.FetchRegistry(context.Background())
	if err == nil {
		t.Errorf("expected error when registry returns 500")
	}
	if fallbackIndex != nil {
		t.Errorf("expected no index on failure, got %+v", fallbackIndex)
	}
}
