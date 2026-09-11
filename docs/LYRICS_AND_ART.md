# Synced Lyrics & Terminal Album Art 📜🖼️

`halpradio` renders a live, self-scrolling lyric sheet and real album artwork
without leaving the terminal. Press `L` for the lyrics drawer and `A` for the
full-size cover art viewer.

---

## 📜 The Lyrics Engine (`pkg/lyrics`)

### Providers

| Order | Provider | Endpoint | Key required |
|---|---|---|---|
| 1 | **LRCLIB** exact match | `GET /api/get?artist_name=&track_name=&album_name=&duration=` | no |
| 2 | **LRCLIB** search | `GET /api/search?artist_name=&track_name=` | no |
| 3 | **NetEase Cloud Music** | `GET /api/search/get` then `GET /api/song/lyric` | no |

The exact lookup is tried first because it takes the track duration into
account. When it misses, the search endpoint picks the best candidate,
preferring entries that carry timestamped lyrics and then the closest duration.
NetEase is the last resort and is treated as best-effort: an unexpected
response shape resolves to "no lyrics" rather than an error.

Instrumental tracks come back as a single `♪ Instrumental ♪` line rather than an
empty sheet, so the drawer can say why there is nothing to sing.

### LRC parsing

`ParseLRC` handles `[mm:ss]`, `[mm:ss.xx]` and `[mm:ss.xxx]` stamps, several
stamps sharing one line of text, and the `[offset:NNN]` tag, which shifts every
timestamp by that many milliseconds. Metadata tags (`[ar:]`, `[ti:]`, `[al:]`,
`[length:]`) are skipped. Lines come back sorted ascending.

### Splitting a radio stream title

ICY metadata is messy. `SplitTrackTitle` recognises `Artist - Title` with any
dash variant plus the lower-case `Title by Artist` form, and strips station
noise: wrapping quotes, `(Official Video)`, `[HQ]`, ` - Topic`, a trailing
` | StationName`, and advert or jingle slugs. When the input is only a station
name or an advert it returns two empty strings, and no lookup is attempted.

### Estimating playback position

Internet radio exposes no seek position, so the lyric clock starts the moment a
station announces a new title and runs from there. Two consequences:

- Stations that announce metadata a few seconds late or early drift. Press `,`
  and `.` to shift the sync in 0.5 second steps. The live value shows in the
  drawer footer, and `lyrics_offset_ms` in `config.yaml` makes a correction
  persistent.
- Joining a station mid-track starts the sheet from the top. The next track
  announcement re-syncs it.

### Caching

Sheets are memoised in RAM with a six hour time-to-live and written to
`~/.cache/halpradio/lyrics/` as one JSON file per track, named by a SHA-256 of
the normalised lookup key. Entries older than 30 days are stale. Tracks with no
match anywhere are negative-cached for an hour so a station full of unmatched
tracks does not hammer the providers. Every disk failure is non-fatal and falls
back to network-only operation.

---

## 🖼️ The Album Art Engine (`pkg/art`)

### Cover providers

| Order | Provider | Notes |
|---|---|---|
| 1 | **iTunes Search API** | fastest, no key; the `100x100bb` artwork path is rewritten to `600x600bb` |
| 2 | **Deezer** | `album.cover_xl`, no key |
| 3 | **MusicBrainz → Cover Art Archive** | recording query resolves a release MBID, then `front-500` |
| 4 | **Last.fm** | only when `lastfm_api_key` is set |

Downloads are capped at 8 MB, must present an image content type, and must
decode before they are accepted.

### Protocol detection

`art.Detect` reads the environment and picks the best transport:

| Priority | Protocol | Detected from |
|---|---|---|
| 1 | Kitty graphics | `TERM_PROGRAM=ghostty`, `TERM` containing `kitty`, `KITTY_WINDOW_ID`, `TERM_PROGRAM=WezTerm` |
| 2 | iTerm2 inline images | `TERM_PROGRAM=iTerm.app`, `ITERM_SESSION_ID` |
| 3 | Sixel | `TERM` containing `foot`, `mlterm`, `yaft` or `sixel`, or `HALPRADIO_SIXEL` |
| 4 | Truecolor half-block | `COLORTERM` of `truecolor`/`24bit`, or `TERM` containing `256color` |
| 5 | Braille | anything else |
| — | None | `TERM` empty or `dumb`, or `HALPRADIO_NO_ART` / `NO_GRAPHICS` set |

