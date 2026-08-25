# Theme System & Audio Visualizers 🎨

`halpradio` features a dynamic styling engine built on [Lipgloss](https://github.com/charmbracelet/lipgloss), offering 6 curated built-in terminal themes, a **Custom User Themes Engine** (`~/.config/halpradio/themes/*.yaml`), and 9 animated audio visualizers.

---

## 🎨 Built-In Color Palettes (`pkg/theme/theme.go`)

Each theme defines a cohesive set of semantic design tokens used across headers, station lists, modal dialogs, status badges, and audio visualizers:

```go
type Theme struct {
    ID          string         // Theme identifier (e.g. "tokyonight", "rose-pine")
    Name        string         // Display name
    Author      string         // Creator or community credit
    Description string         // Short aesthetic summary
    Primary     lipgloss.Color // Active focus borders, highlights, main accent
    Secondary   lipgloss.Color // Subheadings, auxiliary text, accents
    Background  lipgloss.Color // Window & modal background
    Foreground  lipgloss.Color // Standard text, track names, unselected items
    Muted       lipgloss.Color // Secondary metadata, shortcuts, dim borders
    Playing     lipgloss.Color // Live playing station indicator, visualizer VU
    Favorite    lipgloss.Color // Starred favorites icon & indicator
    Border      lipgloss.Color // Pane borders, splitters, modal outlines
    Highlight   lipgloss.Color // Cursor row highlight, search matches
    Badge       lipgloss.Color // Badge background in header & statusbar
    BadgeText   lipgloss.Color // Text color inside solid badges
    HeaderAscii lipgloss.Color // Radio ASCII logo & decorative banners
    IsCustom    bool           // True for user-defined themes loaded from YAML
}
```

### Curated Built-In Themes:

1. **Tokyo Night** (`tokyonight`) — *Default*  
   Deep dark blue aesthetics featuring soft pastel blues (`#7aa2f7`), purples (`#bb9af7`), and cyan highlights (`#7dcfff`).
   
2. **Catppuccin Mocha** (`catppuccin`)  
   Soothing warm pastel palette with lavender (`#cba6f7`), pink (`#f5c2e7`), and mint green (`#a6e3a1`).

3. **Synthwave '84** (`synthwave`)  
   High-contrast neon retro theme with glowing magenta (`#ff007f`), cyan (`#00f0ff`), and neon green (`#39ff14`).

4. **Nord** (`nord`)  
   Arctic, north-bluish clean aesthetic with frost blues (`#88c0d0`), snow storm whites, and muted slate gray (`#4c566a`).

5. **Gruvbox Dark** (`gruvbox`)  
   Retro groove color scheme with warm orange (`#fe8019`), golden yellow (`#fabd2f`), and moss green (`#b8bb26`).

6. **Dracula** (`dracula`)  
   Famous vampire theme with dark background (`#282a36`), vivid purple (`#bd93f9`), and pink (`#ff79c6`).

---

## 🛠️ Custom User Themes Engine (`~/.config/halpradio/themes/`)

`halpradio` supports frictionless theme customization without recompilation. Users can drop any `.yaml` or `.yml` theme file into their configuration directory:

```text
~/.config/halpradio/themes/
├── rose-pine.yaml
├── everforest.yaml
├── kanagawa.yaml
├── monokai-pro.yaml
└── sample_theme.yaml.example
```

> [!NOTE]
> If `~/.config/halpradio/themes/` does not exist on startup, `halpradio` automatically creates the directory and writes a documented `sample_theme.yaml.example` starter file.

### YAML Theme Definition Schema

```yaml
# ~/.config/halpradio/themes/rose-pine.yaml

name: "Rosé Pine"
author: "Community"
description: "All natural pine, faux fur and delicate warmth"

# Semantic color tokens (6-digit hex #RRGGBB, 3-digit hex #RGB, or ANSI color names/numbers)
primary: "#eb6f92"        # Active focus borders, highlights, main accent
secondary: "#f6c177"      # Subheadings, auxiliary text, accents
background: "#191724"     # Window & modal background
foreground: "#e0def4"     # Standard text, track names, unselected items
muted: "#6e6a86"          # Secondary metadata, shortcuts, dim borders
playing: "#9ccfd8"        # Live playing station indicator, visualizer VU
favorite: "#eb6f92"       # Starred favorites icon & indicator
border: "#26233a"         # Pane borders, splitters, modal outlines
highlight: "#31748f"      # Cursor row highlight, search matches
badge: "#eb6f92"          # Badge background in header & statusbar
badge_text: "#191724"     # Text color inside solid badges
header_ascii: "#c4a7e7"   # Radio ASCII logo & decorative banners
```

### Color Token Reference & Resilience

- **Hex Values**: `#RRGGBB` (e.g. `#eb6f92`), `#RGB` (e.g. `#f0a`), or `#RRGGBBAA`.
- **ANSI Color Names**: `red`, `green`, `blue`, `cyan`, `magenta`, `yellow`, `white`, `black`, `brightred`, `brightcyan`, etc.
- **ANSI 256 Numbers**: Strings `0` through `255` (e.g. `"208"` for orange).
- **Zero-Crash Resilience**: If any color token is omitted or malformed, `halpradio` automatically applies clean fallback tokens based on `primary` and `background` without crashing or freezing.

---

## 🔁 Changing & Exporting Themes

### 1. Interactive Theme Picker & Community Hub (`t`)

Press `t` from anywhere in `halpradio` to open the Theme Picker & Community Hub:

```text
╭────────────────── 🎨 COLOR THEMES & COMMUNITY HUB ──────────────────╮
│                                                                     │
│  [1] 🎨 Installed Themes (6)      [2] 🌐 Community Hub (18)         │
│                                                                     │
│  [1]   Tokyo Night                                                  │
│  [2]   Catppuccin Mocha                                             │
│  [3] ❯ Rosé Pine (Custom) ●                                         │
│  [4]   Nord                                                         │
│                                                                     │
│  ╭──────────────────────── Preview: Rosé Pine ────────────────────╮ │
│  │ All natural pine, faux fur and delicate warmth                 │ │
│  │ [ PRI ] [ SEC ] [ HI ] [ PLAY ] [ BADGE ] [ FAV ] [ BORDER ]   │ │
│  │ 📻 HALPRADIO  [LOFI]  ● LIVE 128k                              │ │
│  │ ▶ Tycho - A Walk  ♫ ▂▃▅▆▇█                                     │ │
│  │ ❯ ★ 1. SomaFM Groove Salad                                     │ │
│  ╰────────────────────────────────────────────────────────────────╯ │
│                                                                     │
│  [Tab/1/2] Tab  [p] Live Preview  [Enter/i] Apply/Download  [Esc]   │
╰─────────────────────────────────────────────────────────────────────╯
```

- **Two Tabs**:
  - `[1] 🎨 Installed Themes`: Instant access to built-in and local custom themes.
  - `[2] 🌐 Community Hub`: Discover, filter, preview, and download dozens of community-crafted themes directly from the official themes repository (`halpradio-themes`)!
- **👁️ Live Interactive Preview (`p`)**: Press `p` on any theme (installed or online repository) to temporarily render the entire terminal UI in that theme before downloading or saving! Press `p` or `Esc` to revert, or `Enter`/`i` to confirm and apply.
- **In-App Download (`i` / `Enter`)**: In the Community Hub tab, press `i` or `Enter` on any theme to download it to `~/.config/halpradio/themes/<id>.yaml` and immediately apply it.
- **Search & Filter (`/`)**: In Community Hub, press `/` to instantly filter themes by name, author, style, or category.
- **Delete (`d`)**: Press `d` to remove a downloaded custom theme from your configuration.
- **In-App Export (`E`)**: Press `E` while in the Theme Picker to export the currently active theme into `~/.config/halpradio/themes/<name>.yaml`.
- **Auto-Persistence**: Changing a theme automatically updates `~/.config/halpradio/config.yaml` so your preference persists across restarts.

---

## 💻 CLI Theme Management (`halpradio theme`)

Manage, preview, and install themes directly from the command line:

```bash
# Browse installed and discoverable community themes from repo
halpradio theme list

# Preview theme color swatches and mini UI mockup in terminal
halpradio theme preview rose-pine
halpradio theme preview everforest

# Download and install theme from official repository
halpradio theme install rose-pine
halpradio theme install kanagawa

# Inspect detailed theme tokens and color definitions
halpradio theme info nord

# Export active theme to a starter YAML template
halpradio theme export my-custom-theme

# Remove a custom theme
halpradio theme remove my-custom-theme
```

---

## 🌐 Official Themes Repository (`halpradio-themes`)

Explore, share, and contribute community-crafted palettes in the official repository:

👉 **[https://github.com/halpworld/halpradio-themes](https://github.com/halpworld/halpradio-themes)**

---

## 🔊 Dynamic Audio Visualizers (`pkg/ui/components/visualizer.go`)

<p align="center">
  <img src="./images/preview.png" alt="halpradio Theme & Visualizer in Action — Real Terminal Screenshot" width="800" />
</p>

When audio is playing, `halpradio` renders a zero-jitter, beat-reactive animated visualizer in the Player Bar. Press `v` to cycle through visualizer modes:

```
DJ Cat Mode:    🎧 (=^･ω･^=)ﾉ [💿 ◓] ♫  ▂▃▄▅▆
DJ Dog Mode:    🎧  (∪･ω･∪) ﾉ [💿 ◑] ♫  ▂▃▄▅▆
DJ Bear Mode:   🎧  ʕ •ᴥ•ʔ  ﾉ [💿 ◒] ♫  ▂▃▄▅▆
DJ Frog Mode:   🎧  ( •⊖• ) ﾉ [💿 ◐] ♫  ▂▃▄▅▆
DJ Bunny Mode:  🎧 ( •ㅅ• )  ﾉ [💿 ◓] ♫  ▂▃▄▅▆
Bars Mode:      ♫  ▂▃▄▅▆▇█▇▆▅▄▃▂  ♬
Waveform Mode:  ∿ _⎽⎼─⎻⎺▔⎺⎻─⎼⎽_ ∿
Spectrum Mode:  🔊 BASS ███ MID ███ TREB ███
Minimal Mode:   L:████░░░░ R:██████░░
```

1. **Animated Animal DJs** (`dj-cat`, `dj-dog`, `dj-bear`, `dj-frog`, `dj-bunny`):
   - **Zero-Jitter Normalized Poses**: Every pose (head + arm + deck) has an invariant width (24 visual columns) for smooth rendering.
   - **Harmonic Multi-Frequency Equalizer Rack**: Solid 6-bar mini-EQ (` ▂▃▄▅▆`) driven by harmonic frequency physics (sub-bass kick, mid melody, treble shimmer) with smooth attack and exponential decay.
   - **Rhythmic Groove**: Head bobbing and turntable vinyl rotation (`◓`, `◑`, `◒`, `◐`) tempo-matched to audio playback.
   - **Sleep State**: When stopped/paused, the DJ rests peacefully on the turntable (`🎧 (= - ω - =)..zzZ [ 💿 ] ⏹ STOPPED`).
2. **Bars Equalizer**: Dynamic vertical block bars (` ▂▃▄▅▆▇█`) responding to time ticks.
3. **Waveform**: Smooth sine wave unicode characters depicting audio oscillation.
4. **Spectrum**: Multi-band frequency equalizer.
5. **Minimal**: Compact stereo VU meters.
