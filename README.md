# ytwatch

A local-first YouTube search TUI written in Go with Bubble Tea and Lip Gloss. It complements Newsraft: Newsraft is for following feeds and saving uploads; ytwatch is for searching a topic, inspecting results, and starting playback in mpv.

## Features

- Progressive YouTube search using `yt-dlp`; results appear before all metadata checks finish.
- Up to 55 unique results across a regular relevance search and a separate live-oriented search (results are streamed into the UI as they arrive).
- Result details: channel, duration, views, relative upload time, description, and thumbnail.
- Background metadata resolution helps distinguish current livestreams, upcoming streams, recorded livestreams, and Shorts.
- Live streams launch in `mpv` and open YouTube's pop-out chat in Firefox.
- Optional config file for mpv/chat sway workspaces, mpv arguments and the chat browser.
- Optional Ueberzug++ thumbnail overlay with an on-disk cache.
- Keyboard controls include Vim-style motions and numeric prefixes. `h/l` changes pane focus; `j/k` moves through results or scrolls the preview depending on focus.
- No YouTube API key or account credentials.

## Dependencies

Required:

- Go 1.23+
- `yt-dlp`
- `mpv`

Optional:

- `ueberzugpp` for image thumbnails (Wayland/Sway output)
- Firefox for live-chat pop-outs
- `wl-clipboard` for copying a selected video URL with `y`
- A browser logged in to YouTube (Firefox, Chromium, ...). It fixes the most common playback error, see [If a video won't play](#if-a-video-wont-play)

Keep `yt-dlp` up to date. YouTube changes often and old versions stop working.

## Install (step by step)

You do not need to know Go. You only need to run these commands in a terminal.

**1. Install the tools ytwatch needs.** On Arch Linux:

```bash
sudo pacman -S go yt-dlp mpv
```

On other systems, install `go`, `yt-dlp` and `mpv` with your package manager.

**2. Download the project.**

```bash
git clone https://github.com/geovannewashington/ytwatch.git
cd ytwatch
```

**3. Build it.** This creates a single file named `ytwatch`.

```bash
go build -o ytwatch ./cmd/ytwatch
```

**4. Put it in a folder that is in your `PATH`.** `~/.local/bin` is a common choice.

```bash
install -Dm755 ytwatch ~/.local/bin/ytwatch
```

**5. Run it.**

```bash
ytwatch
```

If you see `command not found`, add `~/.local/bin` to your `PATH` (for example in `~/.zshrc` or `~/.bashrc`):

```bash
export PATH="$HOME/.local/bin:$PATH"
```

Optional: run the tests with `go test ./...`. Go downloads the libraries ytwatch uses automatically on the first build.

Shortcut: steps 3 and 4 in one command (needs `make`):

```bash
make install
```

Another option: `go install ./cmd/ytwatch` builds and installs into `$(go env GOPATH)/bin` (usually `~/go/bin`), which must also be in your `PATH`.

## Usage

```bash
ytwatch
ytwatch "Go interfaces"
ytwatch "live coding Go"
ytwatch --help
```

Running without a query opens the interface with the search input active. Use `/` at any time to search again.

mpv keeps playing when you quit ytwatch.

## Keyboard

| Key | Action |
|---|---|
| `j` / `k`, arrows | Move one result |
| `5j`, `3k` | Move by a count |
| `gg` | First result |
| `G` | Last result |
| `H` / `M` / `L` | Top / middle / bottom of the visible list |
| `Ctrl+d` / `Ctrl+u` | Half-page down / up |
| `Ctrl+f` / `Ctrl+b` | Full-page down / up |
| `PageDown` / `PageUp` | Move one page |
| `/` | Search; Enter submits, Esc cancels |
| `r` | Repeat the current search |
| `Enter` | Play selected result in mpv |
| `y` | Copy selected URL with `wl-copy` |
| `?` | Keyboard help |
| `q` | Quit |

Regular videos have no type prefix. Shorts have an explicit `⚡ SHORT` badge, upcoming and active streams have their own badges, and archived livestreams are marked `📼 LIVE VOD` after the title.

## Metadata and livestream detection

The search phase runs a normal relevance query and a second query with `live` appended, each using `yt-dlp` flat search results to keep the interface responsive. Up to 35 regular and 20 live-oriented results are deduplicated into one list. Four bounded workers then resolve richer metadata in the background. You may see some badges update shortly after results arrive. `yt-dlp`'s `live_status` and `is_live` fields distinguish current streams from upcoming and previously-live broadcasts. Shorts are only marked when their URL/media type says Shorts or the metadata identifies a portrait video of three minutes or less; duration alone is not enough to reliably identify a Short.

A video that could not be fully resolved may remain without a status badge. It can still be selected for playback.

## Thumbnails

Thumbnail images are downloaded asynchronously and cached beneath `${XDG_CACHE_HOME:-~/.cache}/ytwatch/thumbnails`. Ueberzug++ is optional. When it is installed, ytwatch reserves a stable preview area and updates the overlay when the selection changes or the terminal is resized. Disable/uninstall Ueberzug++ to run the TUI without images.

## Configuration

Optional. Create `${XDG_CONFIG_HOME:-~/.config}/ytwatch/config.toml`:

```toml
[mpv]
workspace = "3"                  # sway workspace for mpv windows (omit to leave placement to sway)
args = ["--volume=60"]           # extra mpv arguments
cookies_from_browser = "firefox" # browser whose YouTube login yt-dlp reuses (see below)

[chat]
enabled = true                   # open YouTube's chat pop-out for live streams
workspace = "4"                  # sway workspace for the chat window
browser = "firefox"              # command used to open the chat
```

Workspace placement needs sway (`swaymsg`). ytwatch waits for the new window to appear and moves it, so the window may flash on the current workspace for a moment. Unknown keys or invalid values stop ytwatch at startup with an error naming the line.

## If a video won't play

ytwatch shows why mpv failed in the status bar: `mpv exited with code N | reason`.

- **"Sign in to confirm you're not a bot" or HTTP 429:** YouTube is rate-limiting anonymous requests. Set `cookies_from_browser = "firefox"` (or your browser) under `[mpv]` in the config. yt-dlp then reuses that browser's YouTube login. You must be logged in to YouTube there. It is off by default because it reads your browser's cookie store.
- **HTTP 403:** YouTube refused the video stream. Try again, or update yt-dlp.
- **Anything else:** update `yt-dlp` and `mpv`, then test the URL in a terminal with `mpv "<video url>"` to see the full error.

## Data locations

- Thumbnail cache: `${XDG_CACHE_HOME:-~/.cache}/ytwatch/thumbnails`
- Playback itself is delegated to mpv; ytwatch doesn't store subscriptions, favorites, or a watch-later queue. Keep those in Newsraft as intended.
