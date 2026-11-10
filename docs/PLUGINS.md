# 🔌 Plugin & Extension System Guide

> Complete developer and user documentation for `halpradio`'s sandboxed WebAssembly (Wasm) plugin engine and official curated registry.

---

## 🌟 Overview & Motivation

`halpradio` is designed to be lightweight and blazingly fast in your terminal. Rather than bloating the core binary with every third-party service, `halpradio` provides a **sandboxed WebAssembly plugin system** that lets you extend the player with custom integrations:

- 📡 **Webhooks & Chat Bots**: Broadcast now-playing tracks to Discord, Slack, or Telegram in real-time.
- 🏠 **Home Automation**: Sync Philips Hue or Home Assistant smart lights to radio state changes.
- 📊 **Scrobblers & Stats**: Track listening habits, top stations, and listening time locally or cloud-synced.
- 🎧 **Hardware Controllers**: Hook into stream changes for Stream Decks, Raspberry Pi LCD screens, or LED strips.
- 🎨 **Visualizers & Lyrics**: Fetch live song lyrics or create custom ASCII animations.

Plugins execute inside an isolated **WebAssembly virtual machine**. They get no direct access to your filesystem, environment or processes, and reach the network and their storage only through the host functions listed below, within the permissions you approved. A sandbox limits what a plugin can do; it does not make a plugin trustworthy, so only install plugins whose permissions make sense to you.

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
│  │   • Isolated Memory (max 32 MiB)                      │  │
│  │   • Bounded execution (init 3s, each hook 2s)         │  │
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
Plugins run in a sandbox:
- **No filesystem access** outside the plugin's dedicated data directory (and only with `storage` permission).
- **No network access** unless hosts or CIDRs are declared in `manifest.yaml` and approved by you. Requests go out directly: proxy environment variables (`HTTP_PROXY`, `HTTPS_PROXY`) are ignored for plugin traffic, because a proxy would hide the real destination from the address check.
- **No environment variable or process execution** capabilities.
- **Bounded execution**: `_start` plus `on_init` must finish within 3 seconds and each event hook within 2 seconds. A plugin that runs over is interrupted and stopped (its error shows in the Plugin Manager). Events are queued per plugin (at most 16); while a plugin is busy, further events are dropped.
- **Memory**: at most 32 MiB of linear memory. A module must export its linear memory, or it is refused.
- **Module limits**: before a plugin is compiled, halpradio checks every resource the module could make halpradio allocate, so a small file cannot claim a large amount of host memory. A module is refused if it declares a memory larger than 32 MiB, more than one memory, a table with more than 131,072 entries or more than 8 tables, more than 32,768 locals in one function (2,097,152 in total), imports anything other than functions, or declares more entries in any section than the file actually contains. Table maximums are lowered to 131,072 entries, so `table.grow` past that fails inside the plugin instead of growing host memory. Custom sections (debug names, producer info) are dropped before compiling; they are still part of the approved file hash. Every type, kind, flag and reference-type byte must be one of the values halpradio checks for; anything else (for example typed function references) is refused. Compiling a plugin to machine code must finish within 10 seconds; the limit is checked between functions, so a single function is bounded only by the 20 MiB file limit. A plugin built with the standard Go toolchain (`GOOS=wasip1`) loads within these limits; this is covered by a test. TinyGo and Rust output has not been tested against them yet.

### 2. Permission Approval
- A plugin runs **no code at all** until you approve its permissions: it is not instantiated, and neither `_start` nor `on_init` runs. Plugins you install, or copy into the plugins folder, start **disabled and unapproved**.
- An approval covers **one exact binary and one exact permission set**: halpradio records the SHA-256 of `plugin.wasm` and the approved permissions. If either changes (an update, a reinstall, or someone replacing the files), the approval is void and the plugin stays stopped until you approve it again. The hash is checked again every time the plugin is loaded.
- A plugin runs with the permissions you approved, never with wider ones.
- Revoking approval stops the plugin immediately.
- Approvals from older halpradio versions, which did not record a hash, must be given once more.
- In the TUI the permission prompt shows the plugin's name, ID, version and binary SHA-256 at the top at all times. The rules are listed most sensitive first (wildcards, then local-network rules) and are never shortened: a long rule wraps. When they do not fit, scroll with `j`/`k`, the arrow keys or `PgUp`/`PgDn`; `y` is refused until the end of the list has been on screen, and a window too small to show the rules accepts no approval. Approving takes two keys, `y` and then `Enter`; any other key in between cancels, keys are ignored for one second after the prompt appears, and `n`, `q` or `Esc` decline at once.
- A plugin that finishes installing while you are in another dialog or typing does not open its prompt over it: it stays disabled and the status bar says to open Plugins (`P`) to review it.
- Status messages and desktop notifications from a plugin start with `[plugin:<id>]` (or `[plugin]` when the sender cannot be told apart), so a plugin cannot pass its text off as halpradio's own.

