package ui

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/halpworld/halpradio/pkg/ai"
	"github.com/halpworld/halpradio/pkg/art"
	"github.com/halpworld/halpradio/pkg/lyrics"
	"github.com/halpworld/halpradio/pkg/player"
	"github.com/halpworld/halpradio/pkg/radio"
	"github.com/halpworld/halpradio/pkg/util"
)

// LyricsFetchedMsg carries the result of an asynchronous lyric lookup.
type LyricsFetchedMsg struct {
	StationID string
	TrackKey  string
	Sheet     *lyrics.Sheet
	Err       error
}

// CoverArtFetchedMsg carries the result of an asynchronous cover art lookup.
type CoverArtFetchedMsg struct {
	StationID string
	TrackKey  string
	Cover     *art.Cover
	Err       error
}

// LinerNotesFetchedMsg carries the result of an asynchronous MusicBrainz + Wikipedia liner notes lookup.
type LinerNotesFetchedMsg struct {
	StationID string
	TrackKey  string
	Notes     *ai.LinerNotes
	Err       error
}

// DJIntroGeneratedMsg carries a generated Local AI Radio DJ intro script and TTS engine label.
type DJIntroGeneratedMsg struct {
	Seq       uint64
	StationID string
	TrackKey  string
	Persona   string
	Engine    string
	Script    string
	Err       error
}

// DJSpeechFinishedMsg signals that the local TTS engine finished reading a DJ intro.
type DJSpeechFinishedMsg struct {
	Seq      uint64
	TrackKey string
	Script   string
	Err      error
}

// DJUnduckFinishedMsg clears the DJ broadcast banner after the audio volume unduck completes.
type DJUnduckFinishedMsg struct {
	Seq    uint64
	Script string
}

// DJVoiceDownloadedMsg reports the result of an opt-in HuggingFace ONNX voice model download.
type DJVoiceDownloadedMsg struct {
	ModelID string
	Err     error
}

// lyricsSyncStep is how far one press of the sync nudge keys shifts playback.
const lyricsSyncStep = 500 * time.Millisecond

// nowPlayingTrack returns the best available "Artist - Title" string for the
// track currently on air, preferring an acoustic identification over the raw
// ICY metadata broadcast by the station.
func (m Model) nowPlayingTrack() string {
	if m.IdentifiedResult != nil {
		if s := m.IdentifiedResult.SimpleTitle(); s != "" {
			return s
		}
	}
	return strings.TrimSpace(m.Player.CurrentTrack())
}

// nowPlayingParts splits the current track into artist, title and album,
// falling back to the acoustic fingerprint fields when metadata is thin.
func (m Model) nowPlayingParts() (artist, title, album string) {
	if m.IdentifiedResult != nil {
		artist = m.IdentifiedResult.Artist
		title = m.IdentifiedResult.Title
		album = m.IdentifiedResult.Album
	}
	if artist == "" || title == "" {
		a, t := lyrics.SplitTrackTitle(m.Player.CurrentTrack())
		if a != "" {
			artist = a
		}
		if t != "" {
			title = t
		}
	}
	return artist, title, album
}

// lyricElapsed estimates how far into the current track playback has reached.
// Internet radio exposes no seek position, so the clock starts when the
// station first announced this title and the user can nudge it with , and .
func (m Model) lyricElapsed() time.Duration {
	if m.TrackStartTime.IsZero() {
		return 0
	}
	if m.Player.Status() != player.StatusPlaying {
		return 0
	}
	el := time.Since(m.TrackStartTime) + m.LyricsOffset
	if el < 0 {
		return 0
	}
	return el
}

// nowPlayingSignature identifies the station and track currently on air. A
// change in the signature is what triggers a fresh lyric and artwork lookup.
func (m Model) nowPlayingSignature() string {
	st := m.Player.CurrentStation()
	if st == nil {
		return ""
	}
	if m.Player.Status() != player.StatusPlaying && m.Player.Status() != player.StatusConnecting {
		return ""
	}
	ident := ""
	if m.IdentifiedResult != nil {
		ident = m.IdentifiedResult.SimpleTitle()
	}
	return st.ID + "\x1f" + m.Player.CurrentTrack() + "\x1f" + ident
}

