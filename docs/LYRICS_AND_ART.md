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

Lyric text is untrusted. Every line is stripped of terminal control sequences
(escape sequences such as OSC 52 clipboard writes, C1 controls, bidi overrides
and zero-width characters) and capped at 500 characters, and a sheet keeps at
most 2000 lines. Artist, title and album strings from a provider are cleaned
the same way (200 characters). This happens when a response is parsed and again
whenever a sheet is read back from the disk cache.

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

Cache files are written atomically with mode `0600`. The cache is bounded: on
the first write and then every 32 writes, entries older than 30 days are
removed, and if the directory still holds more than 20 MB the oldest entries
are removed until it fits.

### Request limits

- A provider response larger than 2 MB (after gzip decompression) is refused.
- At most 50 search candidates are considered per provider.
- Each provider (LRCLIB, NetEase) may be asked 4 times in a burst and then once
  every 15 seconds, however fast a station changes its title. A throttled
  lookup is reported as a failure, not as "no lyrics", so it is not
  negatively cached.
- Concurrent lookups for the same track share one request round. When every
  provider failed for a track, that track is not retried for 30 seconds.

---

## 🖼️ The Album Art Engine (`pkg/art`)

### Cover providers

| Order | Provider | Notes |
|---|---|---|
| 1 | **iTunes Search API** | fastest, no key; the `100x100bb` artwork path is rewritten to `600x600bb` |
| 2 | **Deezer** | `album.cover_xl`, no key |
| 3 | **MusicBrainz → Cover Art Archive** | recording query resolves a release MBID, then `front-500` |
| 4 | **Last.fm** | only when `lastfm_api_key` is set |

Image URLs come from provider metadata, so only plain `http`/`https` URLs
without embedded credentials are fetched. Downloads are capped at 8 MB (a
larger body is refused, not truncated) and must present an image content type.
The image header is read with `image.DecodeConfig` before any pixel is decoded:
an image wider or taller than 2048 pixels, or larger than 4 megapixels in
total, is refused. The same check runs again before every full decode in the
renderer and when a cached image is read from disk (a failing cache entry is
deleted). Provider metadata responses are capped at 2 MB.

Artist, title, album and source strings from a provider are stripped of
terminal control sequences and capped at 200 characters; a stored origin URL
that is not a plain `http`/`https` URL is dropped. This also runs when a cover
is read back from the disk cache.

Each provider may be asked 4 times in a burst and then once every 15 seconds.
A lookup skipped by the throttle is not negatively cached. The disk cache is
written atomically with mode `0600` and bounded: entries older than 30 days are
removed and, beyond 200 MB, the oldest files go first.

### Protocol detection

`art.Detect` reads the environment and picks the best transport:

| Priority | Protocol | Detected from |
|---|---|---|
| 1 | Kitty graphics | `TERM` containing `kitty`, `KITTY_WINDOW_ID`, `TERM_PROGRAM=kitty` |
| 2 | iTerm2 inline images | `TERM_PROGRAM=iTerm.app`, `ITERM_SESSION_ID` |
| 3 | Sixel | `TERM` containing `foot`, `mlterm`, `yaft` or `sixel`, or `HALPRADIO_SIXEL` |
| 4 | Truecolor half-block | `COLORTERM` of `truecolor`/`24bit`, `TERM_PROGRAM=ghostty`, `TERM_PROGRAM=WezTerm`, or `TERM` containing `256color` |
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

- On an 80 column terminal or wider the drawer **takes its own columns** rather
  than overlapping the station list, so the list keeps its layout and
  selection.
- Below that the sheet takes over the content area as a full-width overlay,
  because the station list will not render under 28 columns beside an 18 column
  sidebar. `L` therefore always shows something at any width, and resizing
  moves the sheet between the two surfaces without closing it.
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

The status bar carries `[L] Lyrics` and `[A] Art` on every station tab, so the
feature is discoverable without opening the which-key overlay.

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

- Nothing is uploaded, but the track you are listening to is shared: both
  engines send the artist, title, album and duration already broadcast by the
  station, as query parameters, to the third parties below. Each of them can
  see your IP address and what you are listening to.

  | Third party | Receives | Used for | How to turn it off |
  |---|---|---|---|
  | LRCLIB (`lrclib.net`) | artist, title, album, duration | lyrics | `lyrics_enabled: false` |
  | NetEase Cloud Music (`music.163.com`) | artist and title | lyrics fallback | `lyrics_netease_enabled: false`, or `lyrics_enabled: false` |
  | Apple iTunes Search (`itunes.apple.com`) and its image CDN | artist and title | cover art | `album_art_itunes_enabled: false`, or `album_art_enabled: false` |
  | Deezer (`api.deezer.com`) and its image CDN | artist and title | cover art | `album_art_deezer_enabled: false`, or `album_art_enabled: false` |
  | MusicBrainz (`musicbrainz.org`), Cover Art Archive (`coverartarchive.org`, which redirects to `archive.org`) | artist and title, then a release ID | cover art | `album_art_musicbrainz_enabled: false`, or `album_art_enabled: false` |
  | Last.fm (`ws.audioscrobbler.com`) | artist, title and your API key | cover art | leave `lastfm_api_key` empty (the default) |

  Lyrics and cover art are looked up only when the station announces a new
  title, and the per-provider throttle above caps how often each third party
  is asked.
- Requests to LRCLIB and MusicBrainz identify themselves with a `halpradio/…`
  user agent, which both ask for. NetEase requests use a desktop browser user
  agent and a `music.163.com` referer, because the endpoint refuses others.
- Requests follow at most 3 redirects, never from `https` to `http`, and never
  from a public provider to a loopback, private, link-local or cloud metadata
  address; the same rule is enforced when the connection is dialled, so a
  provider's DNS answer cannot point there either. A configured
  `HTTP(S)_PROXY` is still used.
- With both features disabled in `config.yaml`, neither engine is constructed
  and no request is ever made.
