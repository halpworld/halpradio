// Package art renders album artwork inside a terminal.
//
// It has two halves that can be used independently: a Client that resolves
// cover art for a track from several public providers (with RAM and disk
// caches), and a Renderer that turns encoded image bytes into terminal output
// using the best transport the host terminal supports — the Kitty graphics
// protocol, iTerm2 inline images, Sixel, truecolor half-blocks or braille.
//
// Every Renderer result is exactly the number of rows requested and every
// line reports the requested width through lipgloss.Width, so artwork can be
// dropped into a Bubble Tea layout without disturbing the surrounding frame.
package art

import (
	"os"
	"strings"
)

// Protocol identifies a terminal image transport.
type Protocol string

// Supported terminal image transports.
const (
	ProtocolKitty     Protocol = "kitty"
	ProtocolITerm2    Protocol = "iterm2"
	ProtocolSixel     Protocol = "sixel"
	ProtocolHalfBlock Protocol = "halfblock"
	ProtocolBraille   Protocol = "braille"
	ProtocolNone      Protocol = "none"
)

// Environment variables understood by DetectEnv.
const (
	// EnvProtocol forces a specific protocol, bypassing detection.
	EnvProtocol = "HALPRADIO_ART_PROTOCOL"
	// EnvNoArt disables artwork rendering entirely.
	EnvNoArt = "HALPRADIO_NO_ART"
	// EnvSixel declares that the terminal understands Sixel graphics.
	EnvSixel = "HALPRADIO_SIXEL"
)

// Label returns a short human-readable name for the protocol.
func (p Protocol) Label() string {
	switch p {
	case ProtocolKitty:
		return "Kitty Graphics"
	case ProtocolITerm2:
		return "iTerm2 Inline"
	case ProtocolSixel:
		return "Sixel"
	case ProtocolHalfBlock:
		return "Truecolor Half-Block"
	case ProtocolBraille:
		return "Braille"
	case ProtocolNone:
		return "Disabled"
	default:
		return string(p)
	}
}

// Graphical reports whether the protocol uses a real pixel transport rather
// than character-cell approximation.
func (p Protocol) Graphical() bool {
	switch p {
	case ProtocolKitty, ProtocolITerm2, ProtocolSixel:
		return true
	default:
		return false
	}
}

// DetectEnv resolves the best available protocol from the supplied environment
// lookup function. It is the testable core of Detect.
func DetectEnv(getenv func(string) string) Protocol {
	if getenv == nil {
		return ProtocolNone
	}

	// An explicit override always wins, provided it names a real protocol.
	if p, ok := parseProtocol(getenv(EnvProtocol)); ok {
		return p
	}
	if truthy(getenv("NO_GRAPHICS")) || truthy(getenv(EnvNoArt)) {
		return ProtocolNone
	}

	term := strings.ToLower(strings.TrimSpace(getenv("TERM")))
	termProgram := strings.TrimSpace(getenv("TERM_PROGRAM"))
	colorTerm := strings.ToLower(strings.TrimSpace(getenv("COLORTERM")))

	// 1. Kitty graphics protocol: kitty, ghostty and WezTerm all speak it.
	if strings.EqualFold(termProgram, "ghostty") ||
		strings.EqualFold(termProgram, "WezTerm") ||
		strings.Contains(term, "kitty") ||
		strings.TrimSpace(getenv("KITTY_WINDOW_ID")) != "" {
		return ProtocolKitty
	}

	// 2. iTerm2 inline images.
	if strings.EqualFold(termProgram, "iTerm.app") ||
		strings.TrimSpace(getenv("ITERM_SESSION_ID")) != "" {
		return ProtocolITerm2
	}

	// 3. Sixel, but only on an explicit positive signal. A generic
	//    xterm-256color plus COLORTERM is deliberately not enough.
	for _, name := range []string{"foot", "mlterm", "yaft", "sixel"} {
		if strings.Contains(term, name) {
			return ProtocolSixel
		}
	}
	if truthy(getenv(EnvSixel)) {
		return ProtocolSixel
	}

	// 4. Truecolor half-blocks.
	if colorTerm == "truecolor" || colorTerm == "24bit" || strings.Contains(term, "256color") {
		return ProtocolHalfBlock
	}

	// 5. No usable terminal at all.
	if term == "" || term == "dumb" {
		return ProtocolNone
	}

	// 6. Monochrome fallback.
	return ProtocolBraille
}

// Detect resolves the best protocol supported by the current terminal from
// process environment variables.
func Detect() Protocol {
	return DetectEnv(os.Getenv)
}

// Resolve maps a user preference ("auto", "kitty", "iterm2", "sixel",
// "halfblock", "braille", "off"/"none") to a Protocol. "auto" delegates to
// Detect. An unknown value falls back to Detect.
func Resolve(pref string) Protocol {
	if p, ok := parseProtocol(pref); ok {
		return p
	}
	return Detect()
}

// parseProtocol maps a preference string onto a Protocol. It reports false for
// the empty string, "auto" and anything it does not recognise, so callers can
// fall through to detection.
func parseProtocol(pref string) (Protocol, bool) {
	switch strings.ToLower(strings.TrimSpace(pref)) {
	case "kitty":
		return ProtocolKitty, true
	case "iterm", "iterm2":
		return ProtocolITerm2, true
	case "sixel":
		return ProtocolSixel, true
	case "halfblock", "half-block", "half_block", "blocks":
		return ProtocolHalfBlock, true
	case "braille":
		return ProtocolBraille, true
	case "off", "none", "no", "disabled":
		return ProtocolNone, true
	default:
		return "", false
	}
}

// truthy reports whether an environment value should be read as "enabled".
// Any non-empty value except an explicit negative counts as enabled.
func truthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "0", "false", "no", "off":
		return false
	default:
		return true
	}
}
