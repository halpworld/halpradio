package radio

import (
	"os"
	"strings"
)

// LocaleCountry returns the 2-letter country code from the user's POSIX
// locale (LC_ALL, then LC_MESSAGES, then LANG), e.g. "zh_TW.UTF-8" -> "TW".
// It returns "" when no locale names a country.
func LocaleCountry() string {
	for _, env := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if code := CountryFromLocale(os.Getenv(env)); code != "" {
			return code
		}
	}
	return ""
}

// CountryFromLocale extracts the territory from a locale string such as
// "ja_JP.UTF-8", "pt_BR@euro" or "en-GB". It returns "" for "C", "POSIX",
// or values without a two-letter territory.
func CountryFromLocale(locale string) string {
	l := strings.TrimSpace(locale)
	if i := strings.IndexAny(l, ".@"); i >= 0 {
		l = l[:i]
	}
	i := strings.IndexAny(l, "_-")
	if i < 0 {
		return ""
	}
	territory := l[i+1:]
	if len(territory) != 2 {
		return ""
	}
	for _, r := range territory {
		if (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') {
			return ""
		}
	}
	return strings.ToUpper(territory)
}

// PrioritizeCountry moves the entry for code to the front of list so a
// listener sees their own country first. The rest keep their order. The
// list is returned unchanged when code is empty or not present.
func PrioritizeCountry(list []CountryInfo, code string) []CountryInfo {
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" {
		return list
	}
	for i, c := range list {
		if c.Code != code {
			continue
		}
		if i == 0 {
			return list
		}
		out := make([]CountryInfo, 0, len(list))
		out = append(out, c)
		out = append(out, list[:i]...)
		out = append(out, list[i+1:]...)
		return out
	}
	return list
}
