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
visualizer_mode: "dj-cat"   # dj-cat, dj-dog, dj-bear, dj-frog, dj-bunny, bars, wave, spectrum, minimal, off
last_station_id: ""        # Remembers last played station
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
| `1` - `6` | Direct jump to Tab (`1: Catalog`, `2: Activities`, `3: Genres`, `4: Favorites`, `5: RadioBrowser`, `6: Custom`) |
| `Tab` / `Shift+Tab` | Cycle focus between sidebar and station list |
| `g` / `G` | Jump to top / bottom of list |
| `Ctrl+u` / `Ctrl+d` | Scroll half page up / down |

### 🎵 Playback & Audio Controls

| Keybinding | Action |
|---|---|
| `Space` / `Enter` | Toggle Play / Pause selected station |
| `s` | Stop audio stream playback |
| `r` | Play a random station |
| `+` / `=` | Increase volume (+5%) |
| `-` | Decrease volume (-5%) |
| `m` | Toggle Mute / Unmute audio |
| `v` | Cycle audio visualizer mode (`dj-cat` ➔ `dj-dog` ➔ `dj-bear` ➔ `dj-frog` ➔ `dj-bunny` ➔ `bars` ➔ `wave` ➔ `spectrum` ➔ `minimal` ➔ `off`) |

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
| `p` | Export station YAML snippet to system clipboard for GitHub PR |

### 🛠️ Interface & Modals

| Keybinding | Action |
|---|---|
| `t` | Open **Theme Picker** modal |
| `?` / `F1` | Toggle **WhichKey Overlay** |
| `Esc` | Close active modal dialog or clear search filter |
| `q` / `Ctrl+c` | Quit halpradio |
