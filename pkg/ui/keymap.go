package ui

import "github.com/charmbracelet/bubbles/key"

type KeyMap struct {
	Up           key.Binding
	Down         key.Binding
	Left         key.Binding
	Right        key.Binding
	Top          key.Binding
	Bottom       key.Binding
	HalfPageUp   key.Binding
	HalfPageDown key.Binding

	NextStation key.Binding
	PrevStation key.Binding

	PlayPause  key.Binding
	Stop       key.Binding
	RandomPlay key.Binding

	VolUp   key.Binding
	VolDown key.Binding
	Mute    key.Binding

	Search      key.Binding
	Clear       key.Binding
	Activity    key.Binding
	Category    key.Binding
	Favorite    key.Binding
	AddStation  key.Binding
	EditStation key.Binding
	DelStation  key.Binding
	ExportPR    key.Binding

	Visualizer key.Binding
	Theme      key.Binding
	Help       key.Binding
	Quit       key.Binding

	YankTrack  key.Binding
	OpenSearch key.Binding
	CountryTab key.Binding
	HistoryTab key.Binding
	Timer      key.Binding
	Plugins    key.Binding
}

func DefaultKeyMap() KeyMap {
	return KeyMap{
		Plugins: key.NewBinding(
			key.WithKeys("P", "ctrl+p"),
			key.WithHelp("P", "plugins manager"),
		),
		Up: key.NewBinding(
			key.WithKeys("k", "up"),
			key.WithHelp("k/↑", "up"),
		),
		Down: key.NewBinding(
			key.WithKeys("j", "down"),
			key.WithHelp("j/↓", "down"),
		),
		Left: key.NewBinding(
			key.WithKeys("h", "left"),
			key.WithHelp("h/←", "sidebar/tab"),
		),
		Right: key.NewBinding(
			key.WithKeys("l", "right"),
			key.WithHelp("l/→", "main pane"),
		),
		Top: key.NewBinding(
			key.WithKeys("g", "home"),
			key.WithHelp("g", "top"),
		),
		Bottom: key.NewBinding(
			key.WithKeys("G", "end"),
			key.WithHelp("G", "bottom"),
		),
		HalfPageUp: key.NewBinding(
			key.WithKeys("ctrl+u", "pgup"),
			key.WithHelp("ctrl+u", "page up"),
		),
		HalfPageDown: key.NewBinding(
			key.WithKeys("ctrl+d", "pgdown"),
			key.WithHelp("ctrl+d", "page down"),
		),
		NextStation: key.NewBinding(
			key.WithKeys("n", "]", "ctrl+n", "next", "nexttrack", "media_next", "medianext", "xf86audionext"),
			key.WithHelp("n/]", "next station"),
		),
		PrevStation: key.NewBinding(
			key.WithKeys("N", "[", "ctrl+p", "prev", "previous", "media_prev", "mediaprev", "xf86audioprev"),
			key.WithHelp("N/[", "prev station"),
		),
		PlayPause: key.NewBinding(
			key.WithKeys("space", "enter", "play", "pause", "playpause", "media_play_pause", "mediaplaypause", "xf86audioplay", "xf86audiopause"),
			key.WithHelp("space/enter", "play/pause"),
		),
		Stop: key.NewBinding(
			key.WithKeys("s", "x", "stop", "media_stop", "mediastop", "xf86audiostop"),
			key.WithHelp("s/x", "stop audio"),
		),
		RandomPlay: key.NewBinding(
			key.WithKeys("r", "R"),
			key.WithHelp("r", "random station"),
		),
		VolUp: key.NewBinding(
			key.WithKeys("+", "=", ">", "volume_up", "volup", "xf86audioraisevolume"),
			key.WithHelp("+/=", "vol up"),
		),
		VolDown: key.NewBinding(
			key.WithKeys("-", "_", "<", "volume_down", "voldown", "xf86audiolowervolume"),
			key.WithHelp("-", "vol down"),
		),
		Mute: key.NewBinding(
			key.WithKeys("m", "M", "0", "mute", "volume_mute", "xf86audiomute"),
			key.WithHelp("m", "mute/unmute"),
		),
		Search: key.NewBinding(
			key.WithKeys("/"),
			key.WithHelp("/", "search stations"),
		),
		Clear: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "clear/close"),
		),
		Activity: key.NewBinding(
			key.WithKeys("w"),
			key.WithHelp("w", "work/activity mode"),
		),
		Category: key.NewBinding(
			key.WithKeys("c"),
			key.WithHelp("c", "filter genre"),
		),
		Favorite: key.NewBinding(
			key.WithKeys("f"),
			key.WithHelp("f", "toggle favorite"),
		),
		AddStation: key.NewBinding(
			key.WithKeys("a"),
			key.WithHelp("a", "add station"),
		),
		EditStation: key.NewBinding(
			key.WithKeys("e"),
			key.WithHelp("e", "edit station"),
		),
		DelStation: key.NewBinding(
			key.WithKeys("d"),
			key.WithHelp("d", "delete custom station"),
		),
		ExportPR: key.NewBinding(
			key.WithKeys("p"),
			key.WithHelp("p", "export PR snippet"),
		),
		Visualizer: key.NewBinding(
			key.WithKeys("v"),
			key.WithHelp("v", "cycle visualizer"),
		),
		Theme: key.NewBinding(
			key.WithKeys("t"),
			key.WithHelp("t", "theme picker"),
		),
		Help: key.NewBinding(
			key.WithKeys("?", "f1"),
			key.WithHelp("?", "which-key help"),
		),
		Quit: key.NewBinding(
			key.WithKeys("q", "ctrl+c"),
			key.WithHelp("q", "quit"),
		),
		YankTrack: key.NewBinding(
			key.WithKeys("y"),
			key.WithHelp("y", "yank/copy track"),
		),
		OpenSearch: key.NewBinding(
			key.WithKeys("o"),
			key.WithHelp("o", "open search in browser"),
		),
		CountryTab: key.NewBinding(
			key.WithKeys("C"),
			key.WithHelp("C", "countries / FM"),
		),
		HistoryTab: key.NewBinding(
			key.WithKeys("H"),
			key.WithHelp("H", "history view"),
		),
		Timer: key.NewBinding(
			key.WithKeys("z", "Z"),
			key.WithHelp("z", "sleep/pomodoro timer"),
		),
	}
}
