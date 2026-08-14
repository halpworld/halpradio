# Configuration & Keybindings Reference ⚙️

`halpradio` provides zero-config defaults out of the box while allowing full customization via CLI flags, environment variables, and persistent config files.

---

## 📁 Configuration File Locations

All user state and settings are stored in your platform's standard configuration directory:

- **macOS / Linux**: `~/.config/halpradio/`
- **Windows**: `%APPDATA%\halpradio\`

```
~/.config/halpradio/
├── config.yaml       # Persistent user preferences
├── stations.yaml     # Custom user-added radio stations
└── favorites.json    # Favorited stations list
```

### `config.yaml` Schema:

```yaml
volume: 80
player_backend: "auto"     # auto, native, mpv, vlc, cvlc, ffplay, mplayer, mpg123
theme: "tokyonight"        # tokyonight, catppuccin, synthwave, nord, gruvbox, dracula
visualizer_mode: "bars"    # bars, waveform, spectrum, minimal
last_station_id: ""        # Remembers last played station
```

---

## 🚩 Command-Line Flags

```bash
# Force specific audio backend
halpradio -backend mpv

# Force native Go audio engine (no mpv or vlc needed)
halpradio -backend native

# Launch with Synthwave '84 theme
halpradio -theme synthwave

# Display version information
halpradio -version
```

---

## ⌨️ Complete Keybindings Reference (LazyVim Style)

Press `?` or `F1` anywhere in **halpradio** to toggle the floating **WhichKey Help Overlay**.

### 🧭 Navigation & Tab Controls

| Keybinding | Action |
|---|---|
| `j` / `↓` | Move selection down |
| `k` / `↑` | Move selection up |
| `h` / `←` | Focus left sidebar / Previous tab |
| `l` / `→` | Focus main list / Next tab |
| `1` - `4` | Direct jump to Tab (1: All, 2: Favorites, 3: Categories, 4: Online Search) |
| `g` | Jump to top of list |
| `G` | Jump to bottom of list |
| `Ctrl+u` | Scroll half page up |
| `Ctrl+d` | Scroll half page down |

### 🎵 Playback & Audio Controls

| Keybinding | Action |
|---|---|
| `Space` / `Enter` | Toggle Play / Pause selected station |
| `s` | Stop audio stream playback |
| `r` | Play a random station |
| `+` / `=` | Increase volume (+5%) |
| `-` | Decrease volume (-5%) |
| `m` | Mute / Unmute audio |
| `v` | Cycle audio visualizer mode (`bars` ➔ `waveform` ➔ `spectrum` ➔ `minimal`) |

### 📻 Station & Catalog Management

| Keybinding | Action |
|---|---|
| `f` | Toggle favorite star ⭐ |
| `/` | Live fuzzy search / filter stations |
| `a` | Open **Add Custom Station** modal |
| `e` | Edit local custom station |
| `d` | Delete local custom station |
| `p` | Export station YAML snippet to clipboard for GitHub PR |
| `c` | Filter by genre category |

### 🛠️ App & System

| Keybinding | Action |
|---|---|
| `t` | Open **Theme Picker** modal |
| `?` / `F1` | Toggle **WhichKey Overlay** |
| `q` / `Ctrl+c` | Quit halpradio |
| `Esc` | Close active modal dialog or clear search filter |
