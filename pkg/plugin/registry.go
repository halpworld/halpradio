package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	DefaultRegistryURL = "https://raw.githubusercontent.com/halpworld/halpradio-plugins/main/registry.json"
)

// RegistryClient interacts with the remote official plugin registry.
type RegistryClient struct {
	httpClient  *http.Client
	registryURL string
}

// NewRegistryClient creates a new registry client instance.
func NewRegistryClient(registryURL string) *RegistryClient {
	if registryURL == "" {
		registryURL = DefaultRegistryURL
	}
	return &RegistryClient{
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		registryURL: registryURL,
	}
}

// FetchRegistry downloads and decodes the latest registry index.
func (c *RegistryClient) FetchRegistry(ctx context.Context) (*RegistryIndex, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.registryURL, nil)
	if err != nil {
		return c.fallbackRegistry(), err
	}
	req.Header.Set("User-Agent", "halpradio/plugin-manager")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return c.fallbackRegistry(), fmt.Errorf("failed to fetch plugin registry: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.fallbackRegistry(), fmt.Errorf("registry server returned HTTP %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return c.fallbackRegistry(), fmt.Errorf("failed to read registry response: %w", err)
	}

	var index RegistryIndex
	if err := json.Unmarshal(data, &index); err != nil {
		return c.fallbackRegistry(), fmt.Errorf("failed to parse registry JSON: %w", err)
	}

	return &index, nil
}

// DownloadAndInstall downloads the plugin .wasm and writes the manifest to the destination directory.
func (c *RegistryClient) DownloadAndInstall(ctx context.Context, plugin RegistryPlugin, pluginsBaseDir string) error {
	pluginDir := filepath.Join(pluginsBaseDir, plugin.ID)
	if err := os.MkdirAll(pluginDir, 0700); err != nil {
		return fmt.Errorf("failed to create plugin directory: %w", err)
	}

	// Resolve download URL if relative
	downloadURL := plugin.DownloadURL
	if !strings.HasPrefix(downloadURL, "http://") && !strings.HasPrefix(downloadURL, "https://") {
		base, err := url.Parse(c.registryURL)
		if err == nil {
			rel, err := url.Parse(downloadURL)
			if err == nil {
				downloadURL = base.ResolveReference(rel).String()
			}
		}
	}

	// Download wasm binary
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return fmt.Errorf("failed to construct wasm download request: %w", err)
	}
	req.Header.Set("User-Agent", "halpradio/plugin-installer")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to download wasm binary from %s: %w", downloadURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("wasm download returned HTTP %d", resp.StatusCode)
	}

	wasmBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read wasm binary: %w", err)
	}

	// Cryptographic integrity verification: SHA-256
	if plugin.Checksum != "" {
		if err := VerifyChecksum(wasmBytes, plugin.Checksum); err != nil {
			return fmt.Errorf("plugin %s failed security checksum verification: %w", plugin.ID, err)
		}
	}

	// Write plugin.wasm
	wasmPath := filepath.Join(pluginDir, "plugin.wasm")
	if err := os.WriteFile(wasmPath, wasmBytes, 0644); err != nil {
		return fmt.Errorf("failed to save wasm binary: %w", err)
	}

	// Write manifest.yaml
	manifest := Manifest{
		ID:          plugin.ID,
		Name:        plugin.Name,
		Version:     plugin.Version,
		Author:      plugin.Author,
		Description: plugin.Description,
		Homepage:    plugin.Homepage,
		WasmFile:    "plugin.wasm",
		Permissions: plugin.Permissions,
	}

	manifestBytes, err := yaml.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("failed to encode manifest: %w", err)
	}

	manifestPath := filepath.Join(pluginDir, "manifest.yaml")
	if err := os.WriteFile(manifestPath, manifestBytes, 0644); err != nil {
		return fmt.Errorf("failed to save manifest.yaml: %w", err)
	}

	return nil
}

// fallbackRegistry provides offline / bundled known plugins in case registry is unreachable.
func (c *RegistryClient) fallbackRegistry() *RegistryIndex {
	return &RegistryIndex{
		Version:   "1.0.0",
		UpdatedAt: "2026-08-18T00:00:00Z",
		Plugins: []RegistryPlugin{
			{
				ID:          "webhook-broadcaster",
				Name:        "Webhook Broadcaster",
				Version:     "1.0.0",
				Author:      "halpworld",
				Description: "Broadcast now-playing tracks and playback changes to Discord, Slack, or Home Assistant webhooks.",
				Homepage:    "https://github.com/halpworld/halpradio-plugins/tree/main/plugins/webhook-broadcaster",
				DownloadURL: "https://raw.githubusercontent.com/halpworld/halpradio-plugins/main/plugins/webhook-broadcaster/plugin.wasm",
				Permissions: PermissionsConfig{
					Network: []string{"*"},
					Storage: []string{"local"},
					Events:  []string{"on_track_change", "on_playback_change"},
				},
				Tags: []string{"webhook", "discord", "home-assistant", "integration"},
			},
			{
				ID:          "scrobble-logger",
				Name:        "Scrobble Logger & Stats",
				Version:     "1.0.0",
				Author:      "halpworld",
				Description: "Track play history, station listening hours, top artists, and record listening stats.",
				Homepage:    "https://github.com/halpworld/halpradio-plugins/tree/main/plugins/scrobble-logger",
				DownloadURL: "https://raw.githubusercontent.com/halpworld/halpradio-plugins/main/plugins/scrobble-logger/plugin.wasm",
				Permissions: PermissionsConfig{
					Storage: []string{"local"},
					Events:  []string{"on_track_change", "on_playback_change"},
				},
				Tags: []string{"scrobbler", "history", "stats", "analytics"},
			},
		},
	}
}
