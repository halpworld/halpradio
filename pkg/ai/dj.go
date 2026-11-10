package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/halpworld/halpradio/pkg/util"
)

const (
	// defaultOllamaURL is the loopback Ollama endpoint used when none is set.
	defaultOllamaURL = "http://127.0.0.1:11434"
	// maxOllamaResponseBytes caps the /api/generate response body.
	maxOllamaResponseBytes = 64 << 10
	// maxDJScriptWords and maxDJScriptRunes bound any generated DJ script.
	maxDJScriptWords = 55
	maxDJScriptRunes = 400
	// maxOfflineScriptRunes bounds template scripts built from track metadata.
	maxOfflineScriptRunes = 600
)

// Persona defines an AI Radio DJ personality, tone, and default voice model.
type Persona struct {
	ID             string
	Name           string
	Tagline        string
	SystemPrompt   string
	DefaultVoiceID string
}

var personaCatalog = []Persona{
	{
		ID:             "late-night-chill",
		Name:           "Late Night Chill",
		Tagline:        "Warm, laid-back SomaFM & midnight FM host",
		SystemPrompt:   "You are a warm, charismatic late-night FM radio host on halpradio. Write a single tight 1-2 sentence (under 35 words) track intro. Never use markdown, hashtags, or emojis.",
		DefaultVoiceID: "en_US-lessac-medium",
	},
	{
		ID:             "retro-80s",
		Name:           "Retro 80s Broadcaster",
		Tagline:        "High-energy analog FM dial & neon synthwave host",
		SystemPrompt:   "You are a classic 1980s FM rock and synthwave radio DJ on halpradio. Write a punchy 1-2 sentence (under 35 words) track intro with analog FM flair. Never use markdown or emojis.",
		DefaultVoiceID: "en_US-ryan-medium",
	},
	{
		ID:             "cyberpunk",
		Name:           "Cyberpunk Host",
		Tagline:        "Night City underground pirate signal & chrome frequencies",
		SystemPrompt:   "You are a cyberpunk pirate radio DJ broadcasting across the neon grid on halpradio. Write a sleek 1-2 sentence (under 35 words) track intro. Never use markdown or emojis.",
		DefaultVoiceID: "en_US-amy-medium",
	},
	{
		ID:             "bbc-presenter",
		Name:           "BBC Presenter",
		Tagline:        "Knowledgeable BBC Radio 6 Music crate-digger & curator",
		SystemPrompt:   "You are a knowledgeable, articulate BBC Radio 6 Music presenter on halpradio. Write an insightful 1-2 sentence (under 35 words) track intro highlighting recording context. Never use markdown or emojis.",
		DefaultVoiceID: "en_GB-alan-medium",
	},
}

// DefaultPersonas returns all supported AI DJ personas.
func DefaultPersonas() []Persona {
	out := make([]Persona, len(personaCatalog))
	copy(out, personaCatalog)
	return out
}

// GetPersona returns the persona matching id, or "late-night-chill" by default.
func GetPersona(id string) Persona {
	id = strings.ToLower(strings.TrimSpace(id))
	for _, p := range personaCatalog {
		if p.ID == id {
			return p
		}
	}
	return personaCatalog[0]
}

// NextPersonaID cycles to the next persona ID in the catalog.
func NextPersonaID(currentID string) string {
	currentID = strings.ToLower(strings.TrimSpace(currentID))
	for i, p := range personaCatalog {
		if p.ID == currentID {
			return personaCatalog[(i+1)%len(personaCatalog)].ID
		}
	}
	return personaCatalog[0].ID
}

// TrackContext holds the station and track metadata used to craft a DJ intro.
type TrackContext struct {
	StationName string
	Genre       string
	Country     string
	Artist      string
	Title       string
	Album       string
	ReleaseYear string
	OriginCity  string
	Trivia      string
}

// DJConfig configures the local AI DJ host.
type DJConfig struct {
	PersonaID   string
	TTSEngine   string
	VoiceModel  string
	OllamaURL   string
	OllamaModel string
	DuckLevel   int
	Engine      TTSEngine
	HTTPClient  *http.Client
}

// DJ manages script generation (local Ollama or deterministic persona templates)
// and thread-safe local TTS voice synthesis.
type DJ struct {
	mu          sync.Mutex
	persona     Persona
	ttsEngine   TTSEngine
	ollamaURL   string
	ollamaModel string
	duckLevel   int
	httpClient  *http.Client
	// ollamaURLErr is set when the configured Ollama URL was refused; the
	// DJ then uses offline templates only.
	ollamaURLErr error

	speaking    bool
	speakSeq    uint64
	speakCancel context.CancelFunc
	lastScript  string
	variantIdx  int
}

