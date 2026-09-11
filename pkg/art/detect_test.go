package art

import "testing"

// envMap adapts a map to the getenv function DetectEnv expects.
func envMap(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestDetectEnv(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want Protocol
	}{
		{"nil lookup", nil, ProtocolNone},
		{"explicit override wins", map[string]string{
			EnvProtocol: "braille", "TERM": "xterm-kitty",
		}, ProtocolBraille},
		{"override off", map[string]string{EnvProtocol: "off", "TERM": "xterm-kitty"}, ProtocolNone},
		{"override auto falls through", map[string]string{
			EnvProtocol: "auto", "TERM": "xterm-kitty",
		}, ProtocolKitty},
		{"override garbage falls through", map[string]string{
			EnvProtocol: "banana", "TERM": "xterm-kitty",
		}, ProtocolKitty},
		{"NO_GRAPHICS disables", map[string]string{
			"NO_GRAPHICS": "1", "TERM": "xterm-kitty",
		}, ProtocolNone},
		{"NO_GRAPHICS=0 is not truthy", map[string]string{
			"NO_GRAPHICS": "0", "TERM": "xterm-kitty",
		}, ProtocolKitty},
		{"HALPRADIO_NO_ART disables", map[string]string{
			EnvNoArt: "true", "TERM": "xterm-256color",
		}, ProtocolNone},
		{"ghostty", map[string]string{"TERM_PROGRAM": "ghostty", "TERM": "xterm-256color"}, ProtocolKitty},
		{"wezterm", map[string]string{"TERM_PROGRAM": "WezTerm"}, ProtocolKitty},
		{"term kitty", map[string]string{"TERM": "xterm-kitty"}, ProtocolKitty},
		{"kitty window id", map[string]string{"KITTY_WINDOW_ID": "3", "TERM": "screen"}, ProtocolKitty},
		{"iterm program", map[string]string{"TERM_PROGRAM": "iTerm.app", "TERM": "xterm-256color"}, ProtocolITerm2},
		{"iterm session", map[string]string{"ITERM_SESSION_ID": "w0t0p0", "TERM": "screen"}, ProtocolITerm2},
		{"kitty beats iterm", map[string]string{
			"TERM": "xterm-kitty", "ITERM_SESSION_ID": "w0t0p0",
		}, ProtocolKitty},
		{"foot", map[string]string{"TERM": "foot"}, ProtocolSixel},
		{"mlterm", map[string]string{"TERM": "mlterm"}, ProtocolSixel},
		{"yaft", map[string]string{"TERM": "yaft-256color"}, ProtocolSixel},
		{"term says sixel", map[string]string{"TERM": "xterm-sixel"}, ProtocolSixel},
		{"sixel env flag", map[string]string{"TERM": "xterm-256color", EnvSixel: "1"}, ProtocolSixel},
		{"256color plus colorterm is not sixel", map[string]string{
			"TERM": "xterm-256color", "COLORTERM": "truecolor",
		}, ProtocolHalfBlock},
		{"truecolor", map[string]string{"TERM": "screen", "COLORTERM": "truecolor"}, ProtocolHalfBlock},
		{"24bit", map[string]string{"TERM": "screen", "COLORTERM": "24bit"}, ProtocolHalfBlock},
		{"256color term", map[string]string{"TERM": "tmux-256color"}, ProtocolHalfBlock},
		{"dumb terminal", map[string]string{"TERM": "dumb"}, ProtocolNone},
		{"empty term", map[string]string{}, ProtocolNone},
		{"plain vt100 falls back to braille", map[string]string{"TERM": "vt100"}, ProtocolBraille},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var getenv func(string) string
			if tc.env != nil {
				getenv = envMap(tc.env)
			}
			if got := DetectEnv(getenv); got != tc.want {
				t.Fatalf("DetectEnv() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestResolve(t *testing.T) {
	t.Setenv("TERM", "xterm-kitty")
	t.Setenv("HALPRADIO_ART_PROTOCOL", "")
	t.Setenv("HALPRADIO_NO_ART", "")
	t.Setenv("NO_GRAPHICS", "")

	tests := []struct {
		pref string
		want Protocol
	}{
		{"kitty", ProtocolKitty},
		{"iterm2", ProtocolITerm2},
		{"iterm", ProtocolITerm2},
		{"sixel", ProtocolSixel},
		{"halfblock", ProtocolHalfBlock},
		{"half-block", ProtocolHalfBlock},
		{"braille", ProtocolBraille},
		{"off", ProtocolNone},
		{"none", ProtocolNone},
		{"  KITTY  ", ProtocolKitty},
		{"auto", ProtocolKitty},     // delegates to Detect
		{"", ProtocolKitty},         // empty delegates to Detect
		{"nonsense", ProtocolKitty}, // unknown falls back to Detect
	}

	for _, tc := range tests {
		t.Run(tc.pref, func(t *testing.T) {
			if got := Resolve(tc.pref); got != tc.want {
				t.Fatalf("Resolve(%q) = %q, want %q", tc.pref, got, tc.want)
			}
		})
	}
}

func TestDetectUsesProcessEnv(t *testing.T) {
	t.Setenv("HALPRADIO_ART_PROTOCOL", "sixel")
	if got := Detect(); got != ProtocolSixel {
		t.Fatalf("Detect() = %q, want %q", got, ProtocolSixel)
	}
}

func TestProtocolLabelAndGraphical(t *testing.T) {
	tests := []struct {
		proto     Protocol
		label     string
		graphical bool
	}{
		{ProtocolKitty, "Kitty Graphics", true},
		{ProtocolITerm2, "iTerm2 Inline", true},
		{ProtocolSixel, "Sixel", true},
		{ProtocolHalfBlock, "Truecolor Half-Block", false},
		{ProtocolBraille, "Braille", false},
		{ProtocolNone, "Disabled", false},
		{Protocol("weird"), "weird", false},
	}

	for _, tc := range tests {
		t.Run(string(tc.proto), func(t *testing.T) {
			if got := tc.proto.Label(); got != tc.label {
				t.Errorf("Label() = %q, want %q", got, tc.label)
			}
			if got := tc.proto.Graphical(); got != tc.graphical {
				t.Errorf("Graphical() = %v, want %v", got, tc.graphical)
			}
		})
	}
}

func TestTruthy(t *testing.T) {
	tests := map[string]bool{
		"":      false,
		"0":     false,
		"false": false,
		"NO":    false,
		" off ": false,
		"1":     true,
		"true":  true,
		"yes":   true,
		"on":    true,
		"x":     true,
	}
	for in, want := range tests {
		if got := truthy(in); got != want {
			t.Errorf("truthy(%q) = %v, want %v", in, got, want)
		}
	}
}
