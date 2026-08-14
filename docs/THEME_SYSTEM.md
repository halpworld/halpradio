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

<p align="center">
  <img src="./images/visualizers.jpg" alt="halpradio Animated Animal DJ Visualizers" width="800" />
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
   - **Zero-Jitter Normalized Poses**: Every pose (head + arm + deck) has an exact, invariant width (24 visual columns) for smooth, stable rendering without horizontal shifting.
   - **Harmonic Multi-Frequency Equalizer Rack**: Solid 6-bar mini-EQ (` ▂▃▄▅▆`) driven by harmonic frequency physics (sub-bass kick, mid melody, treble shimmer) with smooth attack and exponential decay.
   - **Rhythmic Groove**: Head bobbing and turntable vinyl rotation (`◓`, `◑`, `◒`, `◐`) tempo-matched to audio playback.
   - **Sleep State**: When stopped/paused, the DJ rests peacefully on the turntable (`🎧 (= - ω - =)..zzZ [ 💿 ] ⏹ STOPPED`).
2. **Bars Equalizer**: Dynamic vertical block bars (` ▂▃▄▅▆▇█`) responding to time ticks.
3. **Waveform**: Smooth sine wave unicode characters depicting audio oscillation.
4. **Spectrum**: Multi-band frequency equalizer.
5. **Minimal**: Compact stereo VU meters.

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
