package radio

import (
	"fmt"
	"hash/fnv"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// GeoPoint represents latitude and longitude coordinates in degrees.
type GeoPoint struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

// StationCluster groups stations located in the same city or geographic area.
type StationCluster struct {
	ID          string    `json:"id"`
	City        string    `json:"city"`
	CountryCode string    `json:"country_code"`
	CountryName string    `json:"country_name"`
	Flag        string    `json:"flag"`
	Lat         float64   `json:"lat"`
	Lon         float64   `json:"lon"`
	Stations    []Station `json:"stations"`
}

// WorldCapitalsAndCentroids maps 2-letter ISO country codes to default geographic coordinates.
var CountryCentroids = map[string]GeoPoint{
	"US": {Lat: 38.8951, Lon: -77.0364},  // Washington DC / USA
	"GB": {Lat: 51.5074, Lon: -0.1278},   // London / UK
	"UK": {Lat: 51.5074, Lon: -0.1278},   // London / UK
	"IE": {Lat: 53.3498, Lon: -6.2603},   // Dublin / Ireland
	"FR": {Lat: 48.8566, Lon: 2.3522},    // Paris / France
	"DE": {Lat: 52.5200, Lon: 13.4050},   // Berlin / Germany
	"JP": {Lat: 35.6762, Lon: 139.6503},  // Tokyo / Japan
	"NL": {Lat: 52.3676, Lon: 4.9041},    // Amsterdam / Netherlands
	"IT": {Lat: 41.9028, Lon: 12.4964},   // Rome / Italy
	"ES": {Lat: 40.4168, Lon: -3.7038},   // Madrid / Spain
	"CA": {Lat: 45.4215, Lon: -75.6972},  // Ottawa / Canada
	"AU": {Lat: -35.2809, Lon: 149.1300}, // Canberra / Australia
	"NZ": {Lat: -41.2865, Lon: 174.7762}, // Wellington / New Zealand
	"BR": {Lat: -15.7975, Lon: -47.8919}, // Brasilia / Brazil
	"MX": {Lat: 19.4326, Lon: -99.1332},  // Mexico City / Mexico
	"AR": {Lat: -34.6037, Lon: -58.3816}, // Buenos Aires / Argentina
	"CH": {Lat: 46.9480, Lon: 7.4474},    // Bern / Switzerland
	"AT": {Lat: 48.2082, Lon: 16.3738},   // Vienna / Austria
	"SE": {Lat: 59.3293, Lon: 18.0686},   // Stockholm / Sweden
	"NO": {Lat: 59.9139, Lon: 10.7522},   // Oslo / Norway
	"DK": {Lat: 55.6761, Lon: 12.5683},   // Copenhagen / Denmark
	"FI": {Lat: 60.1699, Lon: 24.9384},   // Helsinki / Finland
	"PT": {Lat: 38.7223, Lon: -9.1393},   // Lisbon / Portugal
	"GR": {Lat: 37.9838, Lon: 23.7275},   // Athens / Greece
	"PL": {Lat: 52.2297, Lon: 21.0122},   // Warsaw / Poland
	"CZ": {Lat: 50.0755, Lon: 14.4378},   // Prague / Czech Republic
	"BE": {Lat: 50.8503, Lon: 4.3517},    // Brussels / Belgium
	"KR": {Lat: 37.5665, Lon: 126.9780},  // Seoul / South Korea
	"CN": {Lat: 39.9042, Lon: 116.4074},  // Beijing / China
	"IN": {Lat: 28.6139, Lon: 77.2090},   // New Delhi / India
	"ZA": {Lat: -25.7479, Lon: 28.2293},  // Pretoria / South Africa
	"IS": {Lat: 64.1466, Lon: -21.9426},  // Reykjavik / Iceland
	"CL": {Lat: -33.4489, Lon: -70.6693}, // Santiago / Chile
	"CO": {Lat: 4.7110, Lon: -74.0721},   // Bogota / Colombia
	"SG": {Lat: 1.3521, Lon: 103.8198},   // Singapore
	"TH": {Lat: 13.7563, Lon: 100.5018},  // Bangkok / Thailand
	"ID": {Lat: -6.2088, Lon: 106.8456},  // Jakarta / Indonesia
	"PH": {Lat: 14.5995, Lon: 120.9842},  // Manila / Philippines
	"EG": {Lat: 30.0444, Lon: 31.2357},   // Cairo / Egypt
	"NG": {Lat: 9.0765, Lon: 7.3986},     // Abuja / Nigeria
	"KE": {Lat: -1.2921, Lon: 36.8219},   // Nairobi / Kenya
	"IL": {Lat: 31.7683, Lon: 35.2137},   // Jerusalem / Israel
	"TR": {Lat: 39.9334, Lon: 32.8597},   // Ankara / Turkey
	"UA": {Lat: 50.4501, Lon: 30.5234},   // Kyiv / Ukraine
	"RO": {Lat: 44.4268, Lon: 26.1025},   // Bucharest / Romania
	"HU": {Lat: 47.4979, Lon: 19.0402},   // Budapest / Hungary
}

// MajorCityCoordinates provides precise coordinates for world broadcast metropolitan hubs.
var MajorCityCoordinates = map[string]GeoPoint{
	// North America
	"new york":      {Lat: 40.7128, Lon: -74.0060},
	"new york city": {Lat: 40.7128, Lon: -74.0060},
	"nyc":           {Lat: 40.7128, Lon: -74.0060},
	"brooklyn":      {Lat: 40.6782, Lon: -73.9442},
	"los angeles":   {Lat: 34.0522, Lon: -118.2437},
	"san francisco": {Lat: 37.7749, Lon: -122.4194},
	"seattle":       {Lat: 47.6062, Lon: -122.3321},
	"chicago":       {Lat: 41.8781, Lon: -87.6298},
	"austin":        {Lat: 30.2672, Lon: -97.7431},
	"boston":        {Lat: 42.3601, Lon: -71.0589},
	"miami":         {Lat: 25.7617, Lon: -80.1918},
	"toronto":       {Lat: 43.6532, Lon: -79.3832},
	"montreal":      {Lat: 45.5017, Lon: -73.5673},
	"vancouver":     {Lat: 49.2827, Lon: -123.1207},
	"mexico city":   {Lat: 19.4326, Lon: -99.1332},

	// Europe
	"london":     {Lat: 51.5074, Lon: -0.1278},
	"manchester": {Lat: 53.4808, Lon: -2.2426},
	"birmingham": {Lat: 52.4862, Lon: -1.8904},
	"glasgow":    {Lat: 55.8642, Lon: -4.2518},
	"edinburgh":  {Lat: 55.9533, Lon: -3.1883},
	"dublin":     {Lat: 53.3498, Lon: -6.2603},
	"cork":       {Lat: 51.8985, Lon: -8.4756},
	"galway":     {Lat: 53.2707, Lon: -9.0568},
	"limerick":   {Lat: 52.6638, Lon: -8.6267},
	"paris":      {Lat: 48.8566, Lon: 2.3522},
	"lyon":       {Lat: 45.7640, Lon: 4.8357},
	"marseille":  {Lat: 43.2965, Lon: 5.3698},
	"berlin":     {Lat: 52.5200, Lon: 13.4050},
	"munich":     {Lat: 48.1351, Lon: 11.5820},
	"hamburg":    {Lat: 53.5511, Lon: 9.9937},
	"cologne":    {Lat: 50.9375, Lon: 6.9603},
	"frankfurt":  {Lat: 50.1109, Lon: 8.6821},
	"amsterdam":  {Lat: 52.3676, Lon: 4.9041},
	"rotterdam":  {Lat: 51.9244, Lon: 4.4777},
	"brussels":   {Lat: 50.8503, Lon: 4.3517},
	"rome":       {Lat: 41.9028, Lon: 12.4964},
	"milan":      {Lat: 45.4642, Lon: 9.1900},
	"madrid":     {Lat: 40.4168, Lon: -3.7038},
	"barcelona":  {Lat: 41.3879, Lon: 2.1699},
	"lisbon":     {Lat: 38.7223, Lon: -9.1393},
	"porto":      {Lat: 41.1579, Lon: -8.6291},
	"vienna":     {Lat: 48.2082, Lon: 16.3738},
	"zurich":     {Lat: 47.3769, Lon: 8.5417},
	"geneva":     {Lat: 46.2044, Lon: 6.1432},
	"stockholm":  {Lat: 59.3293, Lon: 18.0686},
	"oslo":       {Lat: 59.9139, Lon: 10.7522},
	"copenhagen": {Lat: 55.6761, Lon: 12.5683},
	"helsinki":   {Lat: 60.1699, Lon: 24.9384},
	"athens":     {Lat: 37.9838, Lon: 23.7275},
	"prague":     {Lat: 50.0755, Lon: 14.4378},
	"warsaw":     {Lat: 52.2297, Lon: 21.0122},

	// Asia & Oceania
	"tokyo":     {Lat: 35.6762, Lon: 139.6503},
	"osaka":     {Lat: 34.6937, Lon: 135.5023},
	"kyoto":     {Lat: 35.0116, Lon: 135.7681},
	"sapporo":   {Lat: 43.0618, Lon: 141.3545},
	"nagoya":    {Lat: 35.1815, Lon: 136.9066},
	"fukuoka":   {Lat: 33.5904, Lon: 130.4017},
	"seoul":     {Lat: 37.5665, Lon: 126.9780},
	"beijing":   {Lat: 39.9042, Lon: 116.4074},
	"shanghai":  {Lat: 31.2304, Lon: 121.4737},
	"hong kong": {Lat: 22.3193, Lon: 114.1694},
	"taipei":    {Lat: 25.0330, Lon: 121.5654},
	"sydney":    {Lat: -33.8688, Lon: 151.2093},
	"melbourne": {Lat: -37.8136, Lon: 144.9631},
	"brisbane":  {Lat: -27.4698, Lon: 153.0251},
	"auckland":  {Lat: -36.8485, Lon: 174.7633},

	// South America & Africa
	"sao paulo":      {Lat: -23.5505, Lon: -46.6333},
	"rio de janeiro": {Lat: -22.9068, Lon: -43.1729},
	"buenos aires":   {Lat: -34.6037, Lon: -58.3816},
	"santiago":       {Lat: -33.4489, Lon: -70.6693},
	"bogota":         {Lat: 4.7110, Lon: -74.0721},
	"johannesburg":   {Lat: -26.2041, Lon: 28.0473},
	"cape town":      {Lat: -33.9249, Lon: 18.4241},
	"cairo":          {Lat: 30.0444, Lon: 31.2357},
}

// GeocodeStation resolves geographic latitude/longitude and normalized city name for a Station.
func GeocodeStation(st Station) (lat, lon float64, city string, ok bool) {
	// 1. Try explicit city
	if st.City != "" {
		cityKey := strings.ToLower(strings.TrimSpace(st.City))
		if pt, found := MajorCityCoordinates[cityKey]; found {
			return pt.Lat, pt.Lon, st.City, true
		}
	}

	// 2. Try state/province if known
	if st.State != "" {
		stateKey := strings.ToLower(strings.TrimSpace(st.State))
		if pt, found := MajorCityCoordinates[stateKey]; found {
			return pt.Lat, pt.Lon, st.State, true
		}
	}

	// 3. Try searching city name in Station Name (e.g. "BBC Radio London", "FM 802 Osaka", "KEXP Seattle")
	nameLower := strings.ToLower(st.Name)
	for cityKey, pt := range MajorCityCoordinates {
		if strings.Contains(nameLower, cityKey) {
			cityName := strings.Title(cityKey)
			return pt.Lat, pt.Lon, cityName, true
		}
	}

	// 4. Fallback to Country centroid
	countryCode := strings.ToUpper(strings.TrimSpace(st.Country))
	if len(countryCode) != 2 {
		if code := CountryNameToCode(st.Country); code != "" {
			countryCode = code
		}
	}
	if pt, found := CountryCentroids[countryCode]; found {
		countryName := countryCode
		if name, exists := CountryCodeToName[countryCode]; exists {
			countryName = name
		}
		return pt.Lat, pt.Lon, countryName, true
	}

	// 5. Default fallback to International / Equator
	return 0.0, 0.0, "Global", false
}

// HaversineDistance computes great-circle distance between two coordinates in kilometers.
func HaversineDistance(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadiusKm = 6371.0

	dLat := (lat2 - lat1) * (math.Pi / 180.0)
	dLon := (lon2 - lon1) * (math.Pi / 180.0)

	rLat1 := lat1 * (math.Pi / 180.0)
	rLat2 := lat2 * (math.Pi / 180.0)

	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(rLat1)*math.Cos(rLat2)*math.Sin(dLon/2)*math.Sin(dLon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))

	return earthRadiusKm * c
}