// NewDJ creates a new local AI Radio DJ manager.
func NewDJ(cfg DJConfig) *DJ {
	persona := GetPersona(cfg.PersonaID)
	voiceModel := strings.TrimSpace(cfg.VoiceModel)
	if voiceModel == "" {
		voiceModel = persona.DefaultVoiceID
	}
	duck := cfg.DuckLevel
	if duck <= 0 || duck > 100 {
		duck = 20
	}
	ollamaURL := strings.TrimSpace(cfg.OllamaURL)
	if ollamaURL == "" {
		ollamaURL = defaultOllamaURL
	}
	validURL, urlErr := ValidateOllamaURL(ollamaURL)
	engine := cfg.Engine
	if engine == nil {
		engine = NewTTSEngine(cfg.TTSEngine, voiceModel)
	}
	var client http.Client
	if cfg.HTTPClient != nil {
		client = *cfg.HTTPClient
	} else {
		client = http.Client{Timeout: 2500 * time.Millisecond}
	}
	// Never follow redirects from the LLM endpoint: a 3xx response is
	// returned as-is and treated as a failure (offline templates are used).
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &DJ{
		persona:      persona,
		ttsEngine:    engine,
		ollamaURL:    validURL,
		ollamaURLErr: urlErr,
		ollamaModel:  strings.TrimSpace(cfg.OllamaModel),
		duckLevel:    duck,
		httpClient:   &client,
	}
}

// ValidateOllamaURL checks an Ollama endpoint and returns it without a
// trailing slash. Only http and https are accepted, without credentials,
// query or fragment. Plain http is accepted only for loopback hosts
// (localhost, 127.0.0.0/8, ::1), because the prompt and the reply would
// otherwise cross the network unencrypted.
func ValidateOllamaURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.Opaque != "" {
		return "", errors.New("ollama URL must be an absolute http(s) URL")
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("ollama URL must not contain credentials, a query or a fragment")
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
	case "http":
		if !isLoopbackHost(u.Hostname()) {
			return "", fmt.Errorf("ollama URL %q uses plain http to a non-loopback host; use https", util.SanitizeDisplay(u.Host, 128))
		}
	default:
		return "", errors.New("ollama URL scheme must be http or https")
	}
	return strings.TrimRight(u.String(), "/"), nil
}

func isLoopbackHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// OllamaURLError returns why the configured Ollama URL was refused, or nil.
// When non-nil the DJ only uses offline persona templates.
func (d *DJ) OllamaURLError() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.ollamaURLErr
}

// Persona returns the currently active DJ persona.
func (d *DJ) Persona() Persona {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.persona
}

// SetPersona updates the active DJ persona.
func (d *DJ) SetPersona(personaID string) Persona {
	p := GetPersona(personaID)
	d.mu.Lock()
	d.persona = p
	d.mu.Unlock()
	return p
}

// SetTTSEngine updates the local TTS engine preference and voice model.
func (d *DJ) SetTTSEngine(preferred string, modelID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if modelID == "" {
		modelID = d.persona.DefaultVoiceID
	}
	d.ttsEngine = NewTTSEngine(preferred, modelID)
}

// EngineLabel returns the human-readable status label of the active TTS engine.
func (d *DJ) EngineLabel() string {
	d.mu.Lock()
	eng := d.ttsEngine
	d.mu.Unlock()
	if eng == nil {
		return "Visual Broadcast (0ms Cloud Lag)"
	}
	return eng.Label()
}

// DuckRatio returns the target audio ducking ratio in [0.05, 1.0] (e.g. 0.20 for 20%).
func (d *DJ) DuckRatio() float64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	ratio := float64(d.duckLevel) / 100.0
	if ratio < 0.05 {
		ratio = 0.05
	}
	if ratio > 1.0 {
		ratio = 1.0
	}
	return ratio
}

// IsSpeaking reports whether a DJ voice announcement is currently in flight.
func (d *DJ) IsSpeaking() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.speaking
}

// LastScript returns the most recently generated DJ intro script.
func (d *DJ) LastScript() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.lastScript
}

// GenerateIntro crafts a 5-second radio intro script for the given track context.
// If a local Ollama model is configured and responds within the timeout, it is
// used; otherwise deterministic offline persona templates are used with zero latency.
func (d *DJ) GenerateIntro(ctx context.Context, tc TrackContext) (string, string, error) {
	tc = sanitizeTrackContext(tc)
	d.mu.Lock()
	persona := d.persona
	ollamaModel := d.ollamaModel
	ollamaURL := d.ollamaURL
	client := d.httpClient
	variant := d.variantIdx
	d.variantIdx++
	eng := d.ttsEngine
	d.mu.Unlock()

	label := "Visual Broadcast (0ms Cloud Lag)"
	if eng != nil {
		label = eng.Label()
	}

	var script string
	if ollamaModel != "" && ollamaURL != "" {
		if llmScript, err := queryLocalOllama(ctx, client, ollamaURL, ollamaModel, persona, tc); err == nil && llmScript != "" {
			script = llmScript
		}
	}
	if script == "" {
		script = util.SanitizeDisplay(buildOfflinePersonaScript(persona, tc, variant), maxOfflineScriptRunes)
	}

	d.mu.Lock()
	d.lastScript = script
	d.mu.Unlock()

	return script, label, nil
}

