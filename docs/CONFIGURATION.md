# Configuration & Keybindings Reference ⚙️

`halpradio` provides zero-config defaults out of the box while allowing full customization via CLI flags, environment variables, and persistent config files.

---

## 📁 Configuration File Locations

All user state and settings are stored in your platform's standard configuration directory:

- **macOS / Linux**: `~/.config/halpradio/`
- **Windows**: `%APPDATA%\halpradio\`

```
~/.config/halpradio/
├── config.yaml       # Persistent user preferences, timers & system hooks
├── stations.yaml     # Custom user-added radio stations
├── favorites.json    # Favorited stations list
└── saved_tracks.txt  # Bookmarked tracks from history
```

### `config.yaml` Schema:

```yaml
# Audio & Player Preferences
volume: 80
player_backend: "auto"     # auto, native, mpv, vlc, cvlc, ffplay, mplayer, mpg123
theme: "tokyonight"        # tokyonight, catppuccin, synthwave, nord, gruvbox, dracula
visualizer_mode: "dj-cat"   # dj-cat, dj-dog, dj-bear, dj-frog, dj-bunny, bars, wave, spectrum, minimal, off
search_provider: "spotify"  # spotify, youtube, apple, soundcloud, bandcamp, ddg, google
last_station_id: ""        # Remembers last played station

# Pomodoro Focus Timer Settings
pomodoro_focus_min: 25     # Focus session length in minutes
pomodoro_short_break_min: 5 # Short rest duration in minutes
pomodoro_long_break_min: 15 # Long rest duration in minutes
pomodoro_cycles: 4          # Number of focus intervals before a long break
pomodoro_focus_station: ""  # Station ID to play during focus sprints (or empty to keep current)
pomodoro_break_station: ""  # Station ID to play during breaks (or "__pause__" / empty)

# Sleep Timer Settings
sleep_fade_seconds: 10      # Duration of smooth volume fade-out before stopping

# System Events & OS Linking
event_notify_desktop: true  # Send native OS desktop notifications (macOS / Linux / Windows)
event_terminal_bell: true   # Emit terminal bell (\a) chime on interval transitions
event_command_hook: ""      # Path to shell script / command to run on timer transitions
```

---

## 🔔 System Desktop Events & Shell Hook Integration

`halpradio` can trigger actions on your computer when timers transition between sprint and break intervals:

### 1. Native Desktop Notifications
- **macOS**: Triggers `osascript` notification with system `Glass` chime.
- **Linux**: Sends desktop notification via `notify-send`.
- **Windows**: Displays native Windows 10/11 toast notification via PowerShell.

### 2. Custom Shell Script Hook (`event_command_hook`)
Set `event_command_hook: "/path/to/my-hook.sh"` in `config.yaml` or through the in-app modal (`z` ➔ `8`).

Whenever a timer phase transitions, `halpradio` executes your script with the following environment variables:

| Environment Variable | Example Value | Description |
|---|---|---|
| `HALPRADIO_EVENT` | `focus_start`, `short_break_start`, `long_break_start`, `sleep_complete` | The lifecycle event name |
| `HALPRADIO_PHASE` | `Focus`, `Short Break`, `Long Break` | Active Pomodoro phase |
| `HALPRADIO_CYCLE` | `2` | Current sprint cycle index |
| `HALPRADIO_TOTAL_CYCLES` | `4` | Target cycle count before long break |
| `HALPRADIO_TITLE` | `Deep Focus Session 🍅` | Event summary title |
| `HALPRADIO_MESSAGE` | `Sprint #2 of 4 (25m)` | Human-readable event description |
| `HALPRADIO_STATION_ID` | `lofi-girl` | Active or requested station ID |

#### Example Custom Hook Script (`~/.config/halpradio/hook.sh`):
```bash
#!/usr/bin/env bash
# Update Slack status or trigger smart home lights based on Pomodoro phase
case "$HALPRADIO_EVENT" in
  focus_start)
    # Enable macOS Do Not Disturb or update status
    echo "Focus sprint #${HALPRADIO_CYCLE} started!"
    ;;
  short_break_start|long_break_start)
    echo "Break started: Time to stretch!"
    ;;
  sleep_complete)
    echo "Goodnight! Sleep timer finished."
    ;;
esac
```

---

## 🚩 Command-Line Flags

```bash
# Set audio player backend explicitly
halpradio --backend=native
halpradio --backend=mpv
halpradio --backend=vlc

# Choose initial color theme
halpradio --theme=synthwave
halpradio --theme=catppuccin

# Print version and system diagnostic report
halpradio --version
```

---

## ⌨️ Global Keybindings Reference

### 🧭 Navigation & Panes

| Keybinding | Action |
|---|---|
| `j` / `k` or `↓` / `↑` | Move selection down / up |
| `h` / `l` or `←` / `→` | Switch focus between sidebar and main list / Prev & next tab |
| `1` - `7` | Direct jump to Tab (`1: Catalog`, `2: Activities`, `3: Genres`, `4: Favorites`, `5: RadioBrowser`, `6: Custom`, `7: History`) |
| `H` | Jump directly to Track History tab |
| `Tab` / `Shift+Tab` | Cycle focus between sidebar and station list |
| `g` / `G` | Jump to top / bottom of list |
| `Ctrl+u` / `Ctrl+d` | Scroll half page up / down |

### 🎵 Playback, Timers & Audio Controls

| Keybinding | Action |
|---|---|
| `Space` / `Enter` | Toggle Play / Pause selected station (or tune in from history) |
| `s` | Stop audio stream playback |
| `z` / `Z` | Open **Sleep Timer & Pomodoro Focus Mode** modal |
| `r` | Play a random station |
| `+` / `=` | Increase volume (+5%) |
| `-` | Decrease volume (-5%) |
| `m` | Toggle Mute / Unmute audio |
| `v` | Cycle audio visualizer mode (`dj-cat`, `dj-dog`, `dj-bear`, `dj-frog`, `dj-bunny`, `bars`, `wave`, `spectrum`, `minimal`) |

### ⭐ Discovery, Sharing & History

| Keybinding | Action |
|---|---|
| `y` | Yank / copy track metadata (`Artist - Title`) to system clipboard |
| `o` | Open song search in default browser (Spotify, YT Music, Apple, Soundcloud, DDG, Google) |
| `s` | Bookmark track to `~/.config/halpradio/saved_tracks.txt` (on History tab) |
| `c` | Clear track history log (on History tab) |
| `p` | Export station YAML snippet to system clipboard for GitHub PR |

### 📻 Station & Catalog Management

| Keybinding | Action |
|---|---|
| `f` | Toggle favorite star ⭐ |
| `/` | Focus live search bar |
| `w` | Filter by activity work mode (Programming, Cleaning, Reading, Thinking, Relaxing, News) |
| `c` | Filter by genre category |
| `a` | Open **Add Custom Station** modal |
| `e` | Edit local custom station |
| `d` | Delete local custom station |

### 🛠️ Interface & Modals

| Keybinding | Action |
|---|---|
| `t` | Open **Theme Picker** modal |
| `?` / `F1` | Toggle **WhichKey Overlay** |
| `Esc` | Close active modal dialog or clear search filter |
| `q` / `Ctrl+c` | Quit halpradio |
