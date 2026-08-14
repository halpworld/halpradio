# Developer & Contribution Guide 🤝

Thank you for contributing to **halpradio**! Whether you are expanding the public station catalog, building new TUI features, or fixing bugs, this guide will help you get set up quickly.

---

## 🛠️ Development Setup & Requirements

### Prerequisites
- **Go**: Version `1.21` or higher.
- **Git**: For version control.
- **Audio Player (Optional)**: `mpv`, `vlc`, or `ffmpeg` installed on your system for testing multi-format audio streams (or use the built-in native Go player).

### Clone & Run Locally
```bash
# Fork & clone the repository
git clone https://github.com/halpworld/halpradio.git
cd halpradio

# Run directly
go run main.go

# Run unit tests
go test ./...
```

---

## 📻 Contribution Workflow 1: Adding New Radio Stations

Expanding the catalog is the fastest way to contribute!

### Method 1: Exporting Station Snippets via `halpradio` (Recommended)

1. Launch `halpradio` (`go run main.go`).
2. Search or browse for a station (or press `a` to create a custom station).
3. Select the station and press `p`.
4. **halpradio** will generate the exact YAML snippet and copy it directly to your system clipboard!
5. Open [`stations.yaml`](../stations.yaml) and paste your snippet in the appropriate genre section.
6. Commit your changes and open a Pull Request.

### Method 2: Manual YAML Formatting

Add an entry to [`stations.yaml`](../stations.yaml) adhering to this schema:

```yaml
stations:
  - id: station-unique-id
    name: "Station Name"
    url: "https://stream.example.com/live.mp3"
    genre: "Genre / Tags"
    country: "US"           # 2-letter ISO country code (e.g. US, GB, SE, DE, FR, JP)
    bitrate: 128            # Stream bitrate in kbps (e.g. 128, 192, 320)
    codec: "MP3"            # MP3, AAC, OGG, etc.
    homepage: "https://example.com"
```

> [!IMPORTANT]
> - Verify stream URLs are publicly accessible over HTTP/HTTPS.
> - Ensure 2-letter ISO country codes are valid uppercase strings (e.g., `US`, `GB`, `DE`, `FR`, `JP`, `BR`).
> - Keep genre tags concise (e.g., `Lofi`, `Synthwave`, `Ambient`, `Jazz`, `Rock`, `Classical`, `News`).

---

## 💻 Contribution Workflow 2: Code Contributions

### Project Architecture Overview
Read our [Architecture Documentation](./ARCHITECTURE.md) to understand the Elm Architecture (Bubble Tea) used in `halpradio`:
- [`pkg/ui/model.go`](../pkg/ui/model.go): Global state.
- [`pkg/ui/update.go`](../pkg/ui/update.go): Input and event handling.
- [`pkg/ui/view.go`](../pkg/ui/view.go): Lipgloss frame rendering.
- [`pkg/ui/components/`](../pkg/ui/components): Sub-views (Header, StationList, PlayerBar, Modals, WhichKey).
- [`pkg/player/player.go`](../pkg/player/player.go): Audio backends & ICY stream reader.

### Coding Guidelines
1. **Formatting**: Always format your code with `gofmt -s -w .` before committing.
2. **Testing**: Add or update unit tests in `pkg/*/*_test.go` when adding new business logic.
3. **No Unhandled Panic**: Always return or log errors gracefully without calling `panic()`.
4. **Theme Token Usage**: Do not hardcode HEX color strings in UI components; always use tokens from [`pkg/theme/theme.go`](../pkg/theme/theme.go).

---

## 🚀 Release Automation & Homebrew Deployment

`halpradio` uses automated GitHub Actions workflows for continuous integration and multi-platform release distribution:

### 1. Continuous Integration (`.github/workflows/ci.yml`)
- Executes on every `push` and `pull_request` against the `main` branch.
- Runs cross-platform matrix testing on Ubuntu and macOS runners.
- Verifies code formatting with `gofmt`, executes static analysis with `go vet`, and tests with race detection (`go test -race`).
- Checks cross-compilation across `darwin/arm64`, `darwin/amd64`, `linux/amd64`, and `linux/arm64`.

### 2. Automated Releases & Homebrew Tap (`.github/workflows/release.yml`)
- Triggered automatically on pushing a version tag (e.g. `git tag v1.0.0 && git push origin v1.0.0`).
- Cross-compiles standalone release binaries for:
  - macOS Apple Silicon (`darwin_arm64`)
  - macOS Intel (`darwin_amd64`)
  - Linux x86_64 (`linux_amd64`)
  - Linux ARM64 (`linux_arm64`)
- Packages tarballs, generates SHA256 checksums, and publishes GitHub Release assets.
- Automatically generates and publishes Homebrew Formula to [`halpworld/homebrew-tap`](https://github.com/halpworld/homebrew-tap).

---

## 📋 Pull Request Checklist

Before submitting your PR:
- [ ] Code compiles cleanly with `go build main.go`.
- [ ] All unit tests pass with `go test ./...`.
- [ ] Code is formatted with `gofmt -s -w .`.
- [ ] Documentation or comments are updated if changing CLI flags, configuration, or keybindings.
- [ ] PR title is descriptive (e.g., `feat(ui): add visualizer wave mode` or `fix(player): resolve mpv process leak`).

Thank you for making **halpradio** awesome! 🎧