// Speak synthesizes and plays the given DJ script, cancelling any previous speech in flight.
func (d *DJ) Speak(ctx context.Context, text string) error {
	text = util.SanitizeDisplay(text, maxSpokenRunes)
	if text == "" {
		return nil
	}

	d.mu.Lock()
	if d.speakCancel != nil {
		d.speakCancel()
		d.speakCancel = nil
	}
	d.speakSeq++
	mySeq := d.speakSeq
	speakCtx, cancel := context.WithCancel(ctx)
	d.speakCancel = cancel
	d.speaking = true
	d.lastScript = text
	eng := d.ttsEngine
	personaID := d.persona.ID
	d.mu.Unlock()

	defer func() {
		d.mu.Lock()
		if d.speakSeq == mySeq {
			d.speaking = false
			d.speakCancel = nil
		}
		d.mu.Unlock()
		cancel()
	}()

	if eng == nil {
		return nil
	}
	err := eng.Speak(speakCtx, text, personaID)
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

// Cancel immediately aborts any in-flight DJ voice synthesis.
func (d *DJ) Cancel() {
	d.mu.Lock()
	cancel := d.speakCancel
	d.speakCancel = nil
	d.speaking = false
	d.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

type ollamaGenerateRequest struct {
	Model   string         `json:"model"`
	Prompt  string         `json:"prompt"`
	System  string         `json:"system"`
	Stream  bool           `json:"stream"`
	Options map[string]any `json:"options,omitempty"`
}

type ollamaGenerateResponse struct {
	Response string `json:"response"`
}

func queryLocalOllama(ctx context.Context, client *http.Client, baseURL, model string, persona Persona, tc TrackContext) (string, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 2500*time.Millisecond)
	defer cancel()

	prompt := fmt.Sprintf(
		"Station: %s\nGenre: %s\nArtist: %s\nTrack: %s\nAlbum: %s\nYear: %s\nOrigin: %s\nWrite a 1-2 sentence radio DJ intro:",
		fallbackStr(tc.StationName, "halpradio"),
		fallbackStr(tc.Genre, "eclectic"),
		fallbackStr(tc.Artist, "Unknown Artist"),
		fallbackStr(tc.Title, "Featured Track"),
		tc.Album,
		tc.ReleaseYear,
		tc.OriginCity,
	)

	payload, err := json.Marshal(ollamaGenerateRequest{
		Model:  model,
		Prompt: prompt,
		System: persona.SystemPrompt,
		Stream: false,
		Options: map[string]any{
			"temperature": 0.7,
			"num_predict": 70,
		},
	})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, baseURL+"/api/generate", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ollama status %d", resp.StatusCode)
	}

	var out ollamaGenerateResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxOllamaResponseBytes)).Decode(&out); err != nil {
		return "", err
	}

	cleaned := sanitizeDJScript(out.Response)
	return cleaned, nil
}

// sanitizeDJScript turns raw LLM output into a displayable, speakable script:
// terminal control and format characters are removed, whitespace collapsed,
// wrapping quotes and leading dashes stripped, and the result is capped at
// maxDJScriptWords words and maxDJScriptRunes runes.
func sanitizeDJScript(raw string) string {
	s := util.SanitizeDisplay(raw, maxOllamaResponseBytes)
	s = strings.Trim(s, `"'“” `)
	s = strings.TrimSpace(strings.TrimLeft(s, leadingDashCutset))
	fields := strings.Fields(s)
	if len(fields) > maxDJScriptWords {
		fields = fields[:maxDJScriptWords]
	}
	return util.SanitizeDisplay(strings.Join(fields, " "), maxDJScriptRunes)
}

