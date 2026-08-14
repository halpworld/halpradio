# Theme System & Audio Visualizers 🎨

`halpradio` features a dynamic styling engine built on [Lipgloss](https://github.com/charmbracelet/lipgloss), offering 6 curated terminal themes and 4 animated audio visualizers.

---

## 🎨 Theme System (`pkg/theme/theme.go`)

Each theme defines a cohesive set of design tokens used across headers, lists, modal dialogs, status badges, and audio player bars:

```go
type Theme struct {
    Name         string
    Primary      lipgloss.Color
    Secondary    lipgloss.Color
    Background   lipgloss.Color
    Foreground   lipgloss.Color
    Muted        lipgloss.Color
    Playing      lipgloss.Color
    Favorite     lipgloss.Color
    Border       lipgloss.Color
    Highlight    lipgloss.Color
    Badge        lipgloss.Color
    BadgeText    lipgloss.Color
    HeaderAscii  lipgloss.Color
}
```

### Curated Color Palettes:

1. **Tokyo Night** (`tokyonight`)  
   Deep dark blue aesthetics featuring soft pastel blues (`#7aa2f7`), purples (`#bb9af7`), and cyan highlights (`#7dcfff`). Default theme.
   
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

## 🔁 Changing Themes

- **In App**: Press `t` to bring up the **Theme Picker Modal**. Use `j/k` to preview themes in real time, and press `Enter` to confirm.
- **CLI Flag**: Launch with `--theme=<name>` (e.g. `halpradio --theme=synthwave`).
- **Config File**: Saved automatically in `~/.config/halpradio/config.yaml`.

---

## 🔊 Dynamic Audio Visualizers (`pkg/ui/components/visualizer.go`)

When audio is playing, `halpradio` renders an animated visualizer in the Player Bar. Press `v` to cycle through 4 visualizer modes:

```
Bars Mode:     ▃▅▇█▇▅▃ ▂▄▆█▆▄▂
Waveform Mode: ~≈∼-∼≈~-~≈∼-∼≈~
Spectrum Mode: ░▒▓█▓▒░░▒▓█▓▒░
Minimal Mode:  ● ∙ ○ ∙ ● ∙ ○ ∙
```

1. **Bars Equalizer**: Dynamic vertical block bars (` ▂▃▄▅▆▇█`) responding to time ticks.
2. **Waveform**: Smooth sine wave unicode characters depicting audio oscillation.
3. **Spectrum**: Multi-level block density visualizer.
4. **Minimal**: Compact pulsing dot indicators for minimalistic terminal layouts.

---

## 🛠️ Adding a New Theme

To contribute a new color palette:

1. Open [`pkg/theme/theme.go`](../pkg/theme/theme.go).
2. Add your theme entry to the `Themes` map:

```go
"cyberpunk": {
    Name:        "Cyberpunk 2077",
    Primary:     lipgloss.Color("#fcee0a"),
    Secondary:   lipgloss.Color("#00f0ff"),
    Background:  lipgloss.Color("#000b1e"),
    Foreground:  lipgloss.Color("#e8e8e8"),
    Muted:       lipgloss.Color("#586e75"),
    Playing:     lipgloss.Color("#00ff66"),
    Favorite:    lipgloss.Color("#ff0055"),
    Border:      lipgloss.Color("#fcee0a"),
    Highlight:   lipgloss.Color("#ff0055"),
    Badge:       lipgloss.Color("#fcee0a"),
    BadgeText:   lipgloss.Color("#000b1e"),
    HeaderAscii: lipgloss.Color("#00f0ff"),
},
```

3. Update CLI flag documentation and send a PR!