Overrides, highest precedence first:

```bash
HALPRADIO_NO_ART=1 halpradio              # no artwork for this run
HALPRADIO_ART_PROTOCOL=halfblock halpradio # force a transport for this run
```

```yaml
album_art_protocol: sixel   # force a transport permanently
```

Sixel detection is deliberately conservative: there is no reliable positive
signal for it beyond a handful of terminal names, and a Sixel payload sent to a
terminal that does not understand it prints as garbage. Set `HALPRADIO_SIXEL=1`
or `album_art_protocol: sixel` when you know your terminal supports it.

### The layout contract

Every renderer returns **exactly** the requested number of rows, and every
returned line reports the requested column count through `lipgloss.Width`. The
escape-sequence transports place the image at the cursor and pad their rows
with spaces, so Kitty, iTerm2 and Sixel output cannot shift the surrounding
Bubble Tea frame. This invariant is asserted per protocol in
`pkg/art/render_test.go`.

The cell-approximation renderers work on the pixels directly:

- **Half-block** packs two vertical pixels into one cell using `▀` with the
  upper pixel as foreground and the lower as background, emitted as truecolor
  SGR and reset at every line end.
- **Braille** maps a 2×4 pixel block to one Braille cell from a luminance
  threshold, coloured with the block's average colour.

Images are letterboxed to the target cell grid rather than stretched, so covers
stay square on every terminal.

Artwork is cached under `~/.cache/halpradio/art/` as the encoded image plus a
JSON sidecar holding the provider, origin URL and fetch time.

---

## 🖥️ How it fits the TUI

```text
┌─ 📻 CATALOG ──────────────────┬─ 📜 LIVE LYRICS ───────────────┐
│  ▶ SomaFM Groove Salad        │      ▄▄▄▄▄▄▄▄▄▄▄▄              │
│    Nightwave Plaza            │      █ ALBUM ART █             │
│    Radio Paradise             │      ▀▀▀▀▀▀▀▀▀▀▀▀              │
│    KEXP 90.3                  │       🖼 iTunes                 │
│                               │  Tycho - A Walk                │
│                               │    I've been wandering         │
│                               │  ► Searching for a signal ◄    │
│                               │    Everything is quiet         │
│                               │  ━━━━━━━━━━━───────            │
│                               │  ⏱ Synced via LRCLIB           │
│                               │  L close · , . sync            │
└───────────────────────────────┴────────────────────────────────┘
```

- The drawer **takes its own columns** rather than overlapping the station
  list, so the list keeps its own layout and selection.
- It needs an 80 column terminal, because the station list will not render
  below 28 columns beside an 18 column sidebar. Narrower terminals get a status
  message instead of a broken frame.
- Every lookup runs as a Bubble Tea command off the update loop, so a slow
  provider never blocks the keyboard. Results that arrive after the track has
  changed are dropped by comparing the track key.
- Artwork is rasterised in `pkg/ui/update.go` when a cover arrives, when the
  window is resized and when the drawer or modal opens, because the pixel grid
  depends on the surface size. Component views stay pure.
- While the analog frequency tuner is live, `L` keeps its existing meaning of
  sweeping the dial. Everywhere else it toggles the drawer.

### Keybindings

| Key | Action |
|---|---|
| `L` | Toggle the lyrics drawer and move focus into it |
| `A` | Toggle the full-size album art viewer |
| `j` / `k` | Scroll an unsynced sheet while the drawer has focus |
| `,` / `.` | Nudge the lyric sync back / forward by 0.5s |
| `h` | Return focus to the station list, leaving the drawer open |
| `Esc` | Close the drawer |

### Configuration

```yaml
lyrics_enabled: true        # LRCLIB / NetEase synced lyrics engine
lyrics_auto_open: false     # open the drawer on startup
lyrics_offset_ms: 0         # persistent sync correction in milliseconds
album_art_enabled: true     # terminal cover art renderer
album_art_protocol: auto    # auto | kitty | iterm2 | sixel | halfblock | braille | off
lastfm_api_key: ""          # optional extra cover art provider
```

---

## 🔐 Privacy & network behaviour

- Nothing is uploaded. Both engines send only the artist, title, album and
  duration already broadcast by the station, as query parameters.
- Requests identify themselves with a `halpradio/…` user agent, which LRCLIB
  and MusicBrainz both ask for.
- With both features disabled in `config.yaml`, neither engine is constructed
  and no request is ever made.