### 3. Network Rules
Network permissions are host names (`api.example.com`, `*.example.com`), `*`, IP addresses or CIDR ranges.
- `*` and broad ranges such as `0.0.0.0/0`, `::/0` or `0.0.0.0/1` reach **public addresses only**.
- This computer (loopback), your local network (private ranges), link-local and cloud metadata addresses are reachable only when a rule names them **explicitly and narrowly**, for example `127.0.0.1` or `192.168.1.0/24`: the rule must lie entirely inside one such block. The same applies to host names such as `localhost`, `*.local` and `*.internal`.
- Rules that reach local addresses are flagged as a warning in the permission prompt.
- `0.0.0.0/8`, `::` and IPv4-compatible addresses (`::a.b.c.d`) are never reachable, whatever the rules say, because connecting to `0.0.0.0` or `::` reaches this computer. A rule that names only such addresses (for example `0.0.0.0/8` or `::/96`) makes the manifest invalid.
- A manifest has at most 32 network rules, and each is at most 253 characters (a host name label at most 63), so the permission prompt can always show every rule in full.
- Every connection is checked again against the resolved IP address at connect time, and each redirect is checked against the rules.
- IPv6 forms that lead to an IPv4 address are judged by that IPv4 address: IPv4-mapped (`::ffff:a.b.c.d`), NAT64 (`64:ff9b::a.b.c.d`) and 6to4 (`2002:aabb:ccdd::`). So `64:ff9b::7f00:1` counts as loopback. IPv4-compatible (`::a.b.c.d`), local-use NAT64 (`64:ff9b:1::/48`), Teredo (`2001::/32`), site-local (`fec0::/10`) and the other special-purpose ranges (unspecified, `0.0.0.0/8`, CGNAT `100.64.0.0/10`, benchmarking `198.18.0.0/15`, documentation, multicast and broadcast) are never treated as public.

### 4. Manifest Validation
Manifests (from the plugins folder and from the registry) are **rejected**, not cleaned up, when the ID, name, author, description, version, homepage or any permission entry contains control characters, terminal escape sequences, bidirectional or zero-width characters, or is longer than its limit (name 100, author 100, description 500 characters; version 32; at most 32 network rules). Permission entries must be well-formed: storage `local`, events from `on_track_change`, `on_playback_change`, `on_timer_tick` (or `*`).

A plugin folder must be named exactly like the plugin's `id`; a folder that claims another plugin's ID is ignored.

### 5. Registry Trust Model
What you can rely on today, and what you cannot:

**What protects you**
- The registry list and every plugin download travel over `https://`, so they cannot be changed on the way to you without breaking TLS.
- Every download must match the SHA-256 listed for it in the registry entry. This catches corrupted or swapped downloads, but the checksum comes from the same registry as the plugin, so it does not prove who published the plugin.
- Plugins run in the sandbox described above, with only the permissions you approve, inside the memory, time and network limits.
- Nothing runs until you approve it, and the approval covers that exact binary (by its hash) and that exact permission list. Any change needs a new approval.

**What does not protect you**
- **No publisher signature is checked yet.** No publisher key is built into halpradio, so registry trust rests on https, the checksum from that same registry, and your approval. A compromised or misconfigured registry (including a custom `plugin_registry_url`) can offer any plugin with any permission request. halpradio will download it and show you its permissions, but it cannot tell you whether the plugin is genuine. You are the last check: read the permission prompt, and do not approve a plugin you do not expect or one that asks for more than it needs.
- An entry that declares a signature is refused for now, because without a built-in key the signature cannot be verified. Once a publisher key ships, unsigned entries will be refused.

### 6. Messages from Plugins
Status bar messages (`ui_flash`) are prefixed with the plugin's name, `[Plugin Name] message`, and limited to 2 per second per plugin. Notifications (`ui_notify`) carry the plugin's name in the title and are limited to 5 per 10 seconds per plugin. Text from plugins is stripped of control, escape, bidirectional and zero-width characters.

---

## 🖥️ User Guide: Managing Plugins

### 1. In-App Plugin Manager Modal (`P` Key)
Press `P` (or `<space>p`) anywhere in `halpradio` to open the **Plugin & Extension Manager**:

- **`[1] 📦 Installed` Tab**:
  - `Space` / `Enter`: Toggle plugin enable/disable.
  - `p`: View requested permissions & toggle approval status.
  - `d` or `x`: Uninstall the plugin and delete its stored data.
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

# Review and approve a plugin's permissions
halpradio plugin approve webhook-broadcaster

# Enable or disable a plugin
halpradio plugin enable webhook-broadcaster
halpradio plugin disable webhook-broadcaster

# Update installed plugins to the latest release
halpradio plugin update

