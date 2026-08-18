package app

import (
	"bytes"
	"strings"
	"testing"
)

func TestPluginCLI(t *testing.T) {
	// Test help
	var buf bytes.Buffer
	done, err := RunPluginCLI([]string{"help"}, &buf)
	if err != nil || !done {
		t.Fatalf("RunPluginCLI(help) failed: %v", err)
	}
	if !strings.Contains(buf.String(), "halpradio plugin list") {
		t.Errorf("Expected plugin help output, got: %s", buf.String())
	}

	// Test list
	buf.Reset()
	done, err = RunPluginCLI([]string{"list"}, &buf)
	if err != nil || !done {
		t.Fatalf("RunPluginCLI(list) failed: %v", err)
	}
	if !strings.Contains(buf.String(), "INSTALLED PLUGINS") {
		t.Errorf("Expected INSTALLED PLUGINS in list output, got: %s", buf.String())
	}

	// Test invalid command
	buf.Reset()
	_, err = RunPluginCLI([]string{"invalid-subcommand"}, &buf)
	if err == nil {
		t.Errorf("Expected error for invalid subcommand, got nil")
	}
}