// coverSourceLabel names the provider the held artwork came from.
func (m Model) coverSourceLabel() string {
	if m.Cover == nil {
		return ""
	}
	return m.Cover.Source
}

// resetNowPlaying drops the lyric sheet and artwork held for the previous
// track. It is called whenever the station or announced title changes.
func (m *Model) resetNowPlaying() {
	hadArt := len(m.ArtLines) > 0
	m.LyricsSheet = nil
	m.LyricsStatus = ""
	m.LyricsScroll = 0
	m.LyricsTrackKey = ""
	m.IsFetchingLyrics = false
	m.Cover = nil
	m.ArtStatus = ""
	m.ArtTrackKey = ""
	m.IsFetchingArt = false
	m.LinerNotes = nil
	m.LinerNotesStatus = ""
	m.LinerNotesScroll = 0
	m.LinerNotesTrackKey = ""
	m.IsFetchingLinerNotes = false
	m.TrackStartTime = time.Time{}
	m.dropArt(hadArt)
}

// syncNowPlaying starts any lookups the current track still needs and returns
// the commands to run them. It is safe to call on every metadata update: a
// track already fetched or already in flight produces no commands.
func (m *Model) syncNowPlaying() []tea.Cmd {
	st := m.Player.CurrentStation()
	if st == nil {
		return nil
	}
	if m.Player.Status() != player.StatusPlaying && m.Player.Status() != player.StatusConnecting {
		return nil
	}

	var cmds []tea.Cmd
	artist, title, _ := m.nowPlayingParts()
	if artist == "" || title == "" {
		// Station is broadcasting its own name or an advert slug; lyrics and
		// cover art require a song match, while Liner Notes and AI DJ can still
		// synthesize station-level context.
		stKey := "station|" + st.ID
		if m.ShowLinerNotes && !m.IsFetchingLinerNotes && m.LinerNotesTrackKey != stKey {
			m.IsFetchingLinerNotes = true
			m.LinerNotesTrackKey = stKey
			m.LinerNotesScroll = 0
			m.LinerNotesStatus = "Synthesizing station liner notes…"
			cmds = append(cmds, m.fetchLinerNotesCmd(st.ID, stKey))
		}
		if m.Config.AIDJEnabled && m.Player.Status() == player.StatusPlaying && !m.DJBannerActive && m.DJAnnouncedTrackKey != stKey {
			m.DJAnnouncedTrackKey = stKey
			cmds = append(cmds, m.startDJAnnouncement(false)...)
		}
		return cmds
	}
	key := artist + " - " + title
	if m.TrackStartTime.IsZero() {
		m.TrackStartTime = time.Now()
	}

	if m.Config.LyricsEnabled && m.LyricsClient != nil &&
		!m.IsFetchingLyrics && m.LyricsTrackKey != key {
		m.IsFetchingLyrics = true
		m.LyricsTrackKey = key
		m.LyricsSheet = nil
		m.LyricsScroll = 0
		m.LyricsStatus = "Searching LRCLIB for synced lyrics…"
		cmds = append(cmds, m.fetchLyricsCmd(st.ID, key))
	}
	if m.Config.AlbumArtEnabled && m.ArtClient != nil && m.ArtProtocol != art.ProtocolNone &&
		!m.IsFetchingArt && m.ArtTrackKey != key {
		m.IsFetchingArt = true
		m.ArtTrackKey = key
		m.Cover = nil
		m.ArtLines = nil
		m.ArtStatus = "Fetching cover art…"
		cmds = append(cmds, m.fetchCoverCmd(st.ID, key))
	}
	if m.ShowLinerNotes && !m.IsFetchingLinerNotes && m.LinerNotesTrackKey != key {
		m.IsFetchingLinerNotes = true
		m.LinerNotesTrackKey = key
		m.LinerNotes = m.initialLinerNotes()
		m.LinerNotesScroll = 0
		m.LinerNotesStatus = "Fetching MusicBrainz + Wikipedia liner notes…"
		cmds = append(cmds, m.fetchLinerNotesCmd(st.ID, key))
	}
	if m.Config.AIDJEnabled && m.DJAnnouncedTrackKey != key {
		m.DJAnnouncedTrackKey = key
		cmds = append(cmds, m.startDJAnnouncement(false)...)
	}
	return cmds
}

