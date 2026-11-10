package theme

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/halpworld/halpradio/pkg/util"
)

const validThemeYAML = `name: "Remote Theme"
author: "Remote Author"
description: "remote"
primary: "#112233"
secondary: "#445566"
background: "#000000"
foreground: "#ffffff"
`

// tlsRegistryClient returns a client that trusts srv's certificate and is
// allowed to reach 127.0.0.1 (where httptest listens).
func tlsRegistryClient(srv *httptest.Server, registryURL string) *RegistryClient {
	c := NewRegistryClient(registryURL)
	c.httpClient = srv.Client()
	c.allowPrivateHosts = true
	return c
}

func assertNoThemeFiles(t *testing.T, dir string) {
	t.Helper()
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("expected no files in %s, found %v", dir, names)
	}
}

// LOC-2 / TTY-6: every registry text field is sanitized, IDs must be slugs,
// and built-in IDs are dropped.
func TestFetchRegistrySanitizesAllFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"version":"1\u001b[2J","themes":[
 {"id":"evil\u001b[2J","name":"bad id","primary":"#ffffff"},
 {"id":"tokyonight","name":"Shadow","primary":"#ffffff"},
 {"id":"ok-theme","name":"N\u001b]0;pwn\u0007","author":"A\u001b]52;c;ZWNobyBwd25lZA==\u0007","category":"C\u009b31m","description":"d‮esc​","primary":"#fff"},
 {"id":"ok-theme","name":"dup","primary":"#fff"}]}`)
	}))
	defer srv.Close()

	idx, err := NewRegistryClient(srv.URL).FetchRegistry(context.Background())
	if err != nil {
		t.Fatalf("FetchRegistry: %v", err)
	}
	if len(idx.Themes) != 1 {
		t.Fatalf("expected only the valid, non-builtin, non-duplicate entry, got %d: %+v", len(idx.Themes), idx.Themes)
	}
	got := idx.Themes[0]
	if got.ID != "ok-theme" {
		t.Errorf("ID = %q", got.ID)
	}
	for field, v := range map[string]string{"name": got.Name, "author": got.Author, "category": got.Category, "description": got.Description, "version": idx.Version} {
		if !util.IsDisplaySafe(v) {
			t.Errorf("%s not display safe: %q", field, v)
		}
	}
}

// LOC-9: a registry entry can never replace a built-in theme.
func TestDownloadAndInstallRefusesBuiltinID(t *testing.T) {
	ResetCustomThemes()
	defer ResetCustomThemes()
	dir := t.TempDir()
	before := GetTheme("tokyonight")
	err := NewRegistryClient("").DownloadAndInstall(context.Background(), RegistryTheme{
		ID: "tokyonight", Name: "Evil", Primary: "#ff0000", Background: "#000000", Foreground: "#ff0000",
	}, dir)
	if err == nil {
		t.Fatal("expected built-in ID to be refused")
	}
	after := GetTheme("tokyonight")
	if after.IsCustom || after.Foreground != before.Foreground {
		t.Fatalf("built-in theme was replaced: %+v", after)
	}
	assertNoThemeFiles(t, dir)
	if err := NewRegistryClient("").Uninstall("tokyonight", dir); err == nil {
		t.Fatal("expected uninstall of a built-in theme to be refused")
	}
}

func TestDownloadAndInstallRequiresHTTPS(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, validThemeYAML)
	}))
	defer srv.Close()
	dir := t.TempDir()
	c := NewRegistryClient(srv.URL + "/index.json")
	c.allowPrivateHosts = true
	err := c.DownloadAndInstall(context.Background(), RegistryTheme{ID: "plain", DownloadURL: srv.URL + "/t.yaml"}, dir)
	if err == nil {
		t.Fatal("expected http download_url to be refused")
	}
	// A relative download_url against an http registry resolves to http too.
	if err := c.DownloadAndInstall(context.Background(), RegistryTheme{ID: "plain", DownloadURL: "t.yaml"}, dir); err == nil {
		t.Fatal("expected relative download_url under an http registry to be refused")
	}
	assertNoThemeFiles(t, dir)
}

func TestDownloadAndInstallRefusesLocalAddresses(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, validThemeYAML)
	}))
	defer srv.Close()
	dir := t.TempDir()
	c := NewRegistryClient("https://example.invalid/index.json")
	c.httpClient = srv.Client() // trust the cert; guard must still refuse 127.0.0.1
	for _, u := range []string{srv.URL + "/t.yaml", "https://localhost/t.yaml", "https://10.0.0.1/t.yaml", "https://169.254.169.254/latest", "https://[::1]/t.yaml"} {
		if err := c.DownloadAndInstall(context.Background(), RegistryTheme{ID: "local", DownloadURL: u}, dir); err == nil {
			t.Errorf("expected %s to be refused", u)
		}
	}
	assertNoThemeFiles(t, dir)
}

// The dialer guard catches public hostnames that resolve to private space.
func TestRefuseNonPublicDial(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:443", "10.1.2.3:443", "192.168.1.1:443", "169.254.169.254:80", "[::1]:443", "[fe80::1]:443", "0.0.0.0:443"} {
		if err := refuseNonPublicDial("tcp", addr, nil); err == nil {
			t.Errorf("expected dial to %s to be refused", addr)
		}
	}
	if err := refuseNonPublicDial("tcp", net.JoinHostPort("140.82.112.3", "443"), nil); err != nil {
		t.Errorf("public address refused: %v", err)
	}
}

func TestDownloadAndInstallRefusesHTTPSDowngradeRedirect(t *testing.T) {
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, validThemeYAML)
	}))
	defer plain.Close()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, plain.URL+"/t.yaml", http.StatusFound)
	}))
	defer srv.Close()
	dir := t.TempDir()
	c := tlsRegistryClient(srv, srv.URL+"/index.json")
	if err := c.DownloadAndInstall(context.Background(), RegistryTheme{ID: "redir", DownloadURL: srv.URL + "/t.yaml"}, dir); err == nil {
		t.Fatal("expected https->http redirect to be refused")
	}
	assertNoThemeFiles(t, dir)
}

func TestDownloadAndInstallValidatesBodyBeforeWriting(t *testing.T) {
	bodies := map[string]string{
		"/html":  "<html><body>not a theme</body></html>",
		"/empty": "",
		"/nocol": "name: x\nprimary: notacolour\n",
		"/big":   "name: x\nprimary: \"#ffffff\"\nbackground: \"#000000\"\nforeground: \"#ffffff\"\ndescription: \"" + strings.Repeat("a", MaxThemeFileSize) + "\"\n",
	}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, bodies[r.URL.Path])
	}))
	defer srv.Close()
	dir := t.TempDir()
	c := tlsRegistryClient(srv, srv.URL+"/index.json")
	for path := range bodies {
		if err := c.DownloadAndInstall(context.Background(), RegistryTheme{ID: "bad", DownloadURL: srv.URL + path}, dir); err == nil {
			t.Errorf("expected body %s to be refused", path)
		}
	}
	assertNoThemeFiles(t, dir)
}

func TestDownloadAndInstallWritesSanitizedFile0600(t *testing.T) {
	ResetCustomThemes()
	defer ResetCustomThemes()
	payload := validThemeYAML + "extra_key: \"trailing raw bytes that must not be copied\"\n"
	payload = strings.Replace(payload, `name: "Remote Theme"`, `name: "Remote\e]0;pwn\a Theme"`, 1)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, payload)
	}))
	defer srv.Close()
	dir := t.TempDir()
	c := tlsRegistryClient(srv, srv.URL+"/index.json")
	if err := c.DownloadAndInstall(context.Background(), RegistryTheme{ID: "remote-ok", DownloadURL: "/t.yaml"}, dir); err != nil {
		t.Fatalf("DownloadAndInstall: %v", err)
	}
	p := filepath.Join(dir, "remote-ok.yaml")
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "\x1b") || strings.Contains(string(data), "trailing raw bytes") {
		t.Fatalf("raw download was written instead of the re-serialized theme:\n%s", data)
	}
	if runtime.GOOS != "windows" {
		info, _ := os.Stat(p)
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("theme file mode = %v, want 0600", info.Mode().Perm())
		}
	}
	th := GetTheme("remote-ok")
	if !util.IsDisplaySafe(th.Name) || th.Primary != "#112233" {
		t.Fatalf("registered theme = %+v", th)
	}
}

func TestLoadCustomThemesSkipsBuiltinIDs(t *testing.T) {
	ResetCustomThemes()
	defer ResetCustomThemes()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "nord.yaml"), []byte(validThemeYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, _ := LoadCustomThemes(dir)
	if _, ok := loaded["nord"]; ok {
		t.Fatal("custom file with a built-in ID was loaded")
	}
	if GetTheme("nord").IsCustom {
		t.Fatal("built-in nord replaced by a custom file")
	}
}

func TestSanitizeStringDelegatesToSharedSanitizer(t *testing.T) {
	in := "Name‮​\x1b[31m\u009b"
	got := sanitizeString(in, 64)
	if got != util.SanitizeDisplay(in, 64) || !util.IsDisplaySafe(got) {
		t.Fatalf("sanitizeString(%q) = %q", in, got)
	}
}
