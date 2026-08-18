package radio

import (
	"strings"
	"unicode"
)

// VerifiedBroadcasterInfo holds confirmed terrestrial broadcast details for known broadcasters.
type VerifiedBroadcasterInfo struct {
	Country   string
	City      string
	State     string
	Broadcast string // "FM", "DAB", "FM/DAB", "AM"
	Frequency string
}

func normalizeStationName(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else if unicode.IsSpace(r) || r == '-' || r == '/' || r == '.' {
			b.WriteRune(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

var verifiedNameMap = map[string]VerifiedBroadcasterInfo{
	// Ireland (IE) National Broadcaster (RTÉ)
	"rte radio 1":              {Country: "IE", City: "Dublin", State: "Leinster", Broadcast: "FM/DAB", Frequency: "88.5 FM"},
	"rté radio 1":              {Country: "IE", City: "Dublin", State: "Leinster", Broadcast: "FM/DAB", Frequency: "88.5 FM"},
	"rte 1":                    {Country: "IE", City: "Dublin", State: "Leinster", Broadcast: "FM/DAB", Frequency: "88.5 FM"},
	"rte radio 1 extra":        {Country: "IE", City: "Dublin", State: "Leinster", Broadcast: "DAB", Frequency: "DAB+ / LW"},
	"rte 2fm":                  {Country: "IE", City: "Dublin", State: "Leinster", Broadcast: "FM/DAB", Frequency: "90.7 FM"},
	"rté 2fm":                  {Country: "IE", City: "Dublin", State: "Leinster", Broadcast: "FM/DAB", Frequency: "90.7 FM"},
	"rte lyric fm":             {Country: "IE", City: "Limerick", State: "Munster", Broadcast: "FM/DAB", Frequency: "96-99 FM"},
	"rté lyric fm":             {Country: "IE", City: "Limerick", State: "Munster", Broadcast: "FM/DAB", Frequency: "96-99 FM"},
	"rte rnag":                 {Country: "IE", City: "Galway", State: "Connacht", Broadcast: "FM/DAB", Frequency: "92-94 FM"},
	"rté rnag":                 {Country: "IE", City: "Galway", State: "Connacht", Broadcast: "FM/DAB", Frequency: "92-94 FM"},
	"rte raidio na gaeltachta": {Country: "IE", City: "Galway", State: "Connacht", Broadcast: "FM/DAB", Frequency: "92-94 FM"},
	"rté raidió na gaeltachta": {Country: "IE", City: "Galway", State: "Connacht", Broadcast: "FM/DAB", Frequency: "92-94 FM"},
	"rte gold":                 {Country: "IE", City: "Dublin", State: "Leinster", Broadcast: "DAB", Frequency: "DAB+"},
	"rté gold":                 {Country: "IE", City: "Dublin", State: "Leinster", Broadcast: "DAB", Frequency: "DAB+"},
	"rte 2xm":                  {Country: "IE", City: "Dublin", State: "Leinster", Broadcast: "DAB", Frequency: "DAB+"},
	"rte pulse":                {Country: "IE", City: "Dublin", State: "Leinster", Broadcast: "DAB", Frequency: "DAB+"},
	"rte jr":                   {Country: "IE", City: "Dublin", State: "Leinster", Broadcast: "DAB", Frequency: "DAB+"},

	// Ireland (IE) Commercial & Regional Broadcasters
	"today fm":         {Country: "IE", City: "Dublin", State: "Leinster", Broadcast: "FM", Frequency: "100-102 FM"},
	"newstalk":         {Country: "IE", City: "Dublin", State: "Leinster", Broadcast: "FM", Frequency: "106-108 FM"},
	"newstalk ireland": {Country: "IE", City: "Dublin", State: "Leinster", Broadcast: "FM", Frequency: "106-108 FM"},
	"fm104":            {Country: "IE", City: "Dublin", State: "Leinster", Broadcast: "FM", Frequency: "104.4 FM"},
	"fm104 dublin":     {Country: "IE", City: "Dublin", State: "Leinster", Broadcast: "FM", Frequency: "104.4 FM"},
	"dublins 98fm":     {Country: "IE", City: "Dublin", State: "Leinster", Broadcast: "FM", Frequency: "98.1 FM"},
	"98fm":             {Country: "IE", City: "Dublin", State: "Leinster", Broadcast: "FM", Frequency: "98.1 FM"},
	"spin 1038":        {Country: "IE", City: "Dublin", State: "Leinster", Broadcast: "FM", Frequency: "103.8 FM"},
	"spin 1038 dublin": {Country: "IE", City: "Dublin", State: "Leinster", Broadcast: "FM", Frequency: "103.8 FM"},
	"corks 96fm":       {Country: "IE", City: "Cork", State: "Munster", Broadcast: "FM", Frequency: "96.4 FM"},
	"96fm":             {Country: "IE", City: "Cork", State: "Munster", Broadcast: "FM", Frequency: "96.4 FM"},
	"red fm":           {Country: "IE", City: "Cork", State: "Munster", Broadcast: "FM", Frequency: "104-106 FM"},
	"corks red fm":     {Country: "IE", City: "Cork", State: "Munster", Broadcast: "FM", Frequency: "104-106 FM"},
	"galway bay fm":    {Country: "IE", City: "Galway", State: "Connacht", Broadcast: "FM", Frequency: "95.8 FM"},
	"highland radio":   {Country: "IE", City: "Letterkenny", State: "Ulster", Broadcast: "FM", Frequency: "103.3 FM"},
	"ocean fm":         {Country: "IE", City: "Sligo", State: "Connacht", Broadcast: "FM", Frequency: "102.5 FM"},
	"midwest radio":    {Country: "IE", City: "Mayo", State: "Connacht", Broadcast: "FM", Frequency: "96.1 FM"},
	"clare fm":         {Country: "IE", City: "Ennis", State: "Munster", Broadcast: "FM", Frequency: "95-96 FM"},
	"tipp fm":          {Country: "IE", City: "Clonmel", State: "Munster", Broadcast: "FM", Frequency: "95-97 FM"},
	"wlr fm":           {Country: "IE", City: "Waterford", State: "Munster", Broadcast: "FM", Frequency: "95.1 FM"},
	"beat 102 103":     {Country: "IE", City: "Waterford", State: "Munster", Broadcast: "FM", Frequency: "102-103 FM"},
	"radio kerry":      {Country: "IE", City: "Tralee", State: "Munster", Broadcast: "FM", Frequency: "96-98 FM"},
	"live 95":          {Country: "IE", City: "Limerick", State: "Munster", Broadcast: "FM", Frequency: "95.0 FM"},
	"sunshine 106 8":   {Country: "IE", City: "Dublin", State: "Leinster", Broadcast: "FM", Frequency: "106.8 FM"},
	"radio nova":       {Country: "IE", City: "Dublin", State: "Leinster", Broadcast: "FM", Frequency: "100.3 FM"},

	// United Kingdom (GB)
	"bbc radio 1":       {Country: "GB", City: "London", State: "England", Broadcast: "FM/DAB", Frequency: "97-99 FM"},
	"bbc radio 2":       {Country: "GB", City: "London", State: "England", Broadcast: "FM/DAB", Frequency: "88-91 FM"},
	"bbc radio 3":       {Country: "GB", City: "London", State: "England", Broadcast: "FM/DAB", Frequency: "90-93 FM"},
	"bbc radio 4":       {Country: "GB", City: "London", State: "England", Broadcast: "FM/DAB", Frequency: "92-95 FM / 198 LW"},
	"bbc radio 6 music": {Country: "GB", City: "London", State: "England", Broadcast: "DAB", Frequency: "DAB+"},
	"bbc world service": {Country: "GB", City: "London", State: "England", Broadcast: "FM/DAB", Frequency: "DAB / Shortwave"},
	"classic fm":        {Country: "GB", City: "London", State: "England", Broadcast: "FM/DAB", Frequency: "100-102 FM / DAB"},
	"capital fm":        {Country: "GB", City: "London", State: "England", Broadcast: "FM/DAB", Frequency: "95.8 FM / DAB"},
	"heart uk":          {Country: "GB", City: "London", State: "England", Broadcast: "FM/DAB", Frequency: "106.2 FM / DAB"},
	"smooth radio uk":   {Country: "GB", City: "London", State: "England", Broadcast: "FM/DAB", Frequency: "102.2 FM / DAB"},

	// United States (US)
	"kexp 90 3 fm": {Country: "US", City: "Seattle", State: "WA", Broadcast: "FM", Frequency: "90.3 FM"},
	"kexp":         {Country: "US", City: "Seattle", State: "WA", Broadcast: "FM", Frequency: "90.3 FM"},
	"wnyc 93 9 fm": {Country: "US", City: "New York", State: "NY", Broadcast: "FM", Frequency: "93.9 FM"},
	"kcrw 89 9 fm": {Country: "US", City: "Santa Monica", State: "CA", Broadcast: "FM", Frequency: "89.9 FM"},
	"jazz24":       {Country: "US", City: "Seattle", State: "WA", Broadcast: "FM", Frequency: "88.5 FM"},

	// Sweden (SE)
	"sveriges radio p1": {Country: "SE", City: "Stockholm", Broadcast: "FM/DAB", Frequency: "92.4 FM"},
	"sveriges radio p3": {Country: "SE", City: "Stockholm", Broadcast: "FM/DAB", Frequency: "99.3 FM"},
	"mix megapol":       {Country: "SE", City: "Stockholm", Broadcast: "FM", Frequency: "104.3 FM"},

	// Australia (AU)
	"abc triple j":         {Country: "AU", City: "Sydney", Broadcast: "FM/DAB", Frequency: "105.7 FM / DAB"},
	"gold 104 3 melbourne": {Country: "AU", City: "Melbourne", Broadcast: "FM", Frequency: "104.3 FM"},
}

// LookupVerifiedBroadcaster checks if a station URL or canonical broadcaster name matches a verified terrestrial broadcaster.
func LookupVerifiedBroadcaster(streamURL string, name string, countryCode string) (VerifiedBroadcasterInfo, bool) {
	n := normalizeStationName(name)
	u := strings.ToLower(streamURL)
	c := strings.ToUpper(strings.TrimSpace(countryCode))

	// 1. Direct name map match
	if info, ok := verifiedNameMap[n]; ok {
		return info, true
	}

	// 2. Check prefix / contains for RTÉ stations
	if strings.HasPrefix(n, "rte ") || strings.HasPrefix(n, "rté ") {
		if strings.Contains(n, "1 extra") {
			return VerifiedBroadcasterInfo{Country: "IE", City: "Dublin", State: "Leinster", Broadcast: "DAB", Frequency: "DAB+ / LW"}, true
		}
		if strings.Contains(n, "1") || strings.Contains(n, "one") {
			return VerifiedBroadcasterInfo{Country: "IE", City: "Dublin", State: "Leinster", Broadcast: "FM/DAB", Frequency: "88.5 FM"}, true
		}
		if strings.Contains(n, "2fm") || strings.Contains(n, "2") {
			return VerifiedBroadcasterInfo{Country: "IE", City: "Dublin", State: "Leinster", Broadcast: "FM/DAB", Frequency: "90.7 FM"}, true
		}
		if strings.Contains(n, "lyric") {
			return VerifiedBroadcasterInfo{Country: "IE", City: "Limerick", State: "Munster", Broadcast: "FM/DAB", Frequency: "96-99 FM"}, true
		}
		if strings.Contains(n, "rnag") || strings.Contains(n, "gaeltachta") {
			return VerifiedBroadcasterInfo{Country: "IE", City: "Galway", State: "Connacht", Broadcast: "FM/DAB", Frequency: "92-94 FM"}, true
		}
		if strings.Contains(n, "gold") {
			return VerifiedBroadcasterInfo{Country: "IE", City: "Dublin", State: "Leinster", Broadcast: "DAB", Frequency: "DAB+"}, true
		}
	}

	// 3. Irish RTÉ stream domain
	if strings.Contains(u, "rte.ie") {
		if strings.Contains(u, "radio1extra") {
			return VerifiedBroadcasterInfo{Country: "IE", City: "Dublin", State: "Leinster", Broadcast: "DAB", Frequency: "DAB+ / LW"}, true
		}
		if strings.Contains(u, "radio1") || strings.Contains(u, "ieradio1") {
			return VerifiedBroadcasterInfo{Country: "IE", City: "Dublin", State: "Leinster", Broadcast: "FM/DAB", Frequency: "88.5 FM"}, true
		}
		if strings.Contains(u, "2fm") {
			return VerifiedBroadcasterInfo{Country: "IE", City: "Dublin", State: "Leinster", Broadcast: "FM/DAB", Frequency: "90.7 FM"}, true
		}
		if strings.Contains(u, "lyric") {
			return VerifiedBroadcasterInfo{Country: "IE", City: "Limerick", State: "Munster", Broadcast: "FM/DAB", Frequency: "96-99 FM"}, true
		}
		if strings.Contains(u, "rnag") {
			return VerifiedBroadcasterInfo{Country: "IE", City: "Galway", State: "Connacht", Broadcast: "FM/DAB", Frequency: "92-94 FM"}, true
		}
		if strings.Contains(u, "gold") {
			return VerifiedBroadcasterInfo{Country: "IE", City: "Dublin", State: "Leinster", Broadcast: "DAB", Frequency: "DAB+"}, true
		}
		return VerifiedBroadcasterInfo{Country: "IE", City: "Dublin", State: "Leinster", Broadcast: "FM/DAB", Frequency: "88.5 FM"}, true
	}

	// 4. Irish Audioxi commercial streams
	if strings.Contains(u, "audioxi.com") || c == "IE" {
		if strings.Contains(u, "/td") || strings.Contains(n, "today fm") {
			return VerifiedBroadcasterInfo{Country: "IE", City: "Dublin", State: "Leinster", Broadcast: "FM", Frequency: "100-102 FM"}, true
		}
		if strings.Contains(u, "/nt") || strings.Contains(n, "newstalk") {
			return VerifiedBroadcasterInfo{Country: "IE", City: "Dublin", State: "Leinster", Broadcast: "FM", Frequency: "106-108 FM"}, true
		}
		if strings.Contains(u, "/fm104") || strings.Contains(n, "fm104") {
			return VerifiedBroadcasterInfo{Country: "IE", City: "Dublin", State: "Leinster", Broadcast: "FM", Frequency: "104.4 FM"}, true
		}
		if strings.Contains(u, "/98") || strings.Contains(n, "98fm") {
			return VerifiedBroadcasterInfo{Country: "IE", City: "Dublin", State: "Leinster", Broadcast: "FM", Frequency: "98.1 FM"}, true
		}
		if strings.Contains(u, "/sp") || strings.Contains(n, "spin 1038") {
			return VerifiedBroadcasterInfo{Country: "IE", City: "Dublin", State: "Leinster", Broadcast: "FM", Frequency: "103.8 FM"}, true
		}
		if strings.Contains(u, "/96") || strings.Contains(n, "96fm") {
			return VerifiedBroadcasterInfo{Country: "IE", City: "Cork", State: "Munster", Broadcast: "FM", Frequency: "96.4 FM"}, true
		}
		if strings.Contains(u, "/red") || strings.Contains(n, "red fm") {
			return VerifiedBroadcasterInfo{Country: "IE", City: "Cork", State: "Munster", Broadcast: "FM", Frequency: "104-106 FM"}, true
		}
		if strings.Contains(u, "/gbfm") || strings.Contains(n, "galway bay") {
			return VerifiedBroadcasterInfo{Country: "IE", City: "Galway", State: "Connacht", Broadcast: "FM", Frequency: "95.8 FM"}, true
		}
	}

	// 5. BBC Network
	if strings.Contains(u, "bbcmedia.co.uk") || strings.Contains(u, "bbc.co.uk") {
		if strings.Contains(u, "radio1") || strings.Contains(n, "bbc radio 1") {
			return VerifiedBroadcasterInfo{Country: "GB", City: "London", State: "England", Broadcast: "FM/DAB", Frequency: "97-99 FM"}, true
		}
		if strings.Contains(u, "radio2") || strings.Contains(n, "bbc radio 2") {
			return VerifiedBroadcasterInfo{Country: "GB", City: "London", State: "England", Broadcast: "FM/DAB", Frequency: "88-91 FM"}, true
		}
		if strings.Contains(u, "radio3") || strings.Contains(n, "bbc radio 3") {
			return VerifiedBroadcasterInfo{Country: "GB", City: "London", State: "England", Broadcast: "FM/DAB", Frequency: "90-93 FM"}, true
		}
		if strings.Contains(u, "radio4") || strings.Contains(n, "bbc radio 4") {
			return VerifiedBroadcasterInfo{Country: "GB", City: "London", State: "England", Broadcast: "FM/DAB", Frequency: "92-95 FM / 198 LW"}, true
		}
		if strings.Contains(u, "6music") || strings.Contains(n, "bbc radio 6") {
			return VerifiedBroadcasterInfo{Country: "GB", City: "London", State: "England", Broadcast: "DAB", Frequency: "DAB+"}, true
		}
		if strings.Contains(u, "world_service") || strings.Contains(n, "bbc world service") {
			return VerifiedBroadcasterInfo{Country: "GB", City: "London", State: "England", Broadcast: "FM/DAB", Frequency: "DAB / Shortwave"}, true
		}
	}

	// 6. Sveriges Radio
	if strings.Contains(u, "sr.se") {
		if strings.Contains(u, "p1") || strings.Contains(n, "sr p1") {
			return VerifiedBroadcasterInfo{Country: "SE", City: "Stockholm", Broadcast: "FM/DAB", Frequency: "92.4 FM"}, true
		}
		if strings.Contains(u, "p3") || strings.Contains(n, "sr p3") {
			return VerifiedBroadcasterInfo{Country: "SE", City: "Stockholm", Broadcast: "FM/DAB", Frequency: "99.3 FM"}, true
		}
	}

	// 7. Global Radio UK
	if strings.Contains(u, "musicradio.com") {
		if strings.Contains(u, "classic") || strings.Contains(n, "classic fm") {
			return VerifiedBroadcasterInfo{Country: "GB", City: "London", Broadcast: "FM/DAB", Frequency: "100-102 FM / DAB"}, true
		}
		if strings.Contains(u, "capital") || strings.Contains(n, "capital fm") {
			return VerifiedBroadcasterInfo{Country: "GB", City: "London", Broadcast: "FM/DAB", Frequency: "95.8 FM / DAB"}, true
		}
		if strings.Contains(u, "heart") || strings.Contains(n, "heart uk") {
			return VerifiedBroadcasterInfo{Country: "GB", City: "London", Broadcast: "FM/DAB", Frequency: "106.2 FM / DAB"}, true
		}
		if strings.Contains(u, "smooth") || strings.Contains(n, "smooth radio") {
			return VerifiedBroadcasterInfo{Country: "GB", City: "London", Broadcast: "FM/DAB", Frequency: "102.2 FM / DAB"}, true
		}
	}

	// 8. Radio Swiss (SRG SSR)
	if strings.Contains(u, "srg-ssr.ch") || strings.Contains(u, "srgssr.ch") {
		if strings.Contains(u, "rsc") || strings.Contains(n, "radio swiss classic") {
			return VerifiedBroadcasterInfo{Country: "CH", City: "Basel", Broadcast: "DAB+", Frequency: "DAB+"}, true
		}
		if strings.Contains(u, "rsj") || strings.Contains(n, "radio swiss jazz") {
			return VerifiedBroadcasterInfo{Country: "CH", City: "Basel", Broadcast: "DAB+", Frequency: "DAB+"}, true
		}
		if strings.Contains(u, "rsp") || strings.Contains(n, "radio swiss pop") {
			return VerifiedBroadcasterInfo{Country: "CH", City: "Basel", Broadcast: "DAB+", Frequency: "DAB+"}, true
		}
	}

	return VerifiedBroadcasterInfo{}, false
}

// EnrichStation populates confirmed terrestrial broadcast metadata if the station belongs to a verified terrestrial broadcaster.
func EnrichStation(st Station) Station {
	if st.Broadcast != "" && st.Broadcast != "Online" && st.Frequency != "" {
		return st
	}

	if info, ok := LookupVerifiedBroadcaster(st.URL, st.Name, st.Country); ok {
		if st.Broadcast == "" || st.Broadcast == "Online" {
			st.Broadcast = info.Broadcast
		}
		if st.Frequency == "" {
			st.Frequency = info.Frequency
		}
		if st.Country == "" {
			st.Country = info.Country
		}
		if st.City == "" {
			st.City = info.City
		}
		if st.State == "" {
			st.State = info.State
		}
		return st
	}

	return st
}
