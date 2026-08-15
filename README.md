<div align="center">

<p align="center">
  <img src="./docs/images/social-preview.jpg" alt="halpradio — LazyVim-inspired Terminal Internet Radio Streamer" width="850" />
</p>

# 📻 halpradio

**The ultimate terminal Internet Radio streaming application for developers who live in the command line.**  
*Built with Go & Bubble Tea, featuring a LazyVim-inspired keyboard-driven interface.*

[![CI](https://github.com/halpworld/halpradio/actions/workflows/ci.yml/badge.svg)](https://github.com/halpworld/halpradio/actions/workflows/ci.yml)
[![GitHub Release](https://img.shields.io/github/v/release/halpworld/halpradio?style=flat-square&logo=github&color=7aa2f7)](https://github.com/halpworld/halpradio/releases)
[![Homebrew Tap](https://img.shields.io/badge/Homebrew-halpworld%2Ftap-orange?style=flat-square&logo=homebrew)](https://github.com/halpworld/homebrew-tap)
[![Go Version](https://img.shields.io/badge/Go-1.21%2B-00ADD8?style=flat-square&logo=go)](https://golang.org)
[![Go Reference](https://pkg.go.dev/badge/github.com/halpworld/halpradio.svg)](https://pkg.go.dev/github.com/halpworld/halpradio)
[![Go Report Card](https://goreportcard.com/badge/github.com/halpworld/halpradio)](https://goreportcard.com/report/github.com/halpworld/halpradio)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg?style=flat-square)](./LICENSE)
[![Stations](https://img.shields.io/badge/Stations-30%2C000%2B-ff69b4?style=flat-square&logo=radio)](./stations.yaml)
[![PRs Welcome](https://img.shields.io/badge/PRs-welcome-brightgreen.svg?style=flat-square)](./CONTRIBUTING.md)

</div>

---

## ⚡ Quickstart (Get listening in 10 seconds)

### macOS & Linux via Homebrew
```bash
brew install halpworld/tap/halpradio
halpradio
```

### One-Line Shell Installer (macOS & Linux)
```bash
curl -fsSL https://raw.githubusercontent.com/halpworld/halpradio/main/install.sh | bash
halpradio
```

### 🐳 Instant Container Run (Docker)
```bash
docker run --rm -it --device /dev/snd halpworld/halpradio:latest
```

---

## 📺 Real TUI Screenshot Preview

<p align="center">
  <img src="./docs/images/preview.jpg" alt="halpradio TUI Interface — Real Terminal Screenshot" width="850" />
</p>

```
┌── 📻 HALPRADIO v0.0.3 ───────────────────────────────────────────────────────────── [ TOKYO NIGHT ] ┐
│                                                                                                    │
│  [ 1: Catalog (128) ]   [ 2: Work Modes ]   [ 3: Genres ]   [ 4: Online Search ]   [ ▶ PLAY ]      │
│ ────────────────────────────────────────────────────────────────────────────────────────────────── │
│  ▶  ⭐  🇫🇷 Lofi Girl Radio            [ Lofi / Beats ]             FR  •  320 kbps MP3            │
│     ⭐  🇺🇸 Nightwave Plaza            [ Vaporwave / Synth ]        US  •  192 kbps MP3            │
│        🇳🇱 Chillhop Music Radio        [ Lofi / Jazz ]              NL  •  320 kbps MP3            │
│        🇺🇸 SomaFM Groove Salad         [ Downtempo / Ambient ]      US  •  128 kbps MP3            │
│        🇺🇸 KEXP 90.3 FM                [ Indie / Alt Rock ]         US  •  160 kbps AAC            │
│        🇬🇧 BBC Radio 6 Music           [ Eclectic / Rock ]          GB  •  128 kbps AAC            │
│ ────────────────────────────────────────────────────────────────────────────────────────────────── │
│  🇫🇷 Lofi Girl Radio (Lofi / Beats)                                    🔊 [████████░░] 80%          │
│  ♪ Kupla & Philanthrope - Soft Wind           🎧 (=^･ω･^=)ﾉ [💿 ◓] ♫  ▂▃▄▅▆                        │
│ ────────────────────────────────────────────────────────────────────────────────────────────────── │
│  [?] WhichKey  [/] Search  [Space] Play/Pause  [v] Visualizer  [f] Fav  [t] Theme  [q] Quit        │
└────────────────────────────────────────────────────────────────────────────────────────────────────┘
```

---

## ✨ Why halpradio? (Comparison with Other Terminal Players)

| Feature | 📻 **halpradio** | PyRadio | Curseradio | radio-active | mocp / cmus |
|---|:---:|:---:|:---:|:---:|:---:|
| **Zero-Dependency Audio Engine** | ✅ **Yes** (`oto/v3` Go native) | ❌ (requires MPV/VLC) | ❌ (requires MPV) | ❌ (requires FFmpeg) | ❌ (C daemons) |
| **Multi-Backend Auto Detection** | ✅ `mpv` > `vlc` > `ffplay` > `native` | ⚠️ Limited | ⚠️ MPV only | ⚠️ FFmpeg only | ❌ Self only |
| **Beat-Reactive Animated Visualizers** | ✅ **5 Animal DJs + EQ Spectrum** | ❌ None | ❌ None | ❌ None | ⚠️ Basic VU |
| **Live ICY Metadata (Song / Artist)** | ✅ **Real-time Async Extraction** | ⚠️ Partial | ❌ None | ⚠️ Partial | ⚠️ Track only |
| **30,000+ Online Station Search** | ✅ **Built-in RadioBrowser API** | ❌ Manual list | ❌ TuneIn scrap | ⚠️ Search only | ❌ Local files |
| **Vim Navigation & Which-Key (`?`)** | ✅ **Full Vim Modal UX** | ⚠️ Basic keys | ⚠️ Basic keys | ❌ Non-modal | ⚠️ Custom maps |
| **Modern Theme Engine** | ✅ **6 Themes** (Tokyo Night, Synthwave, etc.) | ⚠️ Curses colors | ❌ Basic curses | ❌ Basic | ⚠️ Simple skins |
| **1-Key PR Clipboard Export (`p`)** | ✅ **Instant YAML snippet for PRs** | ❌ Manual | ❌ Manual | ❌ Manual | ❌ N/A |

---

## 🎧 Beat-Reactive Animal DJ Visualizers

<p align="center">
  <img src="./docs/images/visualizers.jpg" alt="halpradio Animated Animal DJ Visualizers — Real Terminal Screenshot" width="850" />
</p>

Press `v` anytime in **halpradio** to cycle through 5 animated animal DJs, classic bars, waveform, spectrum, or minimal meters:

| Visualizer Mode | Animal Character | DJ Booth & Live Equalizer Preview |
|---|---|---|
| `dj-cat` *(default)* | 🐱 **DJ Cat** | `🎧 (=^･ω･^=)ﾉ [💿 ◓] ♫  ▂▃▄▅▆` |
| `dj-dog` | 🐶 **DJ Dog** | `🎧  (∪･ω･∪) ﾉ [💿 ◑] ♫  ▂▃▄▅▆` |
| `dj-bear` | 🐻 **DJ Bear** | `🎧  ʕ •ᴥ•ʔ  ﾉ [💿 ◒] ♫  ▂▃▄▅▆` |
| `dj-frog` | 🐸 **DJ Frog** | `🎧  ( •⊖• ) ﾉ [💿 ◐] ♫  ▂▃▄▅▆` |
| `dj-bunny` | 🐰 **DJ Bunny** | `🎧 ( •ㅅ• )  ﾉ [💿 ◓] ♫  ▂▃▄▅▆` |
| `bars` | 📊 **Bars Equalizer** | `♫  ▂▃▄▅▆▇█▇▆▅▄▃▂  ♬` |
| `wave` | ∿ **Waveform** | `∿ _⎽⎼─⎻⎺▔⎺⎻─⎼⎽_ ∿` |
| `spectrum` | 🔊 **Spectrum** | `🔊 BASS ███ MID ███ TREB ███` |
| `minimal` | 🎚️ **Minimal VU** | `L:████░░░░ R:██████░░` |

- **Zero-Jitter Normalized Poses**: Every pose (head + arm + deck) has an exact, invariant width (24 visual columns) for smooth, stable rendering without horizontal shifting.
- **Harmonic Multi-Frequency Equalizer Rack**: Solid 6-bar mini-EQ (` ▂▃▄▅▆`) driven by harmonic frequency physics (sub-bass kick, mid melody, treble shimmer) with smooth attack and exponential decay.
- **Rhythmic Groove**: Head bobbing and turntable vinyl rotation (`◓`, `◑`, `◒`, `◐`) tempo-matched to audio playback.
- **Sleep State**: When stopped/paused, the DJ rests peacefully on the turntable (`🎧 (= - ω - =)..zzZ [ 💿 ] ⏹ STOPPED`).

---

## 📦 Complete Installation Options

### Method 1: Homebrew (macOS & Linux) — Recommended

```bash
# Install directly from the official tap
brew install halpworld/tap/halpradio
```

To upgrade anytime:
```bash
brew upgrade halpradio
```

---

### Method 2: One-Line Installer Script (macOS & Linux)

Automatically detects your OS and architecture (`arm64` / `amd64`), downloads the latest release binary, and installs it to `/usr/local/bin`:

```bash
curl -fsSL https://raw.githubusercontent.com/halpworld/halpradio/main/install.sh | bash
```

---

### Method 3: Pre-Compiled Binary Releases

Download pre-compiled standalone tarballs from the [GitHub Releases](https://github.com/halpworld/halpradio/releases) page:

| Platform | Architecture | Package |
|---|---|---|
| **macOS** | Apple Silicon (M1/M2/M3/M4) | `halpradio_*_darwin_arm64.tar.gz` |
| **macOS** | Intel x86_64 | `halpradio_*_darwin_amd64.tar.gz` |
| **Linux** | x86_64 | `halpradio_*_linux_amd64.tar.gz` |
| **Linux** | ARM64 / Raspberry Pi / AWS Graviton | `halpradio_*_linux_arm64.tar.gz` |
| **Windows** | x86_64 / ARM64 | `halpradio_*_windows_*.zip` |

---

### Method 4: Go Install

If you have Go 1.21+ installed:

```bash
go install github.com/halpworld/halpradio@latest
```

---

### Method 5: Build From Source

```bash
git clone https://github.com/halpworld/halpradio.git
cd halpradio
go build -o halpradio main.go
./halpradio
```

For Arch Linux (AUR), Scoop (Windows), or Nix packaging, see the [Packaging Guide](./docs/PACKAGING.md).

---

## 🔊 Audio Player Backends & Codec Support

`halpradio` works **completely out of the box** using its built-in **native Go audio engine** (`oto/v3` + `go-mp3`) with zero external binary dependencies required!

| Audio Backend | Priority | MP3 | AAC | OGG | FLAC | HLS / M3U8 | Dependencies |
|---|:---:|:---:|:---:|:---:|:---:|:---:|:---:|
| **mpv** *(recommended)* | 1st | ✅ | ✅ | ✅ | ✅ | ✅ | `brew install mpv` / `apt install mpv` |
| **vlc / cvlc** | 2nd | ✅ | ✅ | ✅ | ✅ | ✅ | `brew install vlc` / `apt install vlc` |
| **ffplay** | 3rd | ✅ | ✅ | ✅ | ✅ | ✅ | `ffmpeg` |
| **native (Go oto)** | 4th | ✅ | ❌ | ❌ | ❌ | ❌ | **Zero dependencies (built-in)** |

`halpradio` will automatically detect `mpv` > `vlc` > `ffplay` > `native` on startup. You can override this anytime with the `-backend` flag:

```bash
halpradio -backend mpv
halpradio -backend native
```

---

## ⌨️ Navigation & Keybindings (Vim Style)

Press `?` or `F1` anywhere in **halpradio** to open the floating **WhichKey Overlay**.

| Category | Keybinding | Action |
|---|---|---|
| **Navigation** | `j` / `k` or `↓` / `↑` | Move down / up |
| | `h` / `l` or `←` / `→` | Focus sidebar / main list or prev/next tab |
| | `1` - `6` | Direct jump to Tab (1: Catalog, 2: Activities, 3: Genres, 4: Favorites, 5: Online, 6: Custom) |
| | `g` / `G` | Jump to top / bottom of list |
| | `Ctrl+u` / `Ctrl+d` | Half page up / down |
| **Playback** | `Space` or `Enter` | Toggle Play / Pause selected station |
| | `s` | Stop audio stream |
| | `r` | Play random station |
| | `+` / `-` | Volume up / down (5% step) |
| | `m` | Mute / unmute |
| | `v` | Cycle visualizer (`dj-cat`, `dj-dog`, `dj-bear`, `dj-frog`, `dj-bunny`, `bars`, `wave`, `spectrum`, `minimal`) |
| **Catalog** | `f` | Toggle Favorite star ⭐ |
| | `/` | Live fuzzy search / filter stations |
| | `w` / `c` | Jump & filter by Activity Mode / Genre Category |
| | `a` | Open **Add Custom Station** modal |
| | `e` / `d` | Edit / Delete local custom station |
| | `p` | Export station YAML snippet to clipboard for GitHub PR |
| **UI & Options**| `t` | Theme picker modal (Tokyo Night, Catppuccin, Synthwave, etc.) |
| | `?` / `F1` | Toggle WhichKey help overlay |
| | `q` / `Ctrl+c` | Quit halpradio |

---

## 🎨 Themes & Customization

Switch themes on the fly by pressing `t` or pass `-theme <name>` via CLI:

- 🌌 **Tokyo Night** (`tokyonight`) — Neon blue and purple developer aesthetic
- ☕ **Catppuccin Mocha** (`catppuccin`) — Soothing pastel dark palette
- 🌆 **Retro Synthwave '84** (`synthwave`) — Vibrant neon magenta and cyan
- ❄️ **Nord** (`nord`) — Cool arctic blue minimalism
- 🪵 **Gruvbox Dark** (`gruvbox`) — Warm retro terminal tones
- 🧛 **Dracula** (`dracula`) — High-contrast gothic vampire theme

---

## 📻 Curated Station Categories & Activity Modes

Explore hundreds of curated streams across diverse activity moods and global genres:

- 🎧 **Focus & Flow**: Lofi Girl Radio, Chillhop Music, SomaFM Groove Salad, DEF CON Radio, Nightwave Plaza
- ☕ **Acoustic & Coffee**: Cafe De Paris, Smooth Jazz Florida, Swiss Classic, Instrumental Ambient
- 🚀 **High Energy Coding**: Synthwave 1980s, Cyberpunk FM, Goa Psytrance, Digitally Imported (DI.FM)
- 🌏 **Global Radio**: BBC Radio 6 Music (UK), KEXP 90.3 FM Seattle (US), Radio Paradise (US), FM 802 Osaka (JP), Big B Radio K-Pop (KR), FIP Radio (FR), Triple J (AU)
- 📰 **News & Public Broadcasts**: NPR 24 Hour Program Stream, BBC World Service, Deutschlandfunk

---

## 📚 Technical Documentation

Explore detailed technical documentation in the [`docs/`](./docs) folder:

- 🏗️ **[Architecture Overview](./docs/ARCHITECTURE.md)**: Elm Architecture (Bubble Tea MVU), package breakdown, event loop, and concurrency model.
- 🎵 **[Audio Engine & Stream Player](./docs/AUDIO_PLAYER.md)**: Multi-backend auto-detection (`mpv`, `vlc`, `ffplay`, native Go), process lifecycle, and real-time ICY metadata extraction.
- 📻 **[Station Catalog & RadioBrowser Integration](./docs/STATION_MANAGEMENT.md)**: Station storage hierarchy (`stations.yaml`, local config, favorites), RadioBrowser API client, and PR export workflow.
- 🎨 **[Theme System & Audio Visualizers](./docs/THEME_SYSTEM.md)**: Lipgloss styling system, theme palettes, and TUI visualizer algorithms.
- ⚙️ **[Configuration & Keybindings](./docs/CONFIGURATION.md)**: Directory layout, `config.yaml` options, CLI flags, and complete keymap reference.
- 📦 **[Packaging & Distribution Guide](./docs/PACKAGING.md)**: Specifications for Homebrew, Arch Linux AUR, Docker, Scoop, and Nix.
- 🤝 **[Developer & Contribution Guide](./docs/CONTRIBUTING.md)**: Developer setup, code standards, unit testing, and Pull Request checklist.
- 🤖 **[AI Agent Integration Guide](./docs/AGENTS.md)**: Guidelines for AI coding agents (**Google Antigravity** via [`AGENTS.md`](./AGENTS.md) & **Claude Code** via [`CLAUDE.md`](./CLAUDE.md)).

---

## 🤝 Contributing New Stations

We love community contributions! Expanding the catalog takes less than 30 seconds:

1. Select any station in **halpradio** and press `p` (copies formatted YAML to clipboard).
2. Paste the snippet into [`stations.yaml`](./stations.yaml) and submit a [Pull Request](https://github.com/halpworld/halpradio/pulls).
3. Alternatively, submit a [Station Suggestion Issue](https://github.com/halpworld/halpradio/issues/new?template=station_request.yml).
4. See [CONTRIBUTING.md](./CONTRIBUTING.md) for full guidelines.

---

## 📄 License

This project is licensed under the [MIT License](./LICENSE).

---

<div align="center">
Made with ❤️ for terminal lovers and internet radio enthusiasts.
</div>