// fetchLyricsCmd looks up a lyric sheet off the UI thread.
func (m Model) fetchLyricsCmd(stationID, key string) tea.Cmd {
	client := m.LyricsClient
	if client == nil {
		return nil
	}
	artist, title, album := m.nowPlayingParts()
	var dur time.Duration
	if m.IdentifiedResult != nil && m.IdentifiedResult.Duration > 0 {
		dur = time.Duration(m.IdentifiedResult.Duration * float64(time.Second))
	}

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()

		sheet, err := client.Fetch(ctx, artist, title, album, dur)
		return LyricsFetchedMsg{
			StationID: stationID,
			TrackKey:  key,
			Sheet:     sheet,
			Err:       err,
		}
	}
}

// fetchCoverCmd downloads cover artwork off the UI thread.
func (m Model) fetchCoverCmd(stationID, key string) tea.Cmd {
	client := m.ArtClient
	if client == nil {
		return nil
	}
	artist, title, album := m.nowPlayingParts()

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		cover, err := client.Fetch(ctx, artist, title, album)
		return CoverArtFetchedMsg{
			StationID: stationID,
			TrackKey:  key,
			Cover:     cover,
			Err:       err,
		}
	}
}

// LyricsDrawerMinWidth is the narrowest terminal that can host the drawer
// alongside the station list, which itself will not render below 28 columns
// next to an 18 column sidebar. Below this the sheet takes over the content
// area instead, so L always shows something.
const LyricsDrawerMinWidth = 80

// LyricsDrawerWidth returns how many columns the lyrics drawer occupies for a
// given terminal width, or 0 when the terminal cannot host it beside the
// station list.
func LyricsDrawerWidth(width int) int {
	if width < LyricsDrawerMinWidth {
		return 0
	}
	w := width * 2 / 5
	if w > 56 {
		w = 56
	}
	if w < 32 {
		w = 32
	}
	return w
}

// LyricsSurface says where the lyric sheet is drawn at a given terminal size.
type LyricsSurface int

const (
	// LyricsSurfaceHidden means the sheet is not on screen.
	LyricsSurfaceHidden LyricsSurface = iota
	// LyricsSurfaceDrawer puts the sheet beside the station list.
	LyricsSurfaceDrawer
	// LyricsSurfaceOverlay gives the sheet the whole content area, for
	// terminals too narrow to show both.
	LyricsSurfaceOverlay
)

// lyricsSurface reports where the sheet goes and how many columns it gets.
func (m Model) lyricsSurface() (LyricsSurface, int) {
	if !m.ShowLyrics {
		return LyricsSurfaceHidden, 0
	}
	width := m.Width
	if width == 0 {
		width = 80
	}
	if drawer := LyricsDrawerWidth(width); drawer > 0 {
		return LyricsSurfaceDrawer, drawer
	}
	return LyricsSurfaceOverlay, width
}

// artTarget returns the cell dimensions artwork should be rendered at for the
// currently visible surface, or zeroes when artwork has nowhere to go.
func (m Model) artTarget() (cols, rows int) {
	width, height := m.Width, m.Height
	if width == 0 || height == 0 {
		width, height = 80, 24
	}

	if m.ShowArtModal {
		cols = width - 16
		if cols > 60 {
			cols = 60
		}
		maxRows := height - 12
		if maxRows < 6 {
			maxRows = 6
		}
		if rows = cellRowsFor(cols); rows > maxRows {
			rows = maxRows
			cols = cellColsFor(rows)
		}
		if cols < 12 {
			return 0, 0
		}
		return cols, rows
	}

	surface, surfaceWidth := m.lyricsSurface()
	if surface == LyricsSurfaceHidden {
		surface, surfaceWidth = m.linerNotesSurface()
	}
	if surface == LyricsSurfaceHidden {
		return 0, 0
	}
	cols = surfaceWidth - 6
	if cols > 20 {
		cols = 20
	}
	rows = cellRowsFor(cols)

	// The sheet is the point of the drawer, so the thumbnail only gets the
	// rows left over once the chrome and a readable run of lyrics are paid
	// for. On a short terminal it is dropped entirely; A still shows it full
	// size.
	budget := drawerArtBudget(height)
	if budget < drawerArtMinRows {
		return 0, 0
	}
	if rows > budget {
		rows = budget
		cols = cellColsFor(rows)
	}
	if cols < 8 {
		return 0, 0
	}
	return cols, rows
}

