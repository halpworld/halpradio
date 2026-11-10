package plugin

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// FR-C-6: 0.0.0.0/8, :: and IPv4-compatible addresses are never reachable,
// whatever the rule says, and a rule naming only them is invalid.
func TestUnspecifiedAddressesNeverAllowed(t *testing.T) {
	rules := []string{"0.0.0.0/8", "0.0.0.0", "0.0.0.0/32", "::", "::/128", "::/96", "0.0.0.0/0", "::/0", "*"}
	for _, rule := range rules {
		p := PermissionsConfig{Network: []string{rule}}
		for _, target := range []string{"0.0.0.0", "0.1.2.3", "::", "::7f00:1", "::ffff:0.0.0.0"} {
			if p.IsIPAllowed(net.ParseIP(target)) {
				t.Errorf("rule %s allowed %s at dial time", rule, target)
			}
			host := target
			if strings.Contains(host, ":") {
				host = "[" + host + "]"
			}
			if p.CanAccessNetwork("http://" + host + ":8080/") {
				t.Errorf("rule %s allowed URL host %s", rule, host)
			}
		}
	}
	// Explicit loopback rules keep working.
	if !(PermissionsConfig{Network: []string{"::1"}}).IsIPAllowed(net.IPv6loopback) {
		t.Error(`rule "::1" no longer reaches ::1`)
	}
	if !(PermissionsConfig{Network: []string{"127.0.0.1"}}).IsIPAllowed(net.ParseIP("127.0.0.1")) {
		t.Error(`rule "127.0.0.1" no longer reaches 127.0.0.1`)
	}
}

func TestUnspecifiedRulesRejectedAtValidation(t *testing.T) {
	bad := []string{"0.0.0.0", "0.0.0.0/8", "0.0.0.0/32", "0.1.2.3", "0.255.0.0/16", "::", "::/128", "::/96", "::7f00:1", "::ffff:0.0.0.0/104", "::/127"}
	for _, r := range bad {
		if ValidNetworkRule(r) {
			t.Errorf("ValidNetworkRule(%q) = true", r)
		}
		m := Manifest{ID: "unspec", Name: "U", Version: "1.0.0", WasmFile: "plugin.wasm",
			Permissions: PermissionsConfig{Network: []string{r}}}
		if err := m.Validate(); err == nil {
			t.Errorf("manifest with rule %q validated", r)
		}
	}
	for _, r := range []string{"0.0.0.0/0", "0.0.0.0/7", "::/0", "::1", "::1/128", "127.0.0.1", "192.168.1.0/24", "api.example.com"} {
		if !ValidNetworkRule(r) {
			t.Errorf("ValidNetworkRule(%q) = false", r)
		}
	}
}

// End to end: with a "0.0.0.0/8" rule (as an old manifest might carry),
// the sandbox transport still cannot reach this host through 0.0.0.0.
func TestUnspecifiedDialBlockedEndToEnd(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		fmt.Fprint(w, "SECRET")
	}))
	defer srv.Close()
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	m := Manifest{ID: "x", Permissions: PermissionsConfig{Network: []string{"0.0.0.0/8", "::/96", "*"}}}
	c := &http.Client{Timeout: 3 * time.Second, Transport: newSandboxedHTTPTransport(m)}
	for _, h := range []string{"0.0.0.0", "0", "[::]"} {
		if resp, err := c.Get("http://" + h + ":" + port + "/"); err == nil {
			resp.Body.Close()
			t.Errorf("%s reached the local server", h)
		}
	}
	if hits.Load() != 0 {
		t.Errorf("server hit %d times", hits.Load())
	}
}

// Consent prompts show every rule in full, so the caps must hold:
// at most MaxNetworkRules rules of at most MaxNetworkRuleLen bytes each.
func TestNetworkRuleCaps(t *testing.T) {
	if MaxNetworkRules != 32 || MaxNetworkRuleLen != 253 {
		t.Fatalf("caps changed: %d rules, %d bytes", MaxNetworkRules, MaxNetworkRuleLen)
	}
	label := strings.Repeat("a", 63)
	long := strings.Join([]string{label, label, label, strings.Repeat("b", 61)}, ".") // 253
	if !ValidNetworkRule(long) {
		t.Fatalf("253-byte host rule refused")
	}
	if ValidNetworkRule(long + "b") {
		t.Fatal("254-byte host rule accepted")
	}
	if ValidNetworkRule(strings.Repeat("a", 64) + ".example.com") {
		t.Fatal("64-character label accepted")
	}
	many := make([]string, MaxNetworkRules+1)
	for i := range many {
		many[i] = fmt.Sprintf("h%d.example.com", i)
	}
	if err := (PermissionsConfig{Network: many}).Validate(); err == nil {
		t.Fatal("33 network rules accepted")
	}
	if err := (PermissionsConfig{Network: many[:MaxNetworkRules]}).Validate(); err != nil {
		t.Fatalf("32 network rules refused: %v", err)
	}
}
