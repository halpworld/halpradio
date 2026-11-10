package radio

import (
	"os"
	"testing"
)

func TestCountryFromLocale(t *testing.T) {
	cases := map[string]string{
		"zh_TW.UTF-8": "TW",
		"zh_HK":       "HK",
		"ja_JP.UTF-8": "JP",
		"nb_NO.UTF-8": "NO",
		"ar_EG@latin": "EG",
		"en-GB":       "GB",
		"C":           "",
		"POSIX":       "",
		"":            "",
		"en":          "",
		"en_001":      "",
		"sr_RS.UTF-8": "RS",
	}
	for in, want := range cases {
		if got := CountryFromLocale(in); got != want {
			t.Errorf("CountryFromLocale(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLocaleCountryPrecedence(t *testing.T) {
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_MESSAGES", "fi_FI.UTF-8")
	t.Setenv("LANG", "en_US.UTF-8")
	if got := LocaleCountry(); got != "FI" {
		t.Errorf("LocaleCountry() = %q, want FI (LC_MESSAGES before LANG)", got)
	}
	t.Setenv("LC_ALL", "he_IL.UTF-8")
	if got := LocaleCountry(); got != "IL" {
		t.Errorf("LocaleCountry() = %q, want IL (LC_ALL wins)", got)
	}
	os.Unsetenv("LC_ALL")
	os.Unsetenv("LC_MESSAGES")
	t.Setenv("LANG", "C")
	if got := LocaleCountry(); got != "" {
		t.Errorf("LocaleCountry() = %q, want empty for C locale", got)
	}
}

func TestPrioritizeCountry(t *testing.T) {
	list := []CountryInfo{{Code: "US"}, {Code: "GB"}, {Code: "TW"}, {Code: "IE"}}

	got := PrioritizeCountry(list, "tw")
	want := []string{"TW", "US", "GB", "IE"}
	for i, c := range got {
		if c.Code != want[i] {
			t.Fatalf("PrioritizeCountry order = %v, want %v", codes(got), want)
		}
	}
	if list[0].Code != "US" {
		t.Errorf("PrioritizeCountry mutated its input: %v", codes(list))
	}

	if got := PrioritizeCountry(list, "ZZ"); codes(got)[0] != "US" || len(got) != 4 {
		t.Errorf("unknown code should leave list unchanged, got %v", codes(got))
	}
	if got := PrioritizeCountry(list, ""); codes(got)[0] != "US" {
		t.Errorf("empty code should leave list unchanged, got %v", codes(got))
	}
}

func codes(list []CountryInfo) []string {
	out := make([]string, len(list))
	for i, c := range list {
		out[i] = c.Code
	}
	return out
}

func TestSearchByLanguageAndNativeCountryName(t *testing.T) {
	stations := []Station{
		{ID: "rthk", Name: "RTHK Radio 2", Country: "HK", Language: "Cantonese"},
		{ID: "yle", Name: "YleX", Country: "FI", Language: "Finnish"},
		{ID: "nogoum", Name: "Nogoum FM", Country: "EG", Language: "Arabic"},
		{ID: "kan", Name: "Kan Bet", Country: "IL", Language: "Hebrew"},
	}
	cases := map[string]string{
		"cantonese": "rthk",
		"香港":        "rthk",
		"suomi":     "yle",
		"arabic":    "nogoum",
		"مصر":       "nogoum",
		"ישראל":     "kan",
		"hong kong": "rthk",
	}
	for q, wantID := range cases {
		got := Filter(stations, q, "")
		if len(got) != 1 || got[0].ID != wantID {
			t.Errorf("Filter(%q) = %v, want only %s", q, got, wantID)
		}
	}
}
