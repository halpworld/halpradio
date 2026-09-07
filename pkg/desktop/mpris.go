package desktop

import (
	"fmt"
	"strings"
	"sync"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/prop"
	"github.com/halpworld/halpradio/pkg/debuglog"
)

const (
	mprisObjectPath = "/org/mpris/MediaPlayer2"
	mprisInterface  = "org.mpris.MediaPlayer2"
	playerInterface = "org.mpris.MediaPlayer2.Player"
	mprisBusName    = "org.mpris.MediaPlayer2.halpradio"
)

// MPRISHandler defines the callbacks triggered by remote MPRIS D-Bus method calls.
type MPRISHandler struct {
	OnPlayPause func()
	OnPlay      func()
	OnPause     func()
	OnStop      func()
	OnNext      func()
	OnPrev      func()
	OnVolume    func(vol float64)
	OnQuit      func()
}

// propWriter is the subset of *prop.Properties used to publish server-side
// property updates. It exists so tests can substitute a fake implementation.
type propWriter interface {
	SetMust(iface, property string, v any)
}

// MPRISServer manages the MPRIS v2 D-Bus daemon.
type MPRISServer struct {
	mu           sync.Mutex
	conn         *dbus.Conn
	props        propWriter
	handler      MPRISHandler
	status       string
	stationName  string
	stationGenre string
	streamURL    string
	trackTitle   string
	volume       float64
	closed       bool
}

// MapStatusToMPRIS converts halpradio status to MPRIS PlaybackStatus.
func MapStatusToMPRIS(status string) string {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "PLAYING":
		return "Playing"
	case "PAUSED":
		return "Paused"
	case "STOPPED", "ERROR", "":
		return "Stopped"
	default:
		return "Stopped"
	}
}

// BuildMPRISMetadata constructs the standard MPRIS metadata dictionary.
func BuildMPRISMetadata(stationName, genre, trackTitle, streamURL string) map[string]any {
	title := strings.TrimSpace(trackTitle)
	artist := ""
	album := strings.TrimSpace(stationName)

	if title == "" {
		title = stationName
	} else if strings.Contains(title, " - ") {
		parts := strings.SplitN(title, " - ", 2)
		artist = strings.TrimSpace(parts[0])
		title = strings.TrimSpace(parts[1])
	}

	var artists []string
	if artist != "" {
		artists = []string{artist}
	} else if stationName != "" {
		artists = []string{stationName}
	}

	meta := map[string]any{
		"mpris:trackid": dbus.ObjectPath("/org/mpris/MediaPlayer2/Track/1"),
		"xesam:title":   title,
		"xesam:url":     streamURL,
	}

	if len(artists) > 0 {
		meta["xesam:artist"] = artists
	}
	if album != "" {
		meta["xesam:album"] = album
	}
	if genre != "" {
		meta["xesam:genre"] = []string{genre}
	}

	return meta
}

// Root MPRIS object methods
type mprisRoot struct {
	server *MPRISServer
}

func (r *mprisRoot) Raise() *dbus.Error {
	return nil
}

func (r *mprisRoot) Quit() *dbus.Error {
	if r.server != nil && r.server.handler.OnQuit != nil {
		r.server.handler.OnQuit()
	}
	return nil
}

// Player MPRIS object methods
type mprisPlayer struct {
	server *MPRISServer
}

func (p *mprisPlayer) Next() *dbus.Error {
	if p.server != nil && p.server.handler.OnNext != nil {
		p.server.handler.OnNext()
	}
	return nil
}

func (p *mprisPlayer) Previous() *dbus.Error {
	if p.server != nil && p.server.handler.OnPrev != nil {
		p.server.handler.OnPrev()
	}
	return nil
}

func (p *mprisPlayer) Pause() *dbus.Error {
	if p.server != nil && p.server.handler.OnPause != nil {
		p.server.handler.OnPause()
	}
	return nil
}

func (p *mprisPlayer) PlayPause() *dbus.Error {
	if p.server != nil && p.server.handler.OnPlayPause != nil {
		p.server.handler.OnPlayPause()
	}
	return nil
}

func (p *mprisPlayer) Stop() *dbus.Error {
	if p.server != nil && p.server.handler.OnStop != nil {
		p.server.handler.OnStop()
	}
	return nil
}

func (p *mprisPlayer) Play() *dbus.Error {
	if p.server != nil && p.server.handler.OnPlay != nil {
		p.server.handler.OnPlay()
	}
	return nil
}

func (p *mprisPlayer) SetPosition(trackID dbus.ObjectPath, pos int64) *dbus.Error {
	return nil
}

func (p *mprisPlayer) OpenUri(uri string) *dbus.Error {
	return nil
}