// drawerArtMinRows is the smallest thumbnail worth drawing in the drawer.
const drawerArtMinRows = 6

// drawerArtBudget returns how many rows of the drawer are free for artwork
// once the frame, the header, the footer and a readable run of lyric lines
// have been accounted for.
func drawerArtBudget(termHeight int) int {
	// Header, player bar, status bar and the spacer between them.
	const chromeRows = 12
	// Drawer border, its own header block with separator, its footer and the minimum sheet.
	const drawerRows = 2 + 7 + 3 + 7
	return termHeight - chromeRows - drawerRows
}

// cellRowsFor returns the row count that renders cols columns as a square,
// assuming a terminal cell is twice as tall as it is wide.
func cellRowsFor(cols int) int {
	rows := int(math.Round(float64(cols) / 2.0))
	if rows < 1 {
		rows = 1
	}
	return rows
}

// cellColsFor is the inverse of cellRowsFor.
func cellColsFor(rows int) int {
	cols := rows * 2
	if cols < 1 {
		cols = 1
	}
	return cols
}

// renderArt re-rasterises the held cover for the current surface size. It is
// pure CPU work on already-downloaded bytes, kept in the update loop so the
// component views stay side-effect free.
func (m *Model) renderArt() {
	had := len(m.ArtLines) > 0
	cols, rows := m.artTarget()
	if m.Cover == nil || m.ArtRenderer == nil || cols == 0 || rows == 0 {
		m.dropArt(had)
		return
	}
	if m.ArtCols == cols && m.ArtRows == rows && len(m.ArtLines) > 0 {
		return
	}
	lines, err := m.ArtRenderer.Render(m.Cover.Data, cols, rows)
	if err != nil {
		m.dropArt(had)
		m.ArtStatus = "Cover art could not be rendered"
		return
	}
	m.ArtLines = lines
	m.ArtCols, m.ArtRows = cols, rows
	m.ArtClearFrames = 0
}

// artClearFrameCount is how many frames carry the image-delete escape after
// artwork disappears, enough to survive a partial repaint.
const artClearFrameCount = 3

// dropArt forgets the rasterised artwork, scheduling the protocol's
// image-delete escape when something was actually on screen.
func (m *Model) dropArt(hadLines bool) {
	m.ArtLines = nil
	m.ArtCols, m.ArtRows = 0, 0
	if hadLines && m.ArtRenderer != nil && m.ArtRenderer.Clear() != "" {
		m.ArtClearFrames = artClearFrameCount
	}
}

// ArtClearSequence returns the escape that evicts a lingering terminal image,
// or an empty string when there is nothing to evict.
func (m Model) ArtClearSequence() string {
	if m.ArtClearFrames <= 0 || m.ArtRenderer == nil {
		return ""
	}
	return m.ArtRenderer.Clear()
}

// toggleLyricsDrawer opens or closes the lyric drawer, moving keyboard focus
// into it so j/k scroll the sheet, and returns any lookup it kicked off.
func (m *Model) toggleLyricsDrawer() []tea.Cmd {
	if !m.Config.LyricsEnabled {
		m.StatusMessage = "Lyrics are disabled (set lyrics_enabled: true in config.yaml)"
		return nil
	}
	if m.ShowLyrics {
		m.ShowLyrics = false
		if m.ActiveFocus == FocusLyrics {
			m.ActiveFocus = FocusMainList
		}
		m.renderArt()
		return nil
	}
	m.ShowLyrics = true
	if m.ShowLinerNotes {
		m.ShowLinerNotes = false
	}
	m.ActiveFocus = FocusLyrics
	cmds := m.syncNowPlaying()
	m.renderArt()
	if m.LyricsSheet == nil && !m.IsFetchingLyrics && m.LyricsStatus == "" {
		if m.Player.Status() != player.StatusPlaying {
			m.LyricsStatus = "Start a station to pull in its lyrics"
		} else {
			m.LyricsStatus = "Waiting for track metadata from the stream…"
		}
	}
	return cmds
}