// sanitizeTrackContext cleans and caps every metadata field before it is used
// in an LLM prompt or an offline script (ICY titles, MusicBrainz and
// Wikipedia data are all remote input).
func sanitizeTrackContext(tc TrackContext) TrackContext {
	tc.StationName = util.SanitizeDisplay(tc.StationName, 80)
	tc.Genre = util.SanitizeDisplay(tc.Genre, 60)
	tc.Country = util.SanitizeDisplay(tc.Country, 60)
	tc.Artist = util.SanitizeDisplay(tc.Artist, 100)
	tc.Title = util.SanitizeDisplay(tc.Title, 120)
	tc.Album = util.SanitizeDisplay(tc.Album, 100)
	tc.ReleaseYear = util.SanitizeDisplay(tc.ReleaseYear, 8)
	tc.OriginCity = util.SanitizeDisplay(tc.OriginCity, 80)
	tc.Trivia = util.SanitizeDisplay(tc.Trivia, 200)
	return tc
}

func buildOfflinePersonaScript(persona Persona, tc TrackContext, variant int) string {
	station := strings.TrimSpace(tc.StationName)
	if station == "" {
		station = "halpradio"
	}
	artist := strings.TrimSpace(tc.Artist)
	title := strings.TrimSpace(tc.Title)
	album := strings.TrimSpace(tc.Album)
	year := strings.TrimSpace(tc.ReleaseYear)
	origin := strings.TrimSpace(tc.OriginCity)
	if origin == "" {
		origin = strings.TrimSpace(tc.Country)
	}
	genre := strings.TrimSpace(tc.Genre)

	// Build optional album/year/origin clause, e.g. ", from the 2011 album Dive out of San Francisco"
	metaClause := buildMetaClause(album, year, origin)

	if artist == "" && title == "" {
		switch persona.ID {
		case "retro-80s":
			return fmt.Sprintf("You're locked into %s on the halpradio FM dial. Keep that stereo cranked while we roll into the next cut.", station)
		case "cyberpunk":
			return fmt.Sprintf("Signal locked to %s on the halpradio night grid. Stay wired for the next transmission.", station)
		case "bbc-presenter":
			return fmt.Sprintf("You're listening to %s via halpradio. Plenty more hand-picked selections on the way.", station)
		default:
			return fmt.Sprintf("You're locked into halpradio on %s. Sit back, relax, and enjoy the flow.", station)
		}
	}

	trackPhrase := title
	if artist != "" && title != "" {
		trackPhrase = fmt.Sprintf("%s with '%s'", artist, title)
	} else if artist != "" {
		trackPhrase = artist
	} else {
		trackPhrase = fmt.Sprintf("'%s'", title)
	}

	switch persona.ID {
	case "retro-80s":
		closers := []string{
			"Turn up the analog dial and let it ride!",
			"Pure high-voltage FM energy right here on halpradio.",
			"Keep your dial locked right where it is!",
		}
		closer := closers[variant%len(closers)]
		return fmt.Sprintf("Coming at you live on %s via halpradio! Up next is %s%s. %s", station, trackPhrase, metaClause, closer)

	case "cyberpunk":
		closers := []string{
			"Stay jacked into the night frequency.",
			"Zero cloud lag, pure chrome signal across the grid.",
			"Keep your neural link tuned to the underground.",
		}
		closer := closers[variant%len(closers)]
		return fmt.Sprintf("Pirate signal locked to %s on halpradio. Incoming transmission: %s%s. %s", station, trackPhrase, metaClause, closer)

	case "bbc-presenter":
		closers := []string{
			"A remarkable piece of production—do turn this one up.",
			"Essential listening for anyone following the %s sound.",
			"Lovely textures throughout this recording.",
		}
		closer := closers[variant%len(closers)]
		if strings.Contains(closer, "%s") {
			g := genre
			if g == "" {
				g = "independent"
			}
			closer = fmt.Sprintf(closer, strings.ToLower(g))
		}
		return fmt.Sprintf("You're listening to %s on halpradio. That brings us to %s%s. %s", station, trackPhrase, metaClause, closer)

	default: // "late-night-chill"
		closers := []string{
			"Sit back and enjoy the flow.",
			"Let the late-night frequencies carry you through.",
			"Settle in and let this one breathe.",
		}
		closer := closers[variant%len(closers)]
		return fmt.Sprintf("You're locked into halpradio on %s. Up next is %s%s. %s", station, trackPhrase, metaClause, closer)
	}
}

func buildMetaClause(album, year, origin string) string {
	var parts []string
	if album != "" && year != "" {
		parts = append(parts, fmt.Sprintf("from the %s album %s", year, album))
	} else if album != "" {
		parts = append(parts, fmt.Sprintf("from the album %s", album))
	} else if year != "" {
		parts = append(parts, fmt.Sprintf("first released in %s", year))
	}

	if origin != "" {
		parts = append(parts, fmt.Sprintf("out of %s", origin))
	}
	if len(parts) == 0 {
		return ""
	}
	return ", " + strings.Join(parts, " ")
}

func fallbackStr(val, def string) string {
	val = strings.TrimSpace(val)
	if val == "" {
		return def
	}
	return val
}
