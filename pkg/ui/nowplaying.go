package ui

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/halpworld/halpradio/pkg/art"
	"github.com/halpworld/halpradio/pkg/lyrics"
	"github.com/halpworld/halpradio/pkg/player"
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
	m.LyricsSheet = nil
	m.LyricsStatus = ""
	m.LyricsScroll = 0
	m.LyricsTrackKey = ""
	m.IsFetchingLyrics = false
	m.Cover = nil
	m.ArtLines = nil
	m.ArtStatus = ""
	m.ArtTrackKey = ""
	m.IsFetchingArt = false
	m.TrackStartTime = time.Time{}
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

	artist, title, _ := m.nowPlayingParts()
	if artist == "" || title == "" {
		// Station is broadcasting its own name or an advert slug; there is
		// nothing a lyrics or artwork provider could match on.
		return nil
	}
	key := artist + " - " + title
	if m.TrackStartTime.IsZero() {
		m.TrackStartTime = time.Now()
	}

	var cmds []tea.Cmd
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
	// Drawer border, its own header block, its footer and the minimum sheet.
	const drawerRows = 2 + 5 + 3 + 7
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