// toggleArtModal opens or closes the full-size album art modal.
func (m *Model) toggleArtModal() []tea.Cmd {
	if !m.Config.AlbumArtEnabled || m.ArtProtocol == art.ProtocolNone {
		m.StatusMessage = "Album art is disabled (set album_art_enabled: true in config.yaml)"
		return nil
	}
	if m.ShowArtModal {
		m.ShowArtModal = false
		m.renderArt()
		return nil
	}

	m.ShowArtModal = true
	cmds := m.syncNowPlaying()
	m.renderArt()
	if m.Cover == nil && !m.IsFetchingArt && m.ArtStatus == "" {
		m.ArtStatus = "Waiting for track metadata from the stream…"
	}
	return cmds
}

// scrollLyrics moves the manual scroll offset used for unsynced sheets.
func (m *Model) scrollLyrics(delta int) {
	if m.LyricsSheet == nil {
		return
	}
	m.LyricsScroll += delta
	maxScroll := len(m.LyricsSheet.Lines) - 1
	if maxScroll < 0 {
		maxScroll = 0
	}
	if m.LyricsScroll > maxScroll {
		m.LyricsScroll = maxScroll
	}
	if m.LyricsScroll < 0 {
		m.LyricsScroll = 0
	}
}

// nudgeLyricsSync shifts the estimated track start, correcting for stations
// that announce metadata late or early.
func (m *Model) nudgeLyricsSync(delta time.Duration) {
	m.LyricsOffset += delta
	m.Config.LyricsOffsetMs = int(m.LyricsOffset / time.Millisecond)
	// Stations tend to be consistently late or early, so the correction is
	// worth carrying across restarts.
	_ = util.SaveConfig(m.Config)
	m.StatusMessage = "Lyric sync offset " + formatOffset(m.LyricsOffset)
}

// formatOffset renders a signed sync offset for the status bar.
func formatOffset(d time.Duration) string {
	secs := d.Round(100 * time.Millisecond).Seconds()
	if secs >= 0 {
		return fmt.Sprintf("+%.1fs", secs)
	}
	return fmt.Sprintf("%.1fs", secs)
}

// linerNotesSurface reports where the liner notes drawer goes and how many columns it gets.
func (m Model) linerNotesSurface() (LyricsSurface, int) {
	if !m.ShowLinerNotes {
		return LyricsSurfaceHidden, 0
	}
	width := m.Width
	if width == 0 {
		width = 80
	}
	if drawer := LyricsDrawerWidth(width); drawer > 0 {
		return LyricsSurfaceDrawer, drawer
	}
	return LyricsSurfaceOverlay, width
}

// activeOrSelectedStation returns the currently playing station, or the highlighted
// station in the current list if nothing is playing yet.
func (m Model) activeOrSelectedStation() *radio.Station {
	if m.Player != nil {
		if st := m.Player.CurrentStation(); st != nil {
			return st
		}
	}
	if len(m.Stations) > 0 && m.SelectedIndex >= 0 && m.SelectedIndex < len(m.Stations) {
		s := m.Stations[m.SelectedIndex]
		return &s
	}
	return nil
}

