package theme

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The theme index must be fetched over https (plain http only for a
// loopback registry); otherwise anyone on the path can swap the theme list.
func TestRegistryIndexRequiresHTTPS(t *testing.T) {
	for _, u := range []string{"http://themes.example.com/index.json", "ftp://example.com/x", "http://10.0.0.1/i.json", "not a url"} {
		if err := checkRegistryIndexURL(u); err == nil {
			t.Errorf("%q accepted", u)
		}
	}
	for _, u := range []string{DefaultThemeRegistryURL, "https://example.com/i.json", "http://127.0.0.1:8080/i.json", "http://localhost/i.json", "http://[::1]/i.json"} {
		if err := checkRegistryIndexURL(u); err != nil {
			t.Errorf("%q refused: %v", u, err)
		}
	}

	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		fmt.Fprint(w, `{"themes":[{"id":"x","name":"x","primary":"#fff"}]}`)
	}))
	defer srv.Close()
	// Same server, reached through a non-loopback http name: refused before
	// any request.
	c := NewRegistryClient(strings.Replace(srv.URL, "127.0.0.1", "themes.example.com", 1))
	if _, err := c.FetchRegistry(context.Background()); err == nil || !strings.Contains(err.Error(), "https") {
		t.Fatalf("plain http index accepted: %v", err)
	}
	if hits != 0 {
		t.Errorf("request sent to a plain http index")
	}
}
