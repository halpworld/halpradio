package theme

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRegistryFallbackCompleteness(t *testing.T) {
	client := NewRegistryClient("")
	regIndex, err := client.FetchRegistry(context.Background())
	if err == nil {
		t.Logf("Fetched default registry index with %d themes", len(regIndex.Themes))
	}

	fallback := client.fallbackRegistry()
	if len(fallback.Themes) < 10 {
		t.Fatalf("Expected at least 10 fallback themes, got %d", len(fallback.Themes))
	}

	for _, th := range fallback.Themes {
		if th.ID == "" {
			t.Errorf("Theme %+v has empty ID", th)
		}
		if th.Name == "" {
			t.Errorf("Theme %s has empty Name", th.ID)
		}
		if th.Author == "" {
			t.Errorf("Theme %s has empty Author", th.ID)
		}
		if th.Description == "" {
			t.Errorf("Theme %s has empty Description", th.ID)
		}
		if th.Primary == "" {
			t.Errorf("Theme %s has empty Primary", th.ID)
		}
		if th.Background == "" {
			t.Errorf("Theme %s has empty Background", th.ID)
		}
		if th.Foreground == "" {
			t.Errorf("Theme %s has empty Foreground", th.ID)
		}
		if th.Playing == "" {
			t.Errorf("Theme %s has empty Playing", th.ID)
		}

		runtimeTheme := th.ToTheme()
		if runtimeTheme.ID != th.ID {
			t.Errorf("ToTheme().ID = %q, expected %q", runtimeTheme.ID, th.ID)
		}
		if !runtimeTheme.IsCustom {
			t.Errorf("ToTheme().IsCustom should be true for registry themes")
		}
	}
}

func TestRegistryFetchMockServer(t *testing.T) {
	mockData := RegistryIndex{
		Version:   "1.0.0",
		UpdatedAt: "2026-08-24T00:00:00Z",
		Themes: []RegistryTheme{
			{
				ID:          "mock-neon",
				Name:        "Mock Neon",
				Author:      "Tester",
				Description: "A test neon theme",
				Category:    "Neon",
				Primary:     "#00ffcc",
				Secondary:   "#ff00cc",
				Background:  "#111122",
				Foreground:  "#ffffff",
				Muted:       "#666688",
				Playing:     "#00ff00",
				Favorite:    "#ff0066",
				Border:      "#00ffcc",
				Highlight:   "#ffff00",
				Badge:       "#00ffcc",
				BadgeText:   "#000000",
				HeaderAscii: "#ff00cc",
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(mockData)
	}))
	defer server.Close()

	client := NewRegistryClient(server.URL)
	idx, err := client.FetchRegistry(context.Background())
	if err != nil {
		t.Fatalf("FetchRegistry returned error: %v", err)
	}

	if len(idx.Themes) != 1 {
		t.Fatalf("Expected 1 theme, got %d", len(idx.Themes))
	}
	if idx.Themes[0].ID != "mock-neon" {
		t.Errorf("Expected ID 'mock-neon', got %s", idx.Themes[0].ID)
	}
}

func TestDownloadAndInstallAndUninstall(t *testing.T) {
	ResetCustomThemes()
	tempDir := t.TempDir()

	testTheme := RegistryTheme{
		ID:          "test-download",
		Name:        "Test Download Theme",
		Author:      "Tester",
		Description: "A downloaded theme for testing",
		Category:    "Modern",
		Primary:     "#336699",
		Secondary:   "#996633",
		Background:  "#0a0a14",
		Foreground:  "#eaeaea",
		Muted:       "#555566",
		Playing:     "#33cc66",
		Favorite:    "#cc3366",
		Border:      "#336699",
		Highlight:   "#3399cc",
		Badge:       "#336699",
		BadgeText:   "#ffffff",
		HeaderAscii: "#996633",
	}

	client := NewRegistryClient("")
	err := client.DownloadAndInstall(context.Background(), testTheme, tempDir)
	if err != nil {
		t.Fatalf("DownloadAndInstall returned error: %v", err)
	}

	// 1. Verify file written on disk
	filePath := filepath.Join(tempDir, "test-download.yaml")
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("Expected theme file at %s: %v", filePath, err)
	}
	if !strings.Contains(string(data), "Test Download Theme") || !strings.Contains(string(data), "#336699") {
		t.Errorf("Theme file missing expected contents: %s", string(data))
	}

	// 2. Verify registered in active memory
	th := GetTheme("test-download")
	if th.Name != "Test Download Theme" {
		t.Errorf("GetTheme('test-download') failed, got %s", th.Name)
	}

	// 3. Test Uninstall
	if err := client.Uninstall("test-download", tempDir); err != nil {
		t.Fatalf("Uninstall returned error: %v", err)
	}

	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Errorf("Expected file %s to be deleted after uninstall", filePath)
	}

	// Verify unregister
	found := false
	for _, cid := range customThemeIDs {
		if cid == "test-download" {
			found = true
			break
		}
	}
	if found {
		t.Errorf("Expected 'test-download' removed from customThemeIDs")
	}
}

func TestDownloadAndInstallPathTraversal(t *testing.T) {
	tempDir := t.TempDir()
	client := NewRegistryClient("")

	traversalTheme := RegistryTheme{
		ID:      "../../../../etc/passwd",
		Name:    "Malicious Traversal",
		Primary: "#ff0000",
	}

	err := client.DownloadAndInstall(context.Background(), traversalTheme, tempDir)
	if err == nil {
		t.Errorf("Expected error on directory traversal ID, got nil")
	}
}
