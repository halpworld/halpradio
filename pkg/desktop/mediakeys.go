package desktop

import "strings"

// MediaAction represents an OS-level or hardware media key action.
type MediaAction string

const (
	ActionPlayPause   MediaAction = "play-pause"
	ActionPlay        MediaAction = "play"
	ActionPause       MediaAction = "pause"
	ActionStop        MediaAction = "stop"
	ActionNextStation MediaAction = "next"
	ActionPrevStation MediaAction = "prev"
	ActionVolumeUp    MediaAction = "volup"
	ActionVolumeDown  MediaAction = "voldown"
	ActionMute        MediaAction = "mute"
	ActionRandom      MediaAction = "random"
	ActionQuit        MediaAction = "quit"
	ActionStatus      MediaAction = "status"
)

// NormalizeKey maps various hardware media keys, terminal escape sequences,
// and keyboard layout variants to standard action identifiers.
func NormalizeKey(rawKey string) (MediaAction, bool) {
	k := strings.ToLower(strings.TrimSpace(rawKey))
	switch k {
	case "play", "pause", "playpause", "play_pause", "media_play", "media_pause", "media_play_pause", "mediaplaypause", "xf86audioplay", "xf86audiopause":
		return ActionPlayPause, true
	case "stop", "media_stop", "mediastop", "xf86audiostop":
		return ActionStop, true
	case "next", "nexttrack", "next_track", "media_next", "medianext", "xf86audionext":
		return ActionNextStation, true
	case "prev", "previous", "prevtrack", "prev_track", "media_prev", "mediaprev", "xf86audioprev":
		return ActionPrevStation, true
	case "volume_up", "volup", "volumeup", "xf86audioraisevolume":
		return ActionVolumeUp, true
	case "volume_down", "voldown", "volumedown", "xf86audiolowervolume":
		return ActionVolumeDown, true
	case "volume_mute", "mute", "volumemute", "xf86audiomute":
		return ActionMute, true
	default:
		return "", false
	}
}

// ParseAction converts a CLI or IPC string argument to a MediaAction.
func ParseAction(str string) (MediaAction, bool) {
	s := strings.ToLower(strings.TrimSpace(str))
	s = strings.ReplaceAll(s, "_", "-")
	switch s {
	case "play-pause", "playpause", "toggle":
		return ActionPlayPause, true
	case "play":
		return ActionPlay, true
	case "pause":
		return ActionPause, true
	case "stop":
		return ActionStop, true
	case "next", "next-station", "nexttrack":
		return ActionNextStation, true
	case "prev", "previous", "prev-station", "prevtrack":
		return ActionPrevStation, true
	case "volup", "vol-up", "volume-up", "volumeup", "+":
		return ActionVolumeUp, true
	case "voldown", "vol-down", "volume-down", "volumedown", "-":
		return ActionVolumeDown, true
	case "mute", "unmute", "toggle-mute":
		return ActionMute, true
	case "random", "shuffle":
		return ActionRandom, true
	case "quit", "exit":
		return ActionQuit, true
	case "status", "info", "current":
		return ActionStatus, true
	default:
		return "", false
	}
}
