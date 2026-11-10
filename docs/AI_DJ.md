# 🎙️ Local AI Radio DJ & Interactive Liner Notes Companion

`halpradio` includes a **100% local, zero-cloud-lag AI Radio DJ** and an **Interactive Liner Notes & Artist Biography Drawer** that transform raw internet radio streams into an authentic, curated FM broadcast experience right inside your terminal.

<p align="center">
  <img src="./images/aidj.gif" alt="halpradio Local AI Radio DJ & Interactive Liner Notes Demo" width="850" />
</p>

---

## ✨ Key Capabilities

1. **📖 Interactive Liner Notes & Artist Biography Drawer (`i`)**
   - Sliding right-hand terminal drawer displaying **Artist Biography**, **Album & Recording Context**, **Discography Highlights**, and **Track Trivia & Broadcast Notes**.
   - Powered by **MusicBrainz** + **Wikipedia REST API** with a two-tier cache (in-memory LRU + 30-day on-disk JSON cache in `<cache>/ai/linernotes/`; see [Where files live](#-where-files-live)).
   - **100% Offline Fallback**: When offline or when a station only broadcasts station-level ICY headers, `halpradio` synthesizes rich station & genre liner notes locally so the drawer is never empty.

2. **🎙️ Local AI Radio DJ (`d` / `D`)**
   - **On-Demand Track Commentary (`d`)**: Press `d` at any time to have your AI DJ introduce the current track and station.
   - **Automatic Track Intros (`--ai-dj` or `ai_dj: true`)**: Automatically announces newly detected tracks on station/song transitions.
   - **Smooth Audio Ducking**: Automatically ducks stream volume to **20%** (`ai_dj_duck_level`) while the DJ speaks and restores playback volume to **100%** afterward without altering your saved master volume.
   - **Live Broadcast Banner**: Displays the active DJ persona, local TTS engine, spoken script, and live audio ducking status above the player bar.

3. **🔊 100% Local TTS Voice Synthesis (Zero Cloud API Keys)**
   - **Piper voices (`piper`)**: Opt-in Piper VITS voices (lessac, ryan, amy, alan), packaged as sherpa-onnx voice bundles and run by the bundled sherpa-onnx runtime (or a `piper` binary already on your `PATH`).
   - **Sherpa-ONNX / Kokoro-82M (`sherpa-onnx`)**: High-expressiveness 82M-parameter local neural TTS (`kokoro-int8-en-v0_19` bundle).
   - **Zero-Download Native OS TTS (`native`)**: Works out of the box with **0 MB downloaded** via macOS `say` or Linux `espeak-ng` / `espeak` / `spd-say`. Windows has no native engine: unless a sherpa-onnx voice or `piper` is installed, the DJ uses Visual Broadcast Mode there.
   - **Visual Broadcast Mode (`silent`)**: Displays the live DJ script and ducks audio briefly without vocal synthesis.

4. **🧠 Optional Local LLM Generation (`Ollama`) + Zero-Latency Templates**
   - Connect a local [Ollama](https://ollama.com) instance (`ai_dj_ollama_model: "llama3.2:1b"`) for dynamic, context-aware radio commentary.
   - When no local LLM is configured (default), `halpradio` uses **zero-latency deterministic persona templates** enriched with MusicBrainz release year, album, and artist origin city.

---

## ⌨️ Keyboard Shortcuts

| Key | Context | Action |
|---|---|---|
| `i` | Anywhere | Toggle the **Interactive Liner Notes & Artist Biography Drawer** |
| `j` / `k` or `↓` / `↑` | Liner Notes Drawer | Scroll artist biography, discography & trivia |
| `g` / `G` | Liner Notes Drawer | Jump to top / bottom of liner notes |
| `d` | Main UI / Liner Notes | Trigger **on-demand Local AI Radio DJ** track intro & audio ducking |
| `D` | Anywhere | Open the **AI DJ Studio & ONNX Voice Manager** modal |
| `d` | Inside AI DJ Studio (`D`) | Download the highlighted voice (and, if needed, the sherpa-onnx runtime) from the `k2-fsa/sherpa-onnx` GitHub releases |
| `t` | Inside AI DJ Studio (`D`) | Trigger a live test DJ announcement |
| `Esc` | Drawer / Modal | Close the Liner Notes drawer or AI DJ Studio modal |

---

## 🎭 Built-in DJ Personas

Cycle personas interactively inside the **AI DJ Studio (`D`)**, via CLI flag (`--dj-persona`), or in `~/.config/halpradio/config.yaml`:

| Persona ID | Display Name | Broadcast Style & Vibe | Recommended ONNX Voice |
|---|---|---|---|
| `late-night-chill` *(default)* | **Late Night Chill** | Warm, laid-back SomaFM & midnight FM host | `en_US-lessac-medium` |
| `retro-80s` | **Retro 80s Broadcaster** | High-energy analog FM dial & neon synthwave host | `en_US-ryan-medium` |
| `cyberpunk` | **Cyberpunk Host** | Night City underground pirate signal & chrome frequencies | `en_US-amy-medium` |
| `bbc-presenter` | **BBC Presenter** | Knowledgeable BBC Radio 6 Music crate-digger & curator | `en_GB-alan-medium` |

---

## 📦 Opt-In Neural Voice Downloads

`halpradio` **never downloads voice models in the background without your consent**. Out of the box, it uses your operating system's native speech synthesizer (`say` on macOS, `espeak-ng` / `espeak` / `spd-say` on Linux) with **0 MB of extra disk usage**.

To upgrade to local neural voices:
1. Press `D` in `halpradio` to open the **AI DJ Studio & ONNX Voice Manager**.
2. Use `j` / `k` to highlight any voice model in the catalog (sizes are the compressed download):
   - `en_US-lessac-medium` (~21 MB — Piper US Warm)
   - `en_US-ryan-medium` (~21 MB — Piper US Retro Broadcaster)
   - `en_US-amy-medium` (~21 MB — Piper US Cyberpunk)
   - `en_GB-alan-medium` (~21 MB — Piper UK BBC Curator)
   - `kokoro-en-v0_19` (~98 MB — Sherpa-ONNX Kokoro-82M)
3. Press `d` to download it.

### What is downloaded, and from where

Everything comes from the GitHub releases of [`k2-fsa/sherpa-onnx`](https://github.com/k2-fsa/sherpa-onnx/releases). Nothing is fetched from HuggingFace.

| What | Release tag | Installed to |
|---|---|---|
| The sherpa-onnx runtime: the `sherpa-onnx-offline-tts` executable plus its shared libraries (the shared build for your OS/CPU: macOS arm64/x64, Linux x64/aarch64, Windows x64/arm64) | `v1.13.8` | `<cache>/ai/models/_runtime/` |
| The voice bundle you picked (`.onnx` model, `tokens.txt`, `espeak-ng-data`) | `tts-models` | `<cache>/ai/models/<bundle>/` |

The runtime is only downloaded when no verified runtime is installed and no standalone `piper` binary is on your `PATH`.

### How downloads are checked

- **SHA-256 pinning.** Every archive URL above has an expected SHA-256 digest and byte size compiled into `halpradio` (`pkg/ai/tts.go`). An archive with no pin is refused before any request is made. The download is hashed while it is written; a size or digest mismatch deletes it and nothing is extracted.
- **HTTPS and a host allow-list.** Downloads and every redirect must be `https` to `github.com`, `objects.githubusercontent.com` or `release-assets.githubusercontent.com` (port 443, no credentials in the URL), at most 5 redirects.
- **Careful extraction.** Only regular files and directories under the expected top-level folder are extracted; links, devices and `..` paths are skipped. Total size, per-file size and entry count are capped. Only the runtime executable gets the execute bit. Extraction happens in a staging folder that is moved into place only when it is complete.
- **Re-checked before every run.** The runtime install writes `.halpradio-manifest.json` with the SHA-256 of every file it extracted. Before each DJ announcement the executable and libraries are re-hashed against it. A missing manifest or any changed file means the bundled runtime is not used (`halpradio` falls back to a `sherpa-onnx-offline-tts` / `piper` on your `PATH`, or to the native/visual engine), and the next voice download reinstalls it. This catches a damaged or half-written install. It is not protection against another program running as your user: the manifest sits in the same folder as the files it describes, so anything that can change the runtime can change the manifest too.
- **Spoken text is plain text.** DJ scripts are stripped of control and escape sequences, capped at 600 characters, and never start with a dash, so text cannot be read as a command-line option by the TTS program.

> **Note — rolling release tag.** The voice bundles live under the `tts-models` tag, which upstream can update in place. If upstream replaces a bundle, its digest no longer matches the pin and `halpradio` refuses the download (you will see a checksum error) until a new `halpradio` release updates the pin. Voices you already downloaded keep working. The runtime tag `v1.13.8` is a fixed version.

> **Upgrading from an older version.** Runtimes installed by earlier versions have no manifest, so they are not used; download a voice again from the AI DJ Studio to reinstall the runtime.

### Where files live

`<cache>` is your OS cache directory plus `halpradio`:

| OS | `<cache>` |
|---|---|
| macOS | `~/Library/Caches/halpradio` |
| Linux | `$XDG_CACHE_HOME/halpradio` (default `~/.cache/halpradio`) |
| Windows | `%LocalAppData%\halpradio` |

Voice models are in `<cache>/ai/models/` (folder mode 0700).

---

## 🌐 What leaves your machine

| Feature | Sent to | Data sent |
|---|---|---|
| Liner notes (`i`) | `musicbrainz.org` | Artist and track title of the current song |
| Liner notes (`i`) | `en.wikipedia.org` | Artist name |
| Ollama DJ scripts (only if `ai_dj_ollama_model` is set) | Your `ai_dj_ollama_url` | Station name, genre, artist, track, album, year, artist origin, the persona's system prompt and the model name |
| Voice download (`d` in the Studio) | GitHub (see above) | Normal HTTPS download requests |

Replies are treated as untrusted:

- **Ollama.** The URL must be `https`, or plain `http` to a loopback host only (`localhost`, `127.0.0.0/8`, `::1`). It must not contain credentials, a query or a fragment. A refused URL turns Ollama off and the DJ uses its offline templates. Redirects are not followed, the reply is capped at 64 KiB, and the script is cleaned of control and escape sequences and cut to 55 words / 400 characters.
- **MusicBrainz / Wikipedia.** Replies are size-capped (1 MiB), and every text field is cleaned of control and escape sequences and length-capped before it is shown or cached. The on-disk cache (files up to 256 KiB) is cleaned again when read.
- **ICY metadata.** Station-supplied titles are cleaned and length-capped before they are used in a prompt, a template or speech.

---

## ⚙️ Configuration & CLI Flags

### Command-Line Flags

```bash
# Launch halpradio with automatic AI DJ track intros enabled
halpradio --ai-dj

# Launch with a specific DJ persona
halpradio --ai-dj --dj-persona=cyberpunk
halpradio --ai-dj --dj-persona=bbc-presenter
```

### `~/.config/halpradio/config.yaml`

```yaml
# Local AI Radio DJ & Interactive Liner Notes Companion
ai_dj: false                              # Auto-announce tracks on change ('d' always works on-demand)
ai_dj_persona: "late-night-chill"         # late-night-chill | retro-80s | cyberpunk | bbc-presenter
ai_dj_tts_engine: "auto"                  # auto | piper | sherpa-onnx | native | silent
ai_dj_voice_model: "en_US-lessac-medium"  # Active voice ID in <cache>/ai/models/
ai_dj_ollama_url: "http://127.0.0.1:11434"# Ollama endpoint: https, or http to localhost/127.0.0.1/::1 only
ai_dj_ollama_model: ""                    # Optional local Ollama model (e.g. "llama3.2:1b")
ai_dj_duck_level: 20                      # Stream volume percentage (5-100) while DJ speaks
```
