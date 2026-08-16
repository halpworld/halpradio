# CLAUDE.md — Claude Code Guidelines for halpradio 📻

## 🛠️ Build & Test Commands
- **Build**: `go build -o halpradio main.go`
- **Run**: `go run main.go`
- **Test All**: `go test ./...`
- **Test Package**: `go test -v ./pkg/player` (or `./pkg/radio`, `./pkg/ui`)
- **Format**: `gofmt -s -w .`
- **Vet**: `go vet ./...`

## 🧱 Key Architecture & Code Layout
`halpradio` is a keyboard-driven TUI internet radio streamer built with Go 1.21+ using **Bubble Tea** (Elm Architecture) and **Lipgloss**.

- `main.go`: Embeds `stations.yaml` via `//go:embed` and calls `app.Run()`.
- `pkg/app/app.go`: CLI flags (`-backend`, `-theme`, `-version`), config initialization, tea.Program runner.
- `pkg/player/player.go`: Multi-backend player manager (`mpv`, `vlc`, `ffplay`, etc.) + native Go fallback (`oto/v3` + `go-mp3`) and ICY stream metadata listener.
- `pkg/radio/store.go`: Station catalog store (`bundled`, `local`, `favorites`), YAML/JSON persistence.
- `pkg/radio/radiobrowser.go`: RadioBrowser HTTP search client.
- `pkg/theme/theme.go`: Theme definitions (`tokyonight`, `catppuccin`, `synthwave`, `nord`, `gruvbox`, `dracula`).
- `pkg/timer/`: Pomodoro focus state machine, sleep timer countdown, and OS notification dispatcher.
- `pkg/ui/model.go` & `update.go` & `view.go`: Bubble Tea Model, Update loop, View orchestrator.
- `pkg/ui/components/`: Sub-views (`header`, `sidebar`, `stationlist`, `playerbar`, `statusbar`, `visualizer`, `modals`, `whichkey`).
- `pkg/util/`: Path resolution (`~/.config/halpradio/`) and clipboard helper.

## 🎨 Code Style & Architectural Constraints
1. **Thread Safety**: Always protect shared state in `player.Manager` with `m.mu.Lock()` / `m.mu.Unlock()`.
2. **Async Events**: Dispatched audio/ICY updates must be sent via `program.Send(ui.TrackUpdatedMsg(...))` or `tea.Cmd`. Never mutate UI state directly from goroutines.
3. **Elm Architecture**: Keep `pkg/ui/components/` views pure. State mutations belong exclusively in `pkg/ui/update.go`.
4. **Theme Tokens**: Never hardcode hex color strings in UI components. Use `theme.Primary`, `theme.Border`, `theme.Playing`, etc.
5. **Resilience**: Never call `panic()` or `os.Exit()` on playback errors. Set `m.status = StatusError` and let the TUI inform the user gracefully.
6. **Verification**: Always run `go test ./...` and `gofmt -s -w .` after making modifications.
