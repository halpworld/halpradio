package app

import (
	"fmt"
	"io"

	"github.com/halpworld/halpradio/pkg/radio"
	"github.com/halpworld/halpradio/pkg/util"
	"gopkg.in/yaml.v3"
)

// embeddedStationCount is the number of valid stations in the catalog built
// into the binary.
func embeddedStationCount(embeddedCatalog []byte) int {
	var catalog radio.StationCatalog
	if len(embeddedCatalog) == 0 || yaml.Unmarshal(embeddedCatalog, &catalog) != nil {
		return 0
	}
	n := 0
	for _, st := range catalog.Stations {
		if radio.ValidStreamURL(st.URL) && st.ID != "" {
			n++
		}
	}
	return n
}

// newCatalogUpdater creates the catalog updater with MinStations set to the
// embedded catalog's size, so a download much smaller than what ships in the
// binary is refused.
func newCatalogUpdater(cfg util.Config, embeddedCatalog []byte) *radio.CatalogUpdater {
	u := radio.NewCatalogUpdater(cfg.CatalogUpdateURL, cfg.CatalogCacheTTLHours)
	u.MinStations = embeddedStationCount(embeddedCatalog)
	return u
}

// loadStationStore loads the station store. The cached catalog download is
// only used when catalog_auto_update is on; otherwise the embedded catalog
// stays authoritative.
func loadStationStore(embeddedCatalog []byte, cfg util.Config) *radio.Store {
	store := radio.NewStore()
	_ = store.Load(embeddedCatalog)
	if cfg.CatalogAutoUpdate {
		_ = store.ReloadBundledFromCache()
	}
	return store
}

// printStoreWarnings prints problems found while loading the station files
// (unparsable files, dropped entries).
func printStoreWarnings(out io.Writer, store *radio.Store) {
	for _, w := range store.LoadWarnings() {
		fmt.Fprintf(out, "Warning: %s\n", safe(w))
	}
}

// storeWarningStatus is a one-line TUI status for store load warnings, or "".
func storeWarningStatus(store *radio.Store) string {
	ws := store.LoadWarnings()
	switch len(ws) {
	case 0:
		return ""
	case 1:
		return "⚠ " + safe(ws[0])
	}
	return fmt.Sprintf("⚠ %s (and %d more station file warnings)", safe(ws[0]), len(ws)-1)
}
