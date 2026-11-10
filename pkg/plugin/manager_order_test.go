package plugin

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/halpworld/halpradio/pkg/util"
)

// FR-A-4: GetPlugins returns plugins sorted by ID, so the row a list draws
// and the row a key acts on are the same plugin.
func TestGetPluginsSortedByID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	ids := []string{"plug-e", "plug-a", "plug-d", "plug-b", "plug-f", "plug-c"}
	for _, id := range ids {
		dir := filepath.Join(util.GetPluginsDir(), id)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		man := fmt.Sprintf("id: %s\nname: %s\nversion: 1.0.0\nauthor: a\n", id, id)
		if err := os.WriteFile(filepath.Join(dir, "manifest.yaml"), []byte(man), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "plugin.wasm"), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	mgr := NewManager("")
	if err := mgr.Scan(); err != nil {
		t.Fatal(err)
	}
	defer mgr.Close()
	want := []string{"plug-a", "plug-b", "plug-c", "plug-d", "plug-e", "plug-f"}
	for i := 0; i < 100; i++ {
		got := mgr.GetPlugins()
		if len(got) != len(want) {
			t.Fatalf("got %d plugins, want %d", len(got), len(want))
		}
		for j := range want {
			if got[j].Manifest.ID != want[j] {
				t.Fatalf("call %d: position %d is %s, want %s", i, j, got[j].Manifest.ID, want[j])
			}
		}
	}
}
