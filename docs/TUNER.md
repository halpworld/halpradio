# Analog Frequency Tuner (Status: On Hold / Experimental) 📻

> [!NOTE]
> The Analog Ham Radio Frequency Tuner feature is currently **on hold** and disabled by default pending a better future design and implementation.

---

## 📌 Overview

The Analog Frequency Tuner was prototyped as a virtual analog radio dial enabling continuous frequency sweeping across FM, AM, and Shortwave (SW) bands with procedural atmospheric static noise crossfades.

Following user feedback on audio ergonomics and usability, the feature has been removed from the standard user-facing TUI and audio pipeline and placed behind an opt-in experimental feature flag.

---

## ⚙️ Enabling the Experimental Tuner

If you wish to test or develop against the experimental tuner prototype:

### 1. Command-Line Flag
Launch `halpradio` with the `--experimental-tuner` flag:

```bash
halpradio --experimental-tuner
```

### 2. Configuration File
Set `experimental_tuner: true` in `~/.config/halpradio/config.yaml`:

```yaml
# ~/.config/halpradio/config.yaml
experimental_tuner: true
```

---

## 🎮 Controls (When Experimental Flag is Enabled)

When enabled:
- **`0` or `F`**: Toggle between the 3D ASCII Globe Explorer and the Analog Frequency Tuner on Tab 9.
- **`h` / `l`**: Fine frequency dial sweep (Left / Right).
- **`H` / `L`**: Fast dial sweep (coarse frequency steps).
- **`b`**: Cycle frequency band (`FM` 87.5–108 MHz, `AM` 530–1700 kHz, `SW` 3.2–22 MHz).
- **`n` / `N`**: Seek to next / previous station carrier frequency on the dial.
- **`Space`**: Mute / unmute audio stream.
- **`9`**: Return to 3D Globe Explorer.

When disabled (default):
- Tab 9 is purely the 3D ASCII Globe Explorer.
- The tuner dial, static noise audio generator, and tuner key shortcuts (`0`, `F`, `b`, `H`, `L`) are completely deactivated.
- Key `0` behaves as standard audio mute toggle.

---

## 🔮 Roadmap & Future Redesign Considerations

The feature is slated for a ground-up redesign. Areas identified for the next iteration include:
1. **Audio Synthesis Architecture**: Independent audio mixing layer for procedural noise vs network audio streams without thread contention or stream stalling.
2. **True Analog Tuning Experience**: Smoother Doppler/heterodyne whistle effects, harmonic distortion, and authentic band-pass filtering.
3. **WebSDR & Real RF Feeds**: Investigating integration with public WebSDR servers (e.g. KiwiSDR) for live terrestrial Shortwave and amateur radio reception.
4. **Ergonomic UI**: Redesigned frequency dial graphics and clearer station locking indicators.
