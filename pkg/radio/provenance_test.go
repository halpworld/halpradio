package radio

import "testing"

var privateStreamURLs = []string{
	"http://127.0.0.1:8080/admin?reboot=1",
	"http://169.254.169.254/latest/meta-data/",
	"http://192.168.1.1/cgi-bin/luci",
	"http://[::1]:631/",
	"http://[::ffff:127.0.0.1]/",
	"http://localhost:6379/",
	"http://printer.local/",
	"http://metadata.google.internal/",
	"http://router/",
	"http://2130706433/",
	"http://0x7f.1/",
	"http://100.100.100.200/",
}

// RR-E-1: remote sources may not point at local or private hosts.
func TestSanitizeStation_RemoteSourcesRefusePrivateHosts(t *testing.T) {
	for _, src := range []string{"radiobrowser", "bundled", "somafm", "party", "catalog", "something-new"} {
		for _, u := range privateStreamURLs {
			if _, ok := SanitizeStation(Station{ID: "rb-1", Name: "x", URL: u, Source: src}); ok {
				t.Errorf("source %s: %s accepted", src, u)
			}
		}
		if _, ok := SanitizeStation(Station{ID: "rb-1", Name: "x", URL: "https://ice1.somafm.com/groovesalad-128-mp3", Source: src}); !ok {
			t.Errorf("source %s: public stream refused", src)
		}
	}
	// sanitizeStations stamps the source before validating.
	got, dropped := sanitizeStations([]Station{
		{ID: "a", Name: "a", URL: "http://10.0.0.5/s"},
		{ID: "b", Name: "b", URL: "https://stream.example.org/s"},
	}, "radiobrowser")
	if len(got) != 1 || dropped != 1 || got[0].ID != "b" {
		t.Fatalf("got %+v dropped %d", got, dropped)
	}
}

func TestSanitizeStation_UserStationsKeepLANHosts(t *testing.T) {
	for _, src := range []string{"local", "custom", ""} {
		for _, u := range privateStreamURLs {
			if _, ok := SanitizeStation(Station{ID: "custom-1", Name: "x", URL: u, Source: src}); !ok {
				t.Errorf("user source %q: %s refused", src, u)
			}
		}
	}
}

func TestAddOrUpdateLocalStation_AcceptsLAN(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	s := NewStore()
	// Even when the caller passes a remote source, the local catalog is the
	// user's own and is stamped "local" first.
	if err := s.AddOrUpdateLocalStation(Station{Name: "NAS", URL: "http://192.168.1.10:8000/live", Source: "radiobrowser"}); err != nil {
		t.Fatal(err)
	}
	if len(s.Local) != 1 || s.Local[0].Source != "local" {
		t.Fatalf("local = %+v", s.Local)
	}
}
