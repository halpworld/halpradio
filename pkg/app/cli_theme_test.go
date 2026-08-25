package app

import (
	"bytes"
	"strings"
	"testing"

	"github.com/halpworld/halpradio/pkg/theme"
)

func TestThemeCLIHelpAndList(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	t.Setenv("XDG_CONFIG_HOME", tempDir)

	// 1. Help output
	var buf bytes.Buffer
	done, err := RunThemeCLI([]string{"help"}, &buf)
	if !done || err != nil {
		t.Fatalf("RunThemeCLI help failed: %v", err)
	}
	if !strings.Contains(buf.String(), "Discover, preview, download") || !strings.Contains(buf.String(), "halpradio theme") {
		t.Errorf("Expected theme help output, got: %s", buf.String())
	}

	// 2. List output
	buf.Reset()
	done, err = RunThemeCLI([]string{"list"}, &buf)
	if !done || err != nil {
		t.Fatalf("RunThemeCLI list failed: %v", err)
	}
	outStr := buf.String()
	if !strings.Contains(outStr, "INSTALLED THEMES") || !strings.Contains(outStr, "Tokyo Night") {
		t.Errorf("Expected installed themes in list, got: %s", outStr)
	}
	if !strings.Contains(outStr, "COMMUNITY REPOSITORY HUB") || !strings.Contains(outStr, "rose-pine") {
		t.Errorf("Expected community repository hub in list, got: %s", outStr)
	}
}

func TestThemeCLIPreviewAndInfo(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	t.Setenv("XDG_CONFIG_HOME", tempDir)

	// 1. Preview built-in theme
	var buf bytes.Buffer
	done, err := RunThemeCLI([]string{"preview", "catppuccin"}, &buf)
	if !done || err != nil {
		t.Fatalf("RunThemeCLI preview catppuccin failed: %v", err)
	}
	outStr := buf.String()
	if !strings.Contains(outStr, "THEME PREVIEW: Catppuccin Mocha") || !strings.Contains(outStr, "HALPRADIO") {
		t.Errorf("Expected preview mockup output, got: %s", outStr)
	}

	// 2. Preview remote theme (e.g. rose-pine)
	buf.Reset()
	done, err = RunThemeCLI([]string{"preview", "rose-pine"}, &buf)
	if !done || err != nil {
		t.Fatalf("RunThemeCLI preview rose-pine failed: %v", err)
	}
	if !strings.Contains(buf.String(), "THEME PREVIEW: Rosé Pine") {
		t.Errorf("Expected remote theme preview, got: %s", buf.String())
	}

	// 3. Info command
	buf.Reset()
	done, err = RunThemeCLI([]string{"info", "nord"}, &buf)
	if !done || err != nil {
		t.Fatalf("RunThemeCLI info failed: %v", err)
	}
	if !strings.Contains(buf.String(), "THEME INFO: Nord") || !strings.Contains(buf.String(), "Primary:") {
		t.Errorf("Expected theme info output, got: %s", buf.String())
	}
}

func TestThemeCLIInstallRemoveExport(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	t.Setenv("XDG_CONFIG_HOME", tempDir)

	// 1. Export active theme
	var buf bytes.Buffer
	done, err := RunThemeCLI([]string{"export", "test-exported"}, &buf)
	if !done || err != nil {
		t.Fatalf("RunThemeCLI export failed: %v", err)
	}
	if !strings.Contains(buf.String(), "Exported theme to") {
		t.Errorf("Expected export confirmation, got: %s", buf.String())
	}

	// 2. Install remote theme (e.g. kanagawa)
	buf.Reset()
	done, err = RunThemeCLI([]string{"install", "kanagawa"}, &buf)
	if !done || err != nil {
		t.Fatalf("RunThemeCLI install failed: %v", err)
	}
	if !strings.Contains(buf.String(), "Successfully installed Kanagawa") {
		t.Errorf("Expected install confirmation, got: %s", buf.String())
	}

	// Verify installed in runtime
	th := theme.GetTheme("kanagawa")
	if th.Name != "Kanagawa" {
		t.Errorf("Expected Kanagawa registered, got %s", th.Name)
	}

	// 3. Remove custom theme
	buf.Reset()
	done, err = RunThemeCLI([]string{"remove", "kanagawa"}, &buf)
	if !done || err != nil {
		t.Fatalf("RunThemeCLI remove failed: %v", err)
	}
	if !strings.Contains(buf.String(), "Successfully removed") {
		t.Errorf("Expected remove confirmation, got: %s", buf.String())
	}
}

func TestThemeCLISuggestions(t *testing.T) {
	var buf bytes.Buffer
	done, err := RunThemeCLI([]string{"lst"}, &buf)
	if done || err == nil {
		t.Fatalf("Expected error for unknown command 'lst'")
	}
	if !strings.Contains(buf.String(), "Did you mean \"list\"?") {
		t.Errorf("Expected typo suggestion for 'lst', got: %s", buf.String())
	}
}
