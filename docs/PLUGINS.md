# 🔌 Plugin & Extension System Guide

> Complete developer and user documentation for `halpradio`'s sandboxed WebAssembly (Wasm) plugin engine and official curated registry.

---

## 🌟 Overview & Motivation

`halpradio` is designed to be lightweight and blazingly fast in your terminal. Rather than bloating the core binary with every third-party service, `halpradio` provides a **military-grade WebAssembly Plugin Architecture** that lets you extend the player with custom integrations:

- 📡 **Webhooks & Chat Bots**: Broadcast now-playing tracks to Discord, Slack, or Telegram in real-time.
- 🏠 **Home Automation**: Sync Philips Hue or Home Assistant smart lights to radio state changes.
- 📊 **Scrobblers & Stats**: Track listening habits, top stations, and listening time locally or cloud-synced.
- 🎧 **Hardware Controllers**: Hook into stream changes for Stream Decks, Raspberry Pi LCD screens, or LED strips.
- 🎨 **Visualizers & Lyrics**: Fetch live song lyrics or create custom ASCII animations.

All plugins execute inside an isolated **WebAssembly Virtual Machine** with zero unauthorized access to your filesystem, network, or OS processes.

---

## 🛡️ Security Architecture & Capabilities

`halpradio` uses [`tetratelabs/wazero`](https://github.com/tetratelabs/wazero), a high-performance, pure Go WebAssembly runtime with **zero CGo dependencies**.

```
┌─────────────────────────────────────────────────────────────┐
│                    halpradio Core TUI                       │
│    (Bubble Tea Event Loop • Audio Player • Stations)        │
└──────────────────────────────┬──────────────────────────────┘
                               │ Asynchronous Event Dispatch
                               ▼
┌─────────────────────────────────────────────────────────────┐
│            Wazero Sandboxed WebAssembly Engine              │
│                                                             │
│  ┌───────────────────────────────────────────────────────┐  │
│  │ Guest Plugin VM: "webhook-broadcaster"                │  │
│  │   • Isolated Memory (max 16 MB)                       │  │
│  │   • Non-blocking execution (timeout 2s)               │  │
│  └───────────────────────────┬───────────────────────────┘  │
│                              │ Capability Checked API Calls │
│                              ▼                              │
│  ┌───────────────────────────────────────────────────────┐  │
│  │ Host Security Gatekeeper (manifest.yaml enforcement)  │  │
│  │   • Network: Check domain against whitelist           │  │
│  │   • Storage: Sandboxed directory only                 │  │
│  │   • User Permission Approval Check                    │  │
│  └───────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
```

### 1. Default-Deny Security Model
Plugins run in a strict sandbox:
- **No filesystem access** outside the plugin's dedicated data directory.
- **No network access** unless explicit domains or CIDRs are declared in `manifest.yaml` and approved by the user.
- **No environment variable or process execution** capabilities.

### 2. Interactive Permission Approval
When a plugin is installed, `halpradio` presents an explicit permission prompt showing requested capabilities (e.g. `🌐 Network: api.discord.com`, `💾 Storage: Local Sandbox`). The plugin remains disabled until you approve it.

---

## 🖥️ User Guide: Managing Plugins

### 1. In-App Plugin Manager Modal (`P` Key)
Press `P` (or `<space>p`) anywhere in `halpradio` to open the **Plugin & Extension Manager**:

- **`[1] 📦 Installed` Tab**:
  - `Space` / `Enter`: Toggle plugin enable/disable.
  - `p`: View requested permissions & toggle approval status.
  - `d` or `x`: Uninstall plugin and clean up data.
  - `u`: Check and apply updates from the official registry.
- **`[2] 🌐 Official Registry` Tab**:
  - `Tab` / `1` / `2` / `h` / `l`: Switch between Installed and Registry views.
  - `i` / `Enter`: Download and install a plugin from the official registry.
  - `Esc` / `q`: Close the modal.

---

### 2. CLI Plugin Management
You can also manage plugins directly from your command line:

```bash
# List all installed plugins and available registry plugins
halpradio plugin list

# Install a plugin from the official registry
halpradio plugin install webhook-broadcaster
halpradio plugin install scrobble-logger

# Enable or disable a plugin
halpradio plugin enable webhook-broadcaster
halpradio plugin disable webhook-broadcaster

# Update installed plugins to the latest release
halpradio plugin update --all

# Remove a plugin
halpradio plugin remove webhook-broadcaster
```

---

### 3. File Paths & Storage Layout

| Resource | Path | Description |
|---|---|---|
| **Installed Plugins** | `~/.config/halpradio/plugins/<plugin-id>/` | Contains `manifest.yaml` and `plugin.wasm`. |
| **Plugin Data Storage** | `~/.config/halpradio/plugins_data/<plugin-id>/` | Sandboxed persistent data (`storage.json`). |
| **Plugin State Registry** | `~/.config/halpradio/plugins.json` | Stores enabled states & permission approvals. |

---

## 🧑‍💻 Developer Tutorial: "Build Your First Plugin in 5 Minutes"

Plugins can be authored in any language that compiles to WebAssembly (`.wasm`), including **Go**, **Rust**, **TypeScript/AssemblyScript**, **Zig**, and **C**.

### Step 1: Create the Plugin Manifest (`manifest.yaml`)

Create a directory `my-awesome-plugin` with a `manifest.yaml`:

```yaml
id: "my-awesome-plugin"
name: "My Awesome Plugin"
version: "1.0.0"
author: "your_name"
description: "Broadcasts song changes or logs listening history."
homepage: "https://github.com/yourname/my-awesome-plugin"
wasm_file: "plugin.wasm"

permissions:
  network:
    - "*.discord.com"           # Domain whitelist (or "*" for all)
  storage:
    - "local"                   # Sandboxed storage permission
  events:
    - "on_track_change"         # Lifecycle events to listen for
    - "on_playback_change"
```

---

### Step 2: Write Plugin Code in Go

Using Go 1.21+ and the official [`halpradiosdk`](https://github.com/halpworld/halpradio-plugins/tree/main/sdk/go/halpradiosdk):

```go
package main

import (
	"fmt"
	"unsafe"

	"github.com/halpworld/halpradio-plugins/sdk/go/halpradiosdk"
)

// Required entry point for wasip1
func main() {}

var memoryBuffer []byte

//export alloc
func alloc(size uint32) uint32 {
	if size == 0 {
		return 0
	}
	memoryBuffer = make([]byte, size)
	return uint32(uintptr(unsafe.Pointer(&memoryBuffer[0])))
}

//export free
func free(ptr uint32, size uint32) {
	memoryBuffer = nil
}

//export on_init
func onInit(ptr uint32, length uint32) uint32 {
	halpradiosdk.Log(halpradiosdk.LogLevelInfo, "My Awesome Plugin initialized!")
	return 0
}

//export on_track_change
func onTrackChange(ptr uint32, length uint32) uint32 {
	if length == 0 {
		return 0
	}
	data := unsafe.Slice((*byte)(unsafe.Pointer(uintptr(ptr))), length)
	payload, err := halpradiosdk.ParseTrackChange(data)
	if err != nil {
		return 1
	}

	// 1. Log to halpradio logs
	halpradiosdk.Log(halpradiosdk.LogLevelInfo, fmt.Sprintf("Now playing: %s - %s", payload.Artist, payload.Title))

	// 2. Flash a message on halpradio's status bar
	halpradiosdk.Flash(fmt.Sprintf("🎵 %s", payload.Title))

	// 3. Save to sandboxed local storage
	_ = halpradiosdk.StorageSet("last_song", payload.Title)

	return 0
}

//export on_playback_change
func onPlaybackChange(ptr uint32, length uint32) uint32 {
	if length == 0 {
		return 0
	}
	data := unsafe.Slice((*byte)(unsafe.Pointer(uintptr(ptr))), length)
	payload, err := halpradiosdk.ParsePlaybackChange(data)
	if err != nil {
		return 1
	}

	halpradiosdk.Log(halpradiosdk.LogLevelDebug, fmt.Sprintf("Playback status: %s", payload.Status))
	return 0
}
```

---

### Step 3: Compile to WebAssembly

Compile directly using Go's built-in `wasip1` compiler target (zero external tools needed):

```bash
GOOS=wasip1 GOARCH=wasm go build -ldflags="-s -w" -o plugin.wasm main.go
```

---

### Step 4: Test Locally in halpradio

1. Create a plugin directory in your local config:
   ```bash
   mkdir -p ~/.config/halpradio/plugins/my-awesome-plugin
   ```
2. Copy `manifest.yaml` and `plugin.wasm`:
   ```bash
   cp manifest.yaml plugin.wasm ~/.config/halpradio/plugins/my-awesome-plugin/
   ```
3. Start `halpradio`, press `P`, select `My Awesome Plugin`, and press `p` or `Space` to approve permissions and enable it!

---

## 🦀 Building Plugins in Rust

Rust developers can compile plugins using `wasm32-wasip1`:

```rust
// Cargo.toml
// [lib]
// crate-type = ["cdylib"]

#[link(wasm_import_module = "halpradio")]
extern "C" {
    fn log(level: u32, ptr: *const u8, len: u32);
    fn ui_flash(ptr: *const u8, len: u32);
}

#[no_mangle]
pub extern "C" fn on_track_change(ptr: *const u8, len: u32) -> u32 {
    let msg = "Hello from Rust plugin!";
    unsafe {
        ui_flash(msg.as_ptr(), msg.len() as u32);
    }
    0
}
```

Build with:
```bash
cargo build --target wasm32-wasip1 --release
cp target/wasm32-wasip1/release/my_plugin.wasm plugin.wasm
```

---

## 📚 Host Functions Reference

The `halpradio` runtime exposes the following host functions to guest WebAssembly modules under the `"halpradio"` namespace:

| Function | Signature | Capability Required | Description |
|---|---|---|---|
| `halpradio.log` | `(level: u32, ptr: u32, len: u32)` | None | Emits a log entry (`0: debug`, `1: info`, `2: warn`, `3: error`). |
| `halpradio.ui_notify` | `(title_ptr: u32, title_len: u32, msg_ptr: u32, msg_len: u32)` | None | Triggers a desktop and/or in-app notification banner. |
| `halpradio.ui_flash` | `(msg_ptr: u32, msg_len: u32)` | None | Shows a temporary status message in the bottom bar. |
| `halpradio.storage_get` | `(key_ptr: u32, key_len: u32, out_ptr: u32, max_len: u32) -> u32` | `storage: ["local"]` | Reads a string value from sandboxed `storage.json`. Returns bytes written. |
| `halpradio.storage_set` | `(key_ptr: u32, key_len: u32, val_ptr: u32, val_len: u32) -> u32` | `storage: ["local"]` | Writes a key-value pair to sandboxed `storage.json`. Returns `0` on success. |
| `halpradio.http_fetch` | `(url_ptr: u32, url_len: u32, method_ptr: u32, method_len: u32, body_ptr: u32, body_len: u32, out_ptr: u32, max_len: u32) -> u32` | `network: ["domain"]` | Performs an HTTP/HTTPS request against a permitted URL. Returns bytes written. |

---

## ⚡ Guest Lifecycle Hooks Reference

Plugins can export any combination of the following lifecycle hooks:

| Hook | Signature | Trigger Event | Payload Format |
|---|---|---|---|
| `on_init` | `(cfg_ptr: u32, cfg_len: u32) -> u32` | Plugin loaded at app launch | JSON object of plugin settings |
| `on_track_change` | `(ptr: u32, len: u32) -> u32` | New song / ICY stream metadata | `{"station": "...", "artist": "...", "title": "...", "bitrate": 128, "codec": "MP3", "timestamp": "..."}` |
| `on_playback_change` | `(ptr: u32, len: u32) -> u32` | Play / Pause / Stop / Volume | `{"status": "playing", "volume": 80, "backend": "mpv", "station": "..."}` |
| `on_timer_tick` | `(ptr: u32, len: u32) -> u32` | Timer / Pomodoro progress tick | `{"mode": "pomodoro", "state": "focus", "remaining_seconds": 1500, "total_seconds": 1500}` |
| `alloc` / `malloc` | `(size: u32) -> u32` | Memory buffer allocation | Host calls this to allocate memory before passing payloads |
| `free` / `dealloc` | `(ptr: u32, size: u32)` | Memory buffer deallocation | Host calls this after hook execution completes |

---

## 🚀 Publishing to the Official Registry

Want to share your plugin with the community?

1. Fork [**`halpworld/halpradio-plugins`**](https://github.com/halpworld/halpradio-plugins).
2. Add your plugin folder under `plugins/<your-plugin-id>/` containing:
   - `manifest.yaml`
   - Source code (`main.go`, `src/`, etc.)
   - `Makefile`
   - `README.md`
   - Compiled `plugin.wasm`
3. Calculate your binary's SHA-256 checksum:
   ```bash
   shasum -a 256 plugin.wasm
   ```
4. Add your plugin's metadata to [`registry.json`](https://github.com/halpworld/halpradio-plugins/blob/main/registry.json).
5. Open a Pull Request! Our automated CI will verify your build and security checks.

---

## 💬 Questions & Support

- 💬 [GitHub Discussions](https://github.com/halpworld/halpradio/discussions)
- 🐛 [Issue Tracker](https://github.com/halpworld/halpradio/issues)
- 🌐 [Official Plugin Registry Repository](https://github.com/halpworld/halpradio-plugins)
