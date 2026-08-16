# AGENTS.md — AI Agent Guidance for halpradio 📻

This file provides system instructions, architectural constraints, and operational context for AI agentic systems (including **Google Antigravity**, **AGY CLI**, **Claude Code**, and automated code maintenance subagents) working in this repository.

---

## 🛠️ Essential Build & Test Commands

Always verify changes using standard Go toolchain commands before declaring completion:

```bash
# Build the application
go build -o halpradio main.go

# Run all unit tests
go test ./...

# Run tests with verbose output for a specific package
go test -v ./pkg/player
go test -v ./pkg/radio
go test -v ./pkg/ui

# Format code according to Go standards
gofmt -s -w .

# Run Go static analysis
go vet ./...
```

---

## 🏗️ Architecture & Component Boundaries

`halpradio` is written in Go 1.21+ using the **Bubble Tea** Elm-style TUI framework (`github.com/charmbracelet/bubbletea`) and **Lipgloss** (`github.com/charmbracelet/lipgloss`).

```
halpradio/
├── main.go               # Embeds stations.yaml; passes catalog to app.Run()
├── stations.yaml         # Bundled station catalog
├── pkg/
    ├── app/app.go        # CLI flags (-backend, -theme, -version), config loading, tea.Program setup
    ├── player/player.go   # Player interface, Manager struct, multi-backend exec, native Oto audio, ICY stream listener
    ├── radio/            # Store (bundled/local/favorites), Station struct, RadioBrowser HTTP client
    ├── theme/theme.go    # Theme struct & color palettes (tokyonight, catppuccin, synthwave, nord, gruvbox, dracula)
    ├── timer/            # Pomodoro focus interval engine, sleep timer with volume fade, OS event dispatcher
    ├── ui/               # Model, Update loop, View orchestrator, keymaps
    │   └── components/   # Pure UI sub-renderers (header, stationlist, playerbar, statusbar, visualizer, modals, whichkey)
    └── util/             # OS config directory (~/.config/halpradio/) and system clipboard helpers
```

---

## 📜 Key Architectural Rules for AI Agents

### 1. Concurrency & Mutex Safety (`pkg/player/player.go`)
- Audio subprocess management (`mpv`, `vlc`, `ffplay`) and native Go streaming (`oto/v3`) run in separate goroutines.
- ICY metadata extraction connects asynchronously over HTTP.
- **Rule**: Always lock `m.mu` (`m.mu.Lock()` / `m.mu.Unlock()`) when reading or writing state in `player.Manager`.
- **Rule**: Use `program.Send(ui.TrackUpdatedMsg(...))` or Bubble Tea `tea.Cmd` to send updates back to the TUI thread. Never modify UI state directly from an asynchronous goroutine.

### 2. Elm Architecture Discipline (`pkg/ui`)
- State mutations belong exclusively in [`pkg/ui/update.go`](./pkg/ui/update.go).
- UI renderers in [`pkg/ui/components/`](./pkg/ui/components/) must remain pure functions taking model state and active theme tokens as arguments.
- Do not perform disk I/O, network requests, or audio command calls inside component `View()` methods.

### 3. Theme Compliance (`pkg/theme/theme.go`)
- **Rule**: Never hardcode hex color strings (e.g. `#7aa2f7`) inside component files.
- Always use active theme tokens provided by `m.theme` (e.g. `theme.Primary`, `theme.Secondary`, `theme.Border`, `theme.Playing`).

### 4. Error Handling & TUI Resilience
- Audio stream errors or invalid URLs should update `player.Manager` status to `StatusError` or populate `lastError`.
- **Rule**: Never call `panic()` or `os.Exit()` inside UI updates or stream handlers. The TUI must remain interactive even when a stream fails.

### 5. Station YAML Schema
- When generating or modifying station entries, adhere strictly to the schema:
  `id`, `name`, `url`, `genre`, `country` (2-letter ISO uppercase), `bitrate` (int kbps), `codec`, `homepage`.

---

## 🤖 Antigravity Integration Guidelines

When operating inside **Google Antigravity (AGY)**:
- **Subagents**: For broad codebase exploration or background research, invoke the `research` subagent. For isolated execution tasks, use `self`.
- **Customizations**: Project-specific rules and instructions reside in `.agents/rules/` or root `AGENTS.md`.
- **Artifacts**: Store large reports, architectural proposals, or logs in `<appDataDir>/brain/<conversation-id>/`.
- **Slash Commands**: Recommend `/plan` for complex refactors, `/goal` for long tasks, or `/grill-me` to clarify UX design decisions with the user.

---

## 🤖 Claude Code Integration Guidelines

When operating inside **Claude Code**:
- Refer to `CLAUDE.md` for condensed CLI execution patterns and quick lookup.
- Always execute `go test ./...` after modifying files.
