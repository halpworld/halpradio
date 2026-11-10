package debuglog

import (
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/halpworld/halpradio/pkg/util"
)

// RedactedURL is logged in place of a value that looks like a URL but cannot
// be parsed, so nothing secret leaks through a parse failure.
const RedactedURL = "[unparseable-url]"

// StripURLSecrets returns raw without userinfo (user:pass@), query string and
// fragment, which is where stream credentials and tokens live. ok is false
// when raw does not parse as an absolute URL with a host.
func StripURLSecrets(raw string) (clean string, ok bool) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", false
	}
	u.User = nil
	u.RawQuery = ""
	u.ForceQuery = false
	u.Fragment = ""
	u.RawFragment = ""
	return u.String(), true
}

// RedactURL makes a URL safe to write to the debug log: userinfo, query and
// fragment are removed. An empty string stays empty; anything that does not
// parse is replaced by RedactedURL.
func RedactURL(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	if clean, ok := StripURLSecrets(raw); ok {
		return clean
	}
	return RedactedURL
}

// RedactArgs returns a copy of argv in which every element containing "://"
// is passed through RedactURL (for logging exec command lines).
func RedactArgs(args []string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		if strings.Contains(a, "://") {
			out[i] = RedactURL(a)
		} else {
			out[i] = a
		}
	}
	return out
}

// RedactText replaces every URL-looking token (anything containing "://")
// inside free text, such as a backend error message, with its redacted form.
func RedactText(s string) string {
	// One log line per entry: newlines, escapes and other control or
	// format runes from remote text are collapsed before anything else.
	s = util.SanitizeDisplay(s, 0)
	if !strings.Contains(s, "://") {
		return s
	}
	fields := strings.Fields(s)
	for i, f := range fields {
		if strings.Contains(f, "://") {
			trimmed := strings.Trim(f, `"'()<>[],;`)
			fields[i] = strings.Replace(f, trimmed, RedactURL(trimmed), 1)
		}
	}
	return strings.Join(fields, " ")
}

// RedactKey describes a key event for the log without recording typed text.
// Named keys (enter, ctrl+c, up, f1 ...) are kept; for printable input
// (isText) and bracketed paste only the length is recorded, so API keys,
// URLs, chat and room codes typed into the UI never reach the log.
func RedactKey(key string, isText, paste bool) string {
	if isText || paste {
		return fmt.Sprintf("text runes=%d paste=%t", utf8.RuneCountInString(key), paste)
	}
	return key
}