// initialLinerNotes constructs an immediate offline LinerNotes record from the
// active station and track so the drawer renders rich content on frame 0 while
// MusicBrainz + Wikipedia lookups are in flight.
func (m Model) initialLinerNotes() *ai.LinerNotes {
	st := m.activeOrSelectedStation()
	artist, title, album := m.nowPlayingParts()
	rawTrack := m.nowPlayingTrack()
	if st == nil && artist == "" && title == "" && rawTrack == "" {
		return nil
	}
	var stName, stGenre, stCountry, stCodec string
	var stBitrate int
	if st != nil {
		stName = st.Name
		stGenre = st.Genre
		stCountry = st.Country
		stCodec = st.Codec
		stBitrate = st.Bitrate
	}
	if artist == "" {
		artist = stName
	}
	if title == "" {
		if rawTrack != "" {
			title = rawTrack
		} else if stName != "" {
			title = stName + " Live Broadcast"
		} else {
			title = "Live Broadcast"
		}
	}
	bio := fmt.Sprintf("%s is currently broadcasting on %s.", artist, fallbackStationName(stName))
	if stGenre != "" && stCountry != "" {
		bio = fmt.Sprintf("%s is featured on %s, curating %s selections from %s.", artist, fallbackStationName(stName), stGenre, stCountry)
	}
	var trivia []string
	if stCodec != "" || stBitrate > 0 {
		trivia = append(trivia, fmt.Sprintf("Streaming live via %s (%s %d kbps).", fallbackStationName(stName), strings.ToUpper(stCodec), stBitrate))
	} else {
		trivia = append(trivia, fmt.Sprintf("Streaming live on %s.", fallbackStationName(stName)))
	}
	return &ai.LinerNotes{
		Artist:      artist,
		Title:       title,
		Album:       album,
		Country:     stCountry,
		Genre:       stGenre,
		Biography:   bio,
		AlbumNotes:  "Press d at any time to hear your Local AI Radio DJ introduce this track.",
		TrackTrivia: trivia,
		Source:      "Offline Station Metadata",
		FetchedAt:   time.Now(),
	}
}

func fallbackStationName(name string) string {
	if strings.TrimSpace(name) == "" {
		return "halpradio"
	}
	return strings.TrimSpace(name)
}

// toggleLinerNotesDrawer opens or closes the interactive Liner Notes & Artist
// Biography drawer (`i`), moving keyboard focus into it so j/k scroll the bio.
func (m *Model) toggleLinerNotesDrawer() []tea.Cmd {
	if m.ShowLinerNotes {
		m.ShowLinerNotes = false
		if m.ActiveFocus == FocusLinerNotes {
			m.ActiveFocus = FocusMainList
		}
		m.renderArt()
		return nil
	}
	m.ShowLinerNotes = true
	if m.ShowLyrics {
		m.ShowLyrics = false
	}
	m.ActiveFocus = FocusLinerNotes
	if m.LinerNotes == nil {
		m.LinerNotes = m.initialLinerNotes()
	}
	cmds := m.syncNowPlaying()
	if !m.IsFetchingLinerNotes {
		if st := m.activeOrSelectedStation(); st != nil {
			key := "station|" + st.ID
			if a, t, _ := m.nowPlayingParts(); a != "" && t != "" {
				key = a + " - " + t
			}
			m.IsFetchingLinerNotes = true
			m.LinerNotesTrackKey = key
			m.LinerNotesScroll = 0
			m.LinerNotesStatus = "Fetching MusicBrainz + Wikipedia liner notes…"
			cmds = append(cmds, m.fetchLinerNotesCmd(st.ID, key))
		}
	}
	m.renderArt()
	return cmds
}

// scrollLinerNotes adjusts the vertical scroll offset inside the Liner Notes drawer.
func (m *Model) scrollLinerNotes(delta int) {
	m.LinerNotesScroll += delta
	maxScroll := 40
	if m.LinerNotes != nil {
		maxScroll = 24 + len(m.LinerNotes.Discography)*2 + len(m.LinerNotes.TrackTrivia)*3
	}
	if m.LinerNotesScroll > maxScroll {
		m.LinerNotesScroll = maxScroll
	}
	if m.LinerNotesScroll < 0 {
		m.LinerNotesScroll = 0
	}
}

// fetchLinerNotesCmd queries MusicBrainz + Wikipedia (with local cache and offline
// synthesis fallback) off the UI thread.
func (m Model) fetchLinerNotesCmd(stationID, key string) tea.Cmd {
	client := m.LinerNotesClient
	if client == nil {
		client = ai.NewLinerNotesClient()
	}
	artist, title, album := m.nowPlayingParts()
	rawTrack := m.nowPlayingTrack()
	st := m.activeOrSelectedStation()
	var stName, stGenre, stCountry, stCodec string
	var stBitrate int
	if st != nil {
		stName = st.Name
		stGenre = st.Genre
		stCountry = st.Country
		stCodec = st.Codec
		stBitrate = st.Bitrate
	}
	q := ai.LinerNotesQuery{
		RawTrack:    rawTrack,
		Artist:      artist,
		Title:       title,
		Album:       album,
		StationName: stName,
		Genre:       stGenre,
		Country:     stCountry,
		Bitrate:     stBitrate,
		Codec:       stCodec,
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()

		notes, err := client.Fetch(ctx, q)
		return LinerNotesFetchedMsg{
			StationID: stationID,
			TrackKey:  key,
			Notes:     notes,
			Err:       err,
		}
	}
}