// StartMPRISServer initializes and registers the MPRIS v2 D-Bus service.
func StartMPRISServer(handler MPRISHandler) (*MPRISServer, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to session bus: %w", err)
	}

	reply, err := conn.RequestName(mprisBusName, dbus.NameFlagDoNotQueue)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("failed to request D-Bus name %s: %w", mprisBusName, err)
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		_ = conn.Close()
		return nil, fmt.Errorf("name %s already taken on D-Bus", mprisBusName)
	}

	s := &MPRISServer{
		conn:    conn,
		handler: handler,
		status:  "Stopped",
		volume:  0.8,
	}

	rootObj := &mprisRoot{server: s}
	playerObj := &mprisPlayer{server: s}

	if err := conn.Export(rootObj, mprisObjectPath, mprisInterface); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("failed to export %s: %w", mprisInterface, err)
	}

	if err := conn.Export(playerObj, mprisObjectPath, playerInterface); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("failed to export %s: %w", playerInterface, err)
	}

	propMap := map[string]map[string]*prop.Prop{
		mprisInterface: {
			"CanQuit":             {Value: true, Writable: false, Emit: prop.EmitConst},
			"CanRaise":            {Value: false, Writable: false, Emit: prop.EmitConst},
			"HasTrackList":        {Value: false, Writable: false, Emit: prop.EmitConst},
			"Identity":            {Value: "halpradio", Writable: false, Emit: prop.EmitConst},
			"DesktopEntry":        {Value: "halpradio", Writable: false, Emit: prop.EmitConst},
			"SupportedUriSchemes": {Value: []string{"http", "https"}, Writable: false, Emit: prop.EmitConst},
			"SupportedMimeTypes":  {Value: []string{"audio/mpeg", "audio/aac", "audio/ogg", "audio/x-wav", "application/ogg"}, Writable: false, Emit: prop.EmitConst},
		},
		playerInterface: {
			"PlaybackStatus": {Value: s.status, Writable: false, Emit: prop.EmitTrue},
			"LoopStatus":     {Value: "None", Writable: false, Emit: prop.EmitConst},
			"Rate":           {Value: 1.0, Writable: false, Emit: prop.EmitConst},
			"Shuffle":        {Value: false, Writable: false, Emit: prop.EmitConst},
			"Metadata":       {Value: BuildMPRISMetadata("", "", "", ""), Writable: false, Emit: prop.EmitTrue},
			"Volume": {
				Value:    s.volume,
				Writable: true,
				Emit:     prop.EmitTrue,
				Callback: func(c *prop.Change) *dbus.Error {
					if v, ok := c.Value.(float64); ok {
						s.onRemoteVolume(v)
					}
					return nil
				},
			},
			"Position":      {Value: int64(0), Writable: false, Emit: prop.EmitFalse},
			"MinimumRate":   {Value: 1.0, Writable: false, Emit: prop.EmitConst},
			"MaximumRate":   {Value: 1.0, Writable: false, Emit: prop.EmitConst},
			"CanControl":    {Value: true, Writable: false, Emit: prop.EmitConst},
			"CanPlay":       {Value: true, Writable: false, Emit: prop.EmitConst},
			"CanPause":      {Value: true, Writable: false, Emit: prop.EmitConst},
			"CanSeek":       {Value: false, Writable: false, Emit: prop.EmitConst},
			"CanGoNext":     {Value: true, Writable: false, Emit: prop.EmitConst},
			"CanGoPrevious": {Value: true, Writable: false, Emit: prop.EmitConst},
		},
	}

	props, err := prop.Export(conn, mprisObjectPath, propMap)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("failed to export properties: %w", err)
	}
	s.props = props

	return s, nil
}

// onRemoteVolume records a volume change requested by a remote MPRIS client
// (playerctl, GNOME media controls, ...) and forwards it to the UI.
//
// This runs on a D-Bus handler goroutine, so it must never be invoked while
// MPRISServer.mu is already held by the caller.
func (s *MPRISServer) onRemoteVolume(v float64) {
	debuglog.Logf("mpris", "remote volume request %.2f", v)
	s.mu.Lock()
	s.volume = v
	s.mu.Unlock()
	if s.handler.OnVolume != nil {
		s.handler.OnVolume(v)
	}
}

// setProp publishes a property value and emits PropertiesChanged.
//
// It deliberately avoids prop.Properties.Set: that method is the entry point for
// remote D-Bus *clients*, so it rejects the read-only PlaybackStatus/Metadata
// properties and runs the property Callback while holding the prop package's
// internal lock. Going through it from here silently dropped every metadata
// update and, via the Volume callback, re-entered MPRISServer.mu and deadlocked
// the caller (see issue #26). SetMust writes the value directly and emits the
// signal without consulting Writable or Callback.
func setProp(props propWriter, iface, name string, v any) {
	if props == nil {
		return
	}
	// SetMust panics if the value type does not match the exported property.
	// A misbehaving D-Bus stack must never take down the TUI.
	defer func() { _ = recover() }()
	props.SetMust(iface, name, v)
}

// UpdatePlaybackState synchronizes state to MPRIS D-Bus properties and emits change signals.
func (s *MPRISServer) UpdatePlaybackState(status string, stationName, genre, trackTitle, streamURL string, volume float64) {
	if s == nil {
		return
	}

	s.mu.Lock()
	if s.closed || s.props == nil {
		s.mu.Unlock()
		return
	}

	mprisStatus := MapStatusToMPRIS(status)
	s.status = mprisStatus
	s.stationName = stationName
	s.stationGenre = genre
	s.trackTitle = trackTitle
	s.streamURL = streamURL
	s.volume = volume
	props := s.props
	s.mu.Unlock()

	meta := BuildMPRISMetadata(stationName, genre, trackTitle, streamURL)

	// Published without holding s.mu: D-Bus may call back into this server.
	debuglog.Logf("mpris", "publishing status=%s volume=%.2f", mprisStatus, volume)
	setProp(props, playerInterface, "PlaybackStatus", mprisStatus)
	setProp(props, playerInterface, "Metadata", meta)
	setProp(props, playerInterface, "Volume", volume)
}

// Close unregisters and disconnects from D-Bus.
func (s *MPRISServer) Close() error {
	if s == nil {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}
	s.closed = true

	if s.conn != nil {
		_ = s.conn.Close()
	}
	return nil
}