# Remove a plugin (also deletes its stored data)
halpradio plugin remove webhook-broadcaster
```

> **Note:** `install`, `enable` and `approve` show the plugin's permissions in full (warnings marked `!!`, long rules wrapped, never cut) and the SHA-256 of its `plugin.wasm`, then ask you to type `yes`. Keys typed before the prompt appears are discarded, and any answer other than `yes` declines.
>
> Without a terminal (scripts), approval takes two steps, so the approval names the exact binary you reviewed:
>
> ```bash
> halpradio plugin approve webhook-broadcaster            # prints the permissions and "Binary SHA-256: <hex>"; approves nothing
> halpradio plugin approve webhook-broadcaster --approve-sha256 <hex>
> ```
>
> `--approve-sha256` approves only when `<hex>` equals the SHA-256 of the installed `plugin.wasm`, and only the permission set that was printed in that run. A bare `--approve` is refused.

Registry behaviour:
- The registry and every plugin download must use `https://`; plain `http://` is refused, as are hosts that resolve only to local or private addresses. Downloads are size-limited (registry 2 MiB, plugin 20 MiB).
- Each entry must carry the SHA-256 of its `plugin.wasm`; the download must match it before anything is written to disk. The plugin folder is written atomically, so a failed install leaves the previous version in place.
- Invalid registry entries are skipped and never shown.
- Updates never install an older version than the one installed.
- If the registry cannot be reached, halpradio says so; there is no built-in fallback list.
- **Signatures:** halpradio can verify an Ed25519 publisher signature over each entry (ID, version, SHA-256 and permissions). No official publisher key is built in yet, so today an entry that declares a signature is refused (it cannot be verified) and unsigned entries are trusted on their checksum alone. Once a key is built in, unsigned entries will be refused. See [Registry Trust Model](#5-registry-trust-model).

---

### 3. File Paths & Storage Layout

| Resource | Path | Description |
|---|---|---|
| **Installed Plugins** | `~/.config/halpradio/plugins/<plugin-id>/` | Contains `manifest.yaml` and `plugin.wasm`. |
| **Plugin Data Storage** | `~/.config/halpradio/plugins_data/<plugin-id>/` | Sandboxed persistent data (`storage.json`). Deleted when the plugin is uninstalled. |
| **Plugin State Registry** | `~/.config/halpradio/plugins.json` | Enabled states and approvals (binary SHA-256 and approved permissions). Written with mode `0600`. |

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

1. Create a plugin directory in your local config (the folder name must equal the manifest `id`):
   ```bash
   mkdir -p ~/.config/halpradio/plugins/my-awesome-plugin
   ```
2. Copy `manifest.yaml` and `plugin.wasm`:
   ```bash
   cp manifest.yaml plugin.wasm ~/.config/halpradio/plugins/my-awesome-plugin/
   ```
3. Start `halpradio`, press `P`, select `My Awesome Plugin`, press `p` to review and approve its permissions, then enable it.
4. Every time you rebuild `plugin.wasm` the hash changes, so approve the plugin again after each build.

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
| `halpradio.log` | `(level: u32, ptr: u32, len: u32)` | Approval | Emits a log entry (`0: debug`, `1: info`, `2: warn`, `3: error`). |
| `halpradio.ui_notify` | `(title_ptr: u32, title_len: u32, msg_ptr: u32, msg_len: u32)` | Approval | Triggers a notification, titled with the plugin's name. Max 5 per 10 s. |
| `halpradio.ui_flash` | `(msg_ptr: u32, msg_len: u32)` | Approval | Shows `[Plugin Name] message` in the bottom bar. Max 2 per second; extra calls are dropped. |
| `halpradio.storage_get` | `(key_ptr: u32, key_len: u32, out_ptr: u32, max_len: u32) -> u32` | `storage: ["local"]` | Reads a string value from sandboxed `storage.json`. Returns bytes written. |
| `halpradio.storage_set` | `(key_ptr: u32, key_len: u32, val_ptr: u32, val_len: u32) -> u32` | `storage: ["local"]` | Writes a key-value pair to sandboxed `storage.json`. Returns `0` on success. |
| `halpradio.http_fetch` | `(url_ptr: u32, url_len: u32, method_ptr: u32, method_len: u32, body_ptr: u32, body_len: u32, out_ptr: u32, max_len: u32) -> u32` | `network: ["domain"]` | Performs an HTTP/HTTPS request against a permitted URL (5 s timeout, request body up to 1 MiB, response up to 5 MiB). Returns bytes written. |

Every host function requires that the plugin is approved; the storage and network functions additionally require the matching permission. A plugin that is not approved never runs, so in practice "Approval" means "available to every running plugin".

---

## ⚡ Guest Lifecycle Hooks Reference

Plugins can export any combination of the following lifecycle hooks:

| Hook | Signature | Trigger Event | Payload Format |
|---|---|---|---|
| `on_init` | `(cfg_ptr: u32, cfg_len: u32) -> u32` | Plugin started (after approval) | JSON object of plugin settings |
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
3. Calculate your binary's SHA-256 checksum (required; entries without a valid 64-character hex checksum are skipped):
   ```bash
   shasum -a 256 plugin.wasm
   ```
4. Add your plugin's metadata to [`registry.json`](https://github.com/halpworld/halpradio-plugins/blob/main/registry.json). The `download_url` must be `https://`, and all text fields must pass the manifest validation described above.
5. Open a Pull Request. Users must approve your plugin's permissions after installing it and again after every update.

---

## 💬 Questions & Support

- 💬 [GitHub Discussions](https://github.com/halpworld/halpradio/discussions)
- 🐛 [Issue Tracker](https://github.com/halpworld/halpradio/issues)
- 🌐 [Official Plugin Registry Repository](https://github.com/halpworld/halpradio-plugins)