// ensureDJ makes sure m.DJ is initialized and synchronized with m.Config.
func (m *Model) ensureDJ() *ai.DJ {
	if m.DJ == nil {
		m.DJ = ai.NewDJ(ai.DJConfig{
			PersonaID:   m.Config.AIDJPersona,
			TTSEngine:   m.Config.AIDJTTSEngine,
			VoiceModel:  m.Config.AIDJVoiceModel,
			OllamaURL:   m.Config.AIDJOllamaURL,
			OllamaModel: m.Config.AIDJOllamaModel,
			DuckLevel:   m.Config.AIDJDuckLevel,
		})
	} else {
		m.DJ.SetPersona(m.Config.AIDJPersona)
		m.DJ.SetTTSEngine(m.Config.AIDJTTSEngine, m.Config.AIDJVoiceModel)
	}
	return m.DJ
}

// refreshDJInstalledVoices updates m.DJInstalledVoices in the update loop so View()
// stays 100% pure and never touches the filesystem.
func (m *Model) refreshDJInstalledVoices() {
	models := ai.DefaultVoiceModels()
	installed := make(map[string]bool, len(models))
	for _, spec := range models {
		installed[spec.ID] = ai.IsModelInstalled(spec.ID)
	}
	m.DJInstalledVoices = installed
}

// cancelDJAnnouncement aborts any in-flight DJ speech, increments the sequence
// token so stale messages are ignored, hides the DJ banner, and unducks volume.
func (m *Model) cancelDJAnnouncement() {
	m.DJAnnounceSeq++
	if m.DJ != nil {
		m.DJ.Cancel()
	}
	m.DJBannerActive = false
	m.DJBannerFading = false
	m.DJBannerUntil = time.Time{}
	if m.Player != nil && m.Player.DuckFactor() < 1.0 {
		m.Player.Unduck(0)
	}
}

// buildDJTrackContext gathers station, ICY/fingerprint track metadata, and cached
// LinerNotes context for a DJ intro announcement.
func (m Model) buildDJTrackContext() (ai.TrackContext, string, string) {
	st := m.activeOrSelectedStation()
	var stID, stName, stGenre, stCountry string
	if st != nil {
		stID = st.ID
		stName = st.Name
		stGenre = st.Genre
		stCountry = st.Country
	}
	artist, title, album := m.nowPlayingParts()
	tc := ai.TrackContext{
		StationName: stName,
		Genre:       stGenre,
		Country:     stCountry,
		Artist:      artist,
		Title:       title,
		Album:       album,
	}
	if m.LinerNotes != nil {
		if tc.Album == "" {
			tc.Album = m.LinerNotes.Album
		}
		tc.ReleaseYear = m.LinerNotes.ReleaseYear
		tc.OriginCity = m.LinerNotes.OriginCity
		if len(m.LinerNotes.TrackTrivia) > 0 {
			tc.Trivia = m.LinerNotes.TrackTrivia[0]
		}
	}
	trackKey := "station|" + stID
	if artist != "" || title != "" {
		trackKey = artist + " - " + title
	}
	return tc, stID, trackKey
}