// BuildStationClusters groups a list of stations into geographic city/regional clusters.
func BuildStationClusters(stations []Station) []StationCluster {
	var clusters []StationCluster
	const clusterThresholdKm = 100.0 // Group stations within 100km radius into same cluster

	for _, st := range stations {
		lat, lon, cityName, _ := GeocodeStation(st)
		countryCode := strings.ToUpper(strings.TrimSpace(st.Country))
		if len(countryCode) != 2 {
			if code := CountryNameToCode(st.Country); code != "" {
				countryCode = code
			}
		}
		countryName := st.CountryName()
		flag := CountryFlagForCode(countryCode)

		// Check if station belongs to an existing nearby cluster
		matchedIdx := -1
		for i := range clusters {
			dist := HaversineDistance(clusters[i].Lat, clusters[i].Lon, lat, lon)
			if dist <= clusterThresholdKm && (clusters[i].CountryCode == countryCode || clusters[i].CountryCode == "") {
				matchedIdx = i
				break
			}
		}

		if matchedIdx >= 0 {
			clusters[matchedIdx].Stations = append(clusters[matchedIdx].Stations, st)
		} else {
			clusterID := fmt.Sprintf("cluster-%s-%s", strings.ToLower(countryCode), strings.ToLower(strings.ReplaceAll(cityName, " ", "-")))
			clusters = append(clusters, StationCluster{
				ID:          clusterID,
				City:        cityName,
				CountryCode: countryCode,
				CountryName: countryName,
				Flag:        flag,
				Lat:         lat,
				Lon:         lon,
				Stations:    []Station{st},
			})
		}
	}

	return clusters
}

