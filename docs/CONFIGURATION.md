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
├── debug.log         # Diagnostic log, only when started with --debug
├── stations.yaml     # Custom user-added radio stations
├── favorites.json    # Favorited stations list
├── saved_tracks.txt  # Bookmarked tracks from history
├── themes/           # Custom YAML themes (*.yaml)
├── plugins.json      # Plugin enabled states & approved permissions
├── plugins/          # Installed WebAssembly plugin packages
└── plugins_data/     # Sandboxed persistent storage per plugin
```

### `config.yaml` Schema:

```yaml
# Audio & Player Preferences
volume: 80
player_backend: "auto"     # auto, native, mpv, vlc, cvlc, ffplay, mplayer, mpg123
theme: "tokyonight"        # built-in (tokyonight, catppuccin, synthwave, nord, gruvbox, dracula) or custom theme ID
visualizer_mode: "dj-cat"   # dj-cat, dj-dog, dj-bear, dj-frog, dj-bunny, bars, wave, spectrum, minimal, off
search_provider: "spotify"  # spotify, youtube, apple, soundcloud, bandcamp, ddg, google
last_station_id: ""        # Remembers last played station

# Desktop Integration & Media Keys
song_notifications: true    # Show desktop banner notifications when track changes
mpris_enabled: true         # Enable Linux MPRIS v2 D-Bus service (playerctl / media widgets)
ipc_enabled: true           # Enable local IPC socket for CLI & script remote control

# WebAssembly Plugin & Extension System
plugins_enabled: true       # Enable/disable Wasm plugin sandbox engine
plugin_registry_url: "https://raw.githubusercontent.com/halpworld/halpradio-plugins/main/registry.json"

# Station Catalog Auto-Update & Caching
catalog_auto_update: true   # Lightweight background sync for newly added curated stations
catalog_cache_ttl_hours: 24 # Minimum hours between remote checks (zero load within TTL)
catalog_update_url: "https://raw.githubusercontent.com/halpworld/halpradio/main/stations.yaml"

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

# Acoustic Stream Fingerprinting & Dirty Metadata Sanitizer
fingerprint_enabled: true   # Enable on-demand acoustic recognition via 'I' (Chromaprint / AcoustID)
auto_identify: false        # Automatically identify music when streams lack ICY track metadata
acoustid_api_key: ""        # AcoustID API key (leave empty to use default halpradio client key)

# Experimental Features (On Hold)
experimental_tuner: false   # Enable experimental Analog Frequency Tuner on Tab 0 (see docs/TUNER.md)
```

---

## 🖥️ Desktop Integration, MPRIS & CLI Remote Control

### 1. Linux MPRIS v2 D-Bus Interface
`halpradio` exposes a complete **MPRIS v2** interface on Linux (`org.mpris.MediaPlayer2.halpradio`) over the session bus.

This allows out-of-the-box hardware media key control via:
- `playerctl play-pause`
- `playerctl next`
- `playerctl previous`
- `playerctl stop`
- `playerctl status` / `playerctl metadata`
- Integration with GNOME, KDE Plasma, Waybar, Polybar, and desktop media widgets.

### 2. macOS & Cross-Platform CLI Remote Control (`halpradio remote`)
Control `halpradio` from anywhere without switching windows:

```bash
# Toggle playback
halpradio remote play-pause

# Next / Previous station in active list
halpradio remote next
halpradio remote prev

# Audio adjustments
halpradio remote volup
halpradio remote voldown
halpradio remote mute
halpradio remote random
halpradio remote status
```

#### macOS Shortcuts, Raycast & Keybinding Examples:
- **macOS Shortcuts / Automator**: Create quick actions executing `halpradio remote play-pause` bound to F7/F8/F9.
- **Raycast / Alfred**: Create script commands for station jumping.
- **Skhd / Karabiner-Elements**:
  ```
  cmd + alt + space : halpradio remote play-pause
  cmd + alt + right : halpradio remote next
  cmd + alt + left  : halpradio remote prev
  ```
- **tmux Keybinding (`~/.tmux.conf`)**:
  ```tmux
  bind-key P run-shell "halpradio remote play-pause"
  bind-key N run-shell "halpradio remote next"
  ```

### 3. Song Change Desktop Notifications
Whenever a stream broadcasts a new track title via ICY metadata, `halpradio` posts a native desktop notification banner (`📻 halpradio — [Station Name]` / `🎶 [Artist] - [Title]`) with automatic deduplication.

Toggle anytime via `-notifications=false` or `song_notifications: false` in `config.yaml`.

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

# Toggle desktop integrations
halpradio --notifications=false
halpradio --mpris=false
halpradio --ipc=false

# CLI Remote Control
halpradio remote play-pause
halpradio remote next
halpradio remote status

# Acoustic stream fingerprinting
halpradio --fingerprint=true
halpradio --auto-identify=true

# Plugin Management CLI
halpradio plugin list
halpradio plugin install webhook-broadcaster
halpradio plugin update --all
halpradio plugin remove webhook-broadcaster

# Print version and system diagnostic report
halpradio --version

# Diagnostics for a bug report (see "Debug Logging" below)
halpradio --debug
halpradio --debug-log /tmp/halpradio.log
```

