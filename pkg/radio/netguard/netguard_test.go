package netguard

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCheckURL(t *testing.T) {
	good := []string{"http://e.example/s", "https://e.example:8443/a?b=c"}
	bad := []string{"", "file:///etc/passwd", "javascript:alert(1)", "https:///x", "http://u:p@e.example/", "http://e.example/a b", "http://e.example/\x1b]52;c;x\x07", "http://e.example/\u009b2J", "gopher://e.example/"}
	for _, u := range good {
		if _, err := CheckURL(u); err != nil {
			t.Errorf("CheckURL(%q) = %v", u, err)
		}
	}
	for _, u := range bad {
		if _, err := CheckURL(u); err == nil {
			t.Errorf("CheckURL(%q) accepted", u)
		}
	}
}

func req(t *testing.T, raw string) *http.Request {
	t.Helper()
	r, err := http.NewRequest(http.MethodGet, raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestCheckRedirect(t *testing.T) {
	pub := req(t, "https://api.example.com/x")
	cases := []struct {
		name string
		next string
		via  []*http.Request
		ok   bool
	}{
		{"same scheme public", "https://cdn.example.com/y", []*http.Request{pub}, true},
		{"https to http downgrade", "http://cdn.example.com/y", []*http.Request{pub}, false},
		{"public to loopback", "https://127.0.0.1/y", []*http.Request{pub}, false},
		{"public to metadata", "https://169.254.169.254/latest", []*http.Request{pub}, false},
		{"public to private", "https://10.0.0.1/", []*http.Request{pub}, false},
		{"public to localhost", "https://localhost/", []*http.Request{pub}, false},
		{"public to ipv6 loopback", "https://[::1]/", []*http.Request{pub}, false},
		{"file scheme", "file:///etc/passwd", []*http.Request{pub}, false},
		{"too many", "https://cdn.example.com/y", []*http.Request{pub, pub, pub}, false},
		{"local origin may stay local", "http://127.0.0.1:9/y", []*http.Request{req(t, "http://127.0.0.1:8/x")}, true},
	}
	for _, c := range cases {
		err := CheckRedirect(req(t, c.next), c.via)
		if (err == nil) != c.ok {
			t.Errorf("%s: CheckRedirect err=%v, want ok=%v", c.name, err, c.ok)
		}
	}
}

func TestClientRefusesPrivateFromPublicOrigin(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits++ }))
	defer srv.Close()
	c := NewClient(2 * time.Second)

	// A request chain that started at a public origin must not dial loopback.
	ctx := WithOrigin(context.Background(), "https://api.example.com/")
	r, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	if _, err := c.Do(r); err == nil || !errors.Is(err, ErrRestricted) {
		t.Errorf("public-origin request reached loopback: err=%v", err)
	}
	// No origin at all is treated as public too.
	r, _ = http.NewRequest(http.MethodGet, srv.URL, nil)
	if _, err := c.Do(r); err == nil {
		t.Errorf("origin-less request reached loopback")
	}
	if hits != 0 {
		t.Errorf("server was hit %d times", hits)
	}
	// A local origin (test server, local mirror) is allowed.
	r, _ = http.NewRequestWithContext(WithOrigin(context.Background(), srv.URL), http.MethodGet, srv.URL, nil)
	resp, err := c.Do(r)
	if err != nil {
		t.Fatalf("local-origin request refused: %v", err)
	}
	resp.Body.Close()
}

func TestClientRefusesHTTPSDowngradeRedirect(t *testing.T) {
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("plain")) }))
	defer plain.Close()
	tls := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, plain.URL+"/x", http.StatusFound)
	}))
	defer tls.Close()
	c := NewClient(2 * time.Second)
	c.Transport.(*http.Transport).TLSClientConfig = tls.Client().Transport.(*http.Transport).TLSClientConfig
	r, _ := http.NewRequestWithContext(WithOrigin(context.Background(), tls.URL), http.MethodGet, tls.URL, nil)
	if resp, err := c.Do(r); err == nil {
		resp.Body.Close()
		t.Fatalf("https to http redirect followed")
	}
}

func TestIsRestrictedIP(t *testing.T) {
	for _, s := range []string{"127.0.0.1", "10.1.2.3", "172.16.0.1", "192.168.1.1", "169.254.169.254", "100.64.0.1", "0.0.0.0", "::1", "fe80::1", "fc00::1", "224.0.0.1"} {
		if !IsRestrictedIP(net.ParseIP(s)) {
			t.Errorf("%s not restricted", s)
		}
	}
	for _, s := range []string{"1.1.1.1", "93.184.216.34", "2606:4700::1111"} {
		if IsRestrictedIP(net.ParseIP(s)) {
			t.Errorf("%s restricted", s)
		}
	}
}

func TestReadLimited(t *testing.T) {
	if b, err := ReadLimited(strings.NewReader("abc"), 3); err != nil || string(b) != "abc" {
		t.Errorf("exact limit: %q %v", b, err)
	}
	if _, err := ReadLimited(strings.NewReader("abcd"), 3); !errors.Is(err, ErrTooLarge) {
		t.Errorf("over limit: %v", err)
	}
}

func TestLimiter(t *testing.T) {
	now := time.Unix(0, 0)
	l := NewLimiter(2, 10*time.Second)
	l.SetClock(func() time.Time { return now })
	if !l.Allow("p") || !l.Allow("p") {
		t.Fatal("burst not allowed")
	}
	if l.Allow("p") {
		t.Fatal("third lookup within interval allowed")
	}
	if !l.Allow("other") {
		t.Fatal("providers share a bucket")
	}
	now = now.Add(10 * time.Second)
	if !l.Allow("p") {
		t.Fatal("token not refilled after interval")
	}
	if l.Allow("p") {
		t.Fatal("more than one token refilled")
	}
}

func TestWriteFileAtomic(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.json")
	if err := os.WriteFile(p, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := WriteFileAtomic(p, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	info, _ := os.Stat(p)
	if string(b) != "new" || info.Mode().Perm() != 0600 {
		t.Errorf("content %q mode %v", b, info.Mode().Perm())
	}
	ents, _ := os.ReadDir(dir)
	if len(ents) != 1 {
		t.Errorf("temp files left behind: %d entries", len(ents))
	}
}

func TestPruneDir(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, size int, age time.Duration) {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, make([]byte, size), 0600); err != nil {
			t.Fatal(err)
		}
		ts := time.Now().Add(-age)
		_ = os.Chtimes(p, ts, ts)
	}
	write("old.json", 10, 48*time.Hour)
	write("a.json", 100, 3*time.Hour)
	write("b.json", 100, 2*time.Hour)
	write("c.json", 100, 1*time.Hour)
	write("keep.txt", 1000, 72*time.Hour)
	write(".x.json.1.tmp", 5, 2*time.Hour)
	PruneDir(dir, 24*time.Hour, 250, ".json")
	exists := func(n string) bool { _, err := os.Stat(filepath.Join(dir, n)); return err == nil }
	if exists("old.json") || exists("a.json") || exists(".x.json.1.tmp") {
		t.Errorf("expired, oldest or stray temp file not pruned")
	}
	if !exists("b.json") || !exists("c.json") || !exists("keep.txt") {
		t.Errorf("pruned too much")
	}
}
