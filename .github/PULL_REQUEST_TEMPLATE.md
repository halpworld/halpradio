## Description

<!-- Describe your changes clearly. If fixing an issue, link to it (e.g. "Closes #123"). -->

## Type of Change

- [ ] 📻 **Station Catalog Update** (New stations, updated URLs, or metadata fixes)
- [ ] 🐛 **Bug Fix** (Non-breaking fix addressing an issue)
- [ ] ✨ **New Feature** (New visualizer, theme, audio engine improvement, etc.)
- [ ] 🎨 **UI / UX Refinement** (Layout, colors, keybindings)
- [ ] 📚 **Documentation** (README, docs, guides)

## Checklist

- [ ] I have read the [CONTRIBUTING.md](https://github.com/halpworld/halpradio/blob/main/CONTRIBUTING.md) guide.
- [ ] If submitting new stations:
  - [ ] Stream URLs have been tested and play properly.
  - [ ] Schema complies with `stations.yaml` (`id`, `name`, `url`, `genre`, `country`, `bitrate`, `codec`, `homepage`).
- [ ] If submitting Go code:
  - [ ] Code is formatted with `gofmt -s -w .`.
  - [ ] All tests pass cleanly (`go test ./...`).
  - [ ] `go vet ./...` reports no issues.
  - [ ] No hardcoded theme colors in components (conforms to theme token system).