---

## 🐛 Debug Logging

halpradio draws in the alternate screen buffer, so anything printed to stdout or stderr is invisible while the
TUI runs. Diagnostics go to a file instead, and are **off by default**:

| Trigger | Effect |
|---|---|
| `halpradio --debug` | Log to `~/.config/halpradio/debug.log` |
| `halpradio --debug-log <path>` | Log to a specific file (implies `--debug`) |
| `HALPRADIO_DEBUG=1 halpradio` | Same as `--debug` |

The log captures:

- **Session header** — halpradio version, Go version, OS/arch, `TERM`, `COLORTERM`, session type, whether a
  D-Bus session bus is present.
- **Player** — the detected backend, the exact external command line, stream URLs, exit codes and errors.
- **Desktop integrations** — MPRIS / IPC startup results and every published playback state change.
- **Update loop** — one line per message the TUI handled, plus a `SLOW` marker for anything over 250 ms.

### Freeze diagnosis

The Bubble Tea update loop is single-threaded, so anything that blocks in it stops the whole TUI from
responding to the keyboard. A watchdog goroutine notices an update that has been running for more than
`5s` and appends every goroutine's stack to the log:

```
14:02:11.884 [update] → Update KeyMsg "enter"
14:02:16.885 [watchdog] STALLED: Update KeyMsg "enter" (stuck 5.001s)
14:02:16.885 [watchdog] goroutine dump (stalled operation):
goroutine 1 [semacquire]:
sync.(*Mutex).Lock(...)
	github.com/halpworld/halpradio/pkg/desktop.(*MPRISServer).UpdatePlaybackState(...)
...
```

If the UI locks up, wait ~10 seconds before killing halpradio so the dump lands in the file.

The log is created with `0600` permissions and rotated to `debug.log.old` once it passes 2 MiB. It contains
station stream URLs and local paths but no credentials — still worth skimming before pasting into a public
issue.

---

## ⌨️ Global Keybindings Reference

### 🧭 Navigation & Panes

| Keybinding | Action |
|---|---|
| `j` / `k` or `↓` / `↑` | Move selection down / up |
| `n` / `]` | Jump and play **Next station** in list |
| `N` / `[` | Jump and play **Previous station** in list |
| `h` / `l` or `←` / `→` | Switch focus between sidebar and main list / Prev & next tab |
| `1` - `9` (`0`) | Direct jump to Tab (`1: Activities`, `2: Catalog`, `3: Countries`, `4: Genres`, `5: Favorites`, `6: RadioBrowser`, `7: Custom`, `8: History`, `9: Globe`, `0: Tuner`) |
| `C` | Jump directly to Countries / FM tab |
| `H` | Jump directly to Track History tab |
| `9` / `M` | Jump directly to 3D Globe Explorer |
| `0` / `F` | Jump directly to Analog Frequency Tuner (experimental) |
| `Tab` / `Shift+Tab` | Cycle focus between sidebar and station list |
| `g` / `G` | Jump to top / bottom of list |
| `Ctrl+u` / `Ctrl+d` | Scroll half page up / down |

### 🎵 Playback, Timers & Audio Controls

| Keybinding | Action |
|---|---|
| `Space` / `Enter` (or Media Play/Pause) | Toggle Play / Pause selected station (or tune in from history) |
| `s` / `x` (or Media Stop) | Stop audio stream playback |
| `z` / `Z` | Open **Sleep Timer & Pomodoro Focus Mode** modal |
| `r` / `R` | Play a random station |
| `+` / `=` / `>` (or Media VolUp) | Increase volume (+5%) — works across ANSI, ISO, AZERTY, QWERTZ layouts |
| `-` / `_` / `<` (or Media VolDown) | Decrease volume (-5%) |
| `m` / `M` / `0` (or Media Mute) | Toggle Mute / Unmute audio |
| `v` | Cycle audio visualizer mode (`dj-cat`, `dj-dog`, `dj-bear`, `dj-frog`, `dj-bunny`, `bars`, `wave`, `spectrum`, `minimal`) |
| `b` | Switch dial band (`FM` / `AM` / `SW`) in frequency tuner mode |

### ⭐ Discovery, Sharing & History

| Keybinding | Action |
|---|---|
| `I` | **Identify playing track** via acoustic stream fingerprinting (Chromaprint / AcoustID / MusicBrainz) |
| `y` | Yank / copy track metadata (`Artist - Title`) or identified song to system clipboard |
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
| `P` | Open **Plugins & Extensions Manager** (Wasm Sandbox) |
| `t` | Open **Theme Picker** modal |
| `?` / `F1` | Toggle **WhichKey Overlay** |
| `Esc` | Close active modal dialog or clear search filter |
| `q` / `Ctrl+c` | Quit halpradio |