// startDJAnnouncement immediately ducks playback volume, activates the DJ
// broadcast banner with an initial persona script, and dispatches asynchronous
// script generation + local TTS synthesis.
func (m *Model) startDJAnnouncement(onDemand bool) []tea.Cmd {
	if !onDemand && m.Player != nil && m.Player.Status() != player.StatusPlaying && m.Player.Status() != player.StatusConnecting {
		return nil
	}
	dj := m.ensureDJ()
	// Cancel any previous announcement in flight and advance the generation sequence token.
	dj.Cancel()
	m.DJAnnounceSeq++
	seq := m.DJAnnounceSeq

	persona := dj.Persona()
	tc, stID, trackKey := m.buildDJTrackContext()
	m.DJAnnouncedTrackKey = trackKey
	if sig := m.nowPlayingSignature(); sig != "" {
		m.nowPlayingSig = sig
	}

	// Immediately duck playback volume so live audio and synchronous callers
	// see the ducked level right away.
	if m.Player != nil {
		m.Player.Duck(dj.DuckRatio(), 0)
	}

	// Populate an immediate zero-latency offline preview script so the banner
	// is never blank while async Ollama/TTS generation runs.
	previewDJ := ai.NewDJ(ai.DJConfig{
		PersonaID:  persona.ID,
		TTSEngine:  m.Config.AIDJTTSEngine,
		VoiceModel: m.Config.AIDJVoiceModel,
		DuckLevel:  m.Config.AIDJDuckLevel,
	})
	previewScript, engineLabel, _ := previewDJ.GenerateIntro(context.Background(), tc)

	m.DJBannerActive = true
	m.DJBannerFading = false
	m.DJBannerPersona = persona.Name
	m.DJBannerEngine = engineLabel
	m.DJBannerScript = previewScript
	m.DJBannerUntil = time.Now().Add(30 * time.Second)
	m.StatusMessage = fmt.Sprintf("🎙️ AI DJ (%s): broadcasting track intro…", persona.Name)

	return []tea.Cmd{m.triggerDJIntroCmd(seq, stID, trackKey, tc)}
}

// triggerDJIntroCmd generates the DJ intro script (consulting local Ollama if
// configured, or offline persona templates) off the UI thread.
func (m Model) triggerDJIntroCmd(seq uint64, stationID, trackKey string, tc ai.TrackContext) tea.Cmd {
	dj := m.DJ
	if dj == nil {
		dj = ai.NewDJ(ai.DJConfig{
			PersonaID:   m.Config.AIDJPersona,
			TTSEngine:   m.Config.AIDJTTSEngine,
			VoiceModel:  m.Config.AIDJVoiceModel,
			OllamaURL:   m.Config.AIDJOllamaURL,
			OllamaModel: m.Config.AIDJOllamaModel,
			DuckLevel:   m.Config.AIDJDuckLevel,
		})
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		script, label, err := dj.GenerateIntro(ctx, tc)
		return DJIntroGeneratedMsg{
			Seq:       seq,
			StationID: stationID,
			TrackKey:  trackKey,
			Persona:   dj.Persona().Name,
			Engine:    label,
			Script:    script,
			Err:       err,
		}
	}
}

// speakDJScriptCmd runs the local TTS engine off the UI thread and returns
// DJSpeechFinishedMsg when speech synthesis finishes so the player can unduck.
func (m Model) speakDJScriptCmd(seq uint64, trackKey, script string) tea.Cmd {
	dj := m.DJ
	if dj == nil {
		return func() tea.Msg {
			return DJSpeechFinishedMsg{Seq: seq, TrackKey: trackKey, Script: script}
		}
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		err := dj.Speak(ctx, script)
		return DJSpeechFinishedMsg{
			Seq:      seq,
			TrackKey: trackKey,
			Script:   script,
			Err:      err,
		}
	}
}

// unduckAfterDelayCmd keeps the DJ banner in its "Fading to 100%" state briefly
// after speech finishes before hiding the banner.
func (m Model) unduckAfterDelayCmd(seq uint64, script string) tea.Cmd {
	return tea.Tick(350*time.Millisecond, func(t time.Time) tea.Msg {
		return DJUnduckFinishedMsg{Seq: seq, Script: script}
	})
}

// downloadVoiceModelCmd downloads an opt-in ONNX voice model from HuggingFace
// off the UI thread when explicitly requested by the user in the DJ Studio modal.
func (m Model) downloadVoiceModelCmd(modelID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		err := ai.DownloadVoiceModel(ctx, modelID, nil)
		return DJVoiceDownloadedMsg{
			ModelID: modelID,
			Err:     err,
		}
	}
}
