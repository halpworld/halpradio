# 🎙️ Local AI Radio DJ & Interactive Liner Notes Companion

`halpradio` includes a **100% local, zero-cloud-lag AI Radio DJ** and an **Interactive Liner Notes & Artist Biography Drawer** that transform raw internet radio streams into an authentic, curated FM broadcast experience right inside your terminal.

<p align="center">
  <img src="./images/aidj.gif" alt="halpradio Local AI Radio DJ & Interactive Liner Notes Demo" width="850" />
</p>

---

## ✨ Key Capabilities

1. **📖 Interactive Liner Notes & Artist Biography Drawer (`i`)**
   - Sliding right-hand terminal drawer displaying **Artist Biography**, **Album & Recording Context**, **Discography Highlights**, and **Track Trivia & Broadcast Notes**.
   - Powered by **MusicBrainz** + **Wikipedia REST API** with a two-tier cache (in-memory LRU + 30-day on-disk JSON cache in `~/.cache/halpradio/ai/linernotes/`).
   - **100% Offline Fallback**: When offline or when a station only broadcasts station-level ICY headers, `halpradio` synthesizes rich station & genre liner notes locally so the drawer is never empty.

2. **🎙️ Local AI Radio DJ (`d` / `D`)**
   - **On-Demand Track Commentary (`d`)**: Press `d` at any time to have your AI DJ introduce the current track and station.
   - **Automatic Track Intros (`--ai-dj` or `ai_dj: true`)**: Automatically announces newly detected tracks on station/song transitions.
   - **Smooth Audio Ducking**: Automatically ducks stream volume to **20%** (`ai_dj_duck_level`) while the DJ speaks and restores playback volume to **100%** afterward without altering your saved master volume.
   - **Live Broadcast Banner**: Displays the active DJ persona, local TTS engine, spoken script, and live audio ducking status above the player bar.

3. **🔊 100% Local TTS Voice Synthesis (Zero Cloud API Keys)**
   - **Piper ONNX (`piper`)**: Ultra-fast local neural TTS using opt-in `.onnx` voice models from HuggingFace (`rhasspy/piper-voices`).
   - **Sherpa-ONNX / Kokoro-82M (`sherpa-onnx`)**: High-expressiveness 82M-parameter local neural TTS (`hexgrad/Kokoro-82M`).
   - **Zero-Download Native OS TTS (`native`)**: Works out of the box with **0 MB downloaded** via macOS `say`, Linux `espeak-ng` / `espeak` / `spd-say`, or Windows SAPI.
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
| `d` | Inside AI DJ Studio (`D`) | Download the highlighted ONNX voice model from HuggingFace |
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

## 📦 Opt-In HuggingFace ONNX Voice Models

`halpradio` **never downloads large voice models in the background without your consent**. Out of the box, it uses your operating system's native speech synthesizer (`say` on macOS, `espeak-ng` on Linux) with **0 MB of extra disk usage**.

To upgrade to local neural ONNX voices:
1. Press `D` in `halpradio` to open the **AI DJ Studio & ONNX Voice Manager**.
2. Use `j` / `k` to highlight any voice model in the catalog:
   - `en_US-lessac-medium` (~63 MB — Piper US Warm)
   - `en_US-ryan-medium` (~63 MB — Piper US Retro Broadcaster)
   - `en_US-amy-medium` (~63 MB — Piper US Cyberpunk)
   - `en_GB-alan-medium` (~63 MB — Piper UK BBC Curator)
   - `kokoro-en-v0_19` (~82 MB — Sherpa-ONNX Kokoro-82M)
3. Press `d` to download the `.onnx` weights and `.onnx.json` config directly from HuggingFace into `~/.cache/halpradio/ai/models/`.

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
ai_dj_voice_model: "en_US-lessac-medium"  # Active ONNX model ID in ~/.cache/halpradio/ai/models/
ai_dj_ollama_url: "http://127.0.0.1:11434"# Optional local Ollama endpoint
ai_dj_ollama_model: ""                    # Optional local Ollama model (e.g. "llama3.2:1b")
ai_dj_duck_level: 20                      # Stream volume percentage (5-100) while DJ speaks
```