// FindNearestCluster finds the closest StationCluster to target coordinates (lat, lon).
func FindNearestCluster(clusters []StationCluster, targetLat, targetLon float64) (*StationCluster, int, float64) {
	if len(clusters) == 0 {
		return nil, -1, 0.0
	}

	minDist := math.MaxFloat64
	bestIdx := -1

	for i := range clusters {
		dist := HaversineDistance(targetLat, targetLon, clusters[i].Lat, clusters[i].Lon)
		if dist < minDist {
			minDist = dist
			bestIdx = i
		}
	}

	if bestIdx >= 0 {
		return &clusters[bestIdx], bestIdx, minDist
	}
	return nil, -1, 0.0
}

var freqRegex = regexp.MustCompile(`(?i)([0-9]{2,4}(?:\.[0-9]{1,2})?)\s*(?:MHz|FM|kHz|AM)?`)

// ExtractOrAssignFrequency parses the frequency in MHz/kHz from station metadata,
// or calculates a deterministic frequency slot on the dial based on the station's ID.
func ExtractOrAssignFrequency(st Station, band string) float64 {
	band = strings.ToUpper(strings.TrimSpace(band))
	if band == "" {
		band = "FM"
	}

	// 1. Try extracting explicit frequency from Frequency or Name string
	rawFreq := st.Frequency
	if rawFreq == "" {
		rawFreq = st.Name
	}

	if match := freqRegex.FindStringSubmatch(rawFreq); len(match) >= 2 {
		if val, err := strconv.ParseFloat(match[1], 64); err == nil {
			if band == "FM" && val >= 87.5 && val <= 108.0 {
				return math.Round(val*10) / 10
			}
			if band == "AM" && val >= 530 && val <= 1710 {
				return math.Round(val)
			}
			if band == "SW" && val >= 3.0 && val <= 30.0 {
				return math.Round(val*100) / 100
			}
		}
	}

	// 2. Deterministic dial slot based on FNV hash of station ID
	h := fnv.New32a()
	h.Write([]byte(st.ID + ":" + st.Name))
	hashVal := float64(h.Sum32())

	switch band {
	case "AM":
		// AM: 530 kHz - 1710 kHz (step 10 kHz)
		stepCount := (1710 - 530) / 10
		idx := int(math.Mod(hashVal, float64(stepCount)))
		return float64(530 + idx*10)
	case "SW":
		// Shortwave: 3.2 MHz - 22.0 MHz (step 0.05 MHz)
		steps := int((22.0 - 3.2) / 0.05)
		idx := int(math.Mod(hashVal, float64(steps)))
		return math.Round((3.2+float64(idx)*0.05)*100) / 100
	default: // FM
		// FM: 87.5 MHz - 108.0 MHz (step 0.1 MHz)
		steps := int((108.0 - 87.5) / 0.1)
		idx := int(math.Mod(hashVal, float64(steps)))
		return math.Round((87.5+float64(idx)*0.1)*10) / 10
	}
}

// LookupNearestLocation finds the closest known country or city to a given coordinate.
func LookupNearestLocation(lat, lon float64) (countryCode, countryName, city, flag string, distKm float64) {
	bestDist := math.MaxFloat64
	bestCity := ""
	bestCountryCode := ""

	// Check major metropolitan coordinates
	for cityName, pt := range MajorCityCoordinates {
		d := HaversineDistance(lat, lon, pt.Lat, pt.Lon)
		if d < bestDist {
			bestDist = d
			bestCity = cityName
			// Determine closest country centroid
			minCentroidDist := math.MaxFloat64
			for code, centroid := range CountryCentroids {
				cd := HaversineDistance(pt.Lat, pt.Lon, centroid.Lat, centroid.Lon)
				if cd < minCentroidDist {
					minCentroidDist = cd
					bestCountryCode = code
				}
			}
		}
	}

	// Check country centroids
	for code, pt := range CountryCentroids {
		d := HaversineDistance(lat, lon, pt.Lat, pt.Lon)
		if d < bestDist {
			bestDist = d
			bestCountryCode = code
			bestCity = Station{Country: code}.CountryName()
		}
	}

	if bestCountryCode == "" {
		return "GL", "Global", "Earth Territory", "🌍", bestDist
	}

	countryName = Station{Country: bestCountryCode}.CountryName()
	flag = CountryFlagForCode(bestCountryCode)
	if bestCity == "" {
		bestCity = countryName
	}
	return bestCountryCode, countryName, bestCity, flag, bestDist
}
