<div align="center">

<pre>
  ╲ ╱  
╭─────╮
│ • • │
╰──◡──╯
</pre>

# ytwatch

**Search YouTube from the terminal and play it in mpv.**

[![Go](https://img.shields.io/badge/go-1.23+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![License: MIT](https://img.shields.io/badge/license-MIT-green)](LICENSE)
[![Platform](https://img.shields.io/badge/platform-linux-lightgrey)](#install)

![ytwatch](docs/screenshot.png)

</div>

ytwatch is a small terminal interface for finding videos. It searches with
[yt-dlp](https://github.com/yt-dlp/yt-dlp), shows thumbnails and descriptions
next to the results, and plays with [mpv](https://mpv.io). No account and no
API key.

## Features

- Results stream in as they are found, with channel, length, views, age,
  description (with its line breaks) and thumbnail.
- Videos, Shorts, live streams and playlists in one search, with badges.
- Play a whole playlist, or open it with `B` to browse its videos.
- Search filters like youtube.com: type, duration, upload date.
- Set the search language and region.
- Select several results with `V` and play them as a queue or download them.
- Pick the video quality, or play audio only.
- Search history on Up and Down, deleted automatically after 30 days.
- Open the video or a numbered description link in your browser, or in a
  program you choose (for example images in `nsxiv`).
- Live streams open their chat in your browser.
- Moves the mpv window to a workspace you choose on sway, i3, Hyprland, niri,
  bspwm and X11 window managers.
- Color themes, and every color can be changed.

## Install

You need `yt-dlp`, `mpv` and Go 1.23 or newer. On Arch Linux:

```sh
sudo pacman -S go yt-dlp mpv
```

Then build and install:

```sh
git clone https://github.com/geovannewashington/ytwatch.git
cd ytwatch
make install    # builds and copies ytwatch to ~/.local/bin
```

Make sure `~/.local/bin` is in your `PATH`. Without `make`, run
`go build -o ytwatch ./cmd/ytwatch` and copy the file where you like.

Keep `yt-dlp` up to date, because YouTube changes often. On Debian and Ubuntu
the packaged version is usually too old: use `pipx install yt-dlp` instead.

Optional programs:

| Program | For |
|---|---|
| `ueberzugpp` | Thumbnails. Works on Wayland and X11, and in terminals with a graphics protocol |
| `ffmpeg` | Joining video and audio in downloads |
| `wl-clipboard`, `xclip` or `xsel` | Copying links with `y` |
| `wmctrl` | Moving windows to a workspace on X11 |

ytwatch is made for Linux. Run `ytwatch doctor` to see what is installed and
what your session supports.

## Usage

```sh
ytwatch                 # opens with the search line active
ytwatch "Go interfaces"
ytwatch doctor
```

mpv keeps playing when you quit ytwatch. Press `?` inside for the full list
of keys.

| Key | Action |
|---|---|
| `j` `k`, `h` `l` | Move through results, switch between results and preview |
| `Enter` | Play in mpv. On a playlist, play all of it. |
| `B` | Browse a playlist: open it to pick single videos |
| `Esc` | Go back from a playlist, or quit from the main page |
| `/` | Edit the search. `Up` and `Down` walk through past searches |
| `r` | Repeat the search |
| `f` | Filters: type, duration, upload date |
| `V` | Select lines, then `Enter` plays them as a queue and `d` downloads them |
| `d` | Download. Files go to `~/Downloads/ytwatch` |
| `a` | Audio only |
| `Q` | Video quality: best, 1080p, 720p, 480p, 360p |
| `o` `y` | Open or copy the video link. With a number, such as `3o`, the 3rd description link |
| `t` | Next color theme |
| `gg` `G` `5j` `Ctrl+d` | Vim-style movement and counts |
| `q` | Quit |

## Configuration

Everything is optional. The file is `~/.config/ytwatch/config.toml`
(`$XDG_CONFIG_HOME` is respected). A mistake stops ytwatch at start with an
error that names the line.

```toml
[general]
browser = "firefox"          # for o and live chat; default: your desktop's browser
audio_only = false
quality = "best"             # best, 1080p, 720p, 480p, 360p

[mpv]
workspace = "3"              # where mpv windows go; the syntax is your WM's own
args = ["--volume=60"]
cookies_from_browser = "firefox"   # see Troubleshooting

[chat]
enabled = true
# workspace = "4"            # default: the same as mpv, so chat opens next to the video

[download]
dir = "~/Videos/youtube"
args = ["--embed-metadata"]  # extra yt-dlp arguments

[theme]
name = "everforest"          # catppuccin, gruvbox, nord, tokyonight,
                             # rose-pine, dracula, terminal
accent = "#88c0d0"           # any role can be overridden

[preview]
output = "auto"              # auto, off, x11, wayland, sixel, kitty, iterm2, chafa

[search]
language = "pt-BR"           # code like pt, ja or pt-BR; empty: YouTube decides
region = "BR"                # two-letter country code

[history]
enabled = true
max_age_days = 30
max_entries = 100

[ui]
animations = "full"          # full, minimal (progress only) or off

[cache]                      # thumbnails, cleaned each time ytwatch starts
max_age_days = 7
max_size_mb = 100

# Open description links with a program. The command runs without a shell,
# and %l is the link. Links without a match open in the browser.
[link.images]
match = '\.(png|jpe?g|gif|webp)(\?.*)?$'
command = ["nsxiv", "%l"]
```

Workspace values: sway, i3 and niri take a name or number; Hyprland takes `3`
or `name:video`; bspwm a desktop name or `^2`; `wmctrl` a number from 1.

## Troubleshooting

ytwatch shows why mpv failed in the status line. Run `ytwatch doctor` first.

- **"Sign in to confirm you're not a bot", or HTTP 429.** YouTube is limiting
  anonymous requests. Set `cookies_from_browser` under `[mpv]` to a browser
  where you are logged in to YouTube. It is off by default because it reads
  that browser's cookies.
- **HTTP 403.** YouTube refused the stream. Try again, or update `yt-dlp`.
- **Results differ from what I see on youtube.com.** YouTube personalizes
  results for logged-in accounts and picks the language and country from
  your connection. ytwatch is not logged in. Set `language` and `region`
  under `[search]` to get closer. The region is sent as a fake address from
  that country, which YouTube may ignore.
- **No thumbnails.** Install `ueberzugpp`. If your terminal runs under another
  display server than your session (for example an X11 terminal inside
  Wayland), set `[preview] output` to `x11` or `wayland`.
- **Thumbnail opens as its own tiled window on Hyprland.** ueberzugpp draws
  the `wayland` thumbnail in a window of its own and can only place it over the
  terminal when that window floats. Add a window rule for it. With a Lua
  config (as Omarchy ships it), in `hyprland.lua`:

  ```lua
  hl.window_rule({
    match = { class = "^(ueberzugpp_.*)$" },
    float = true, no_anim = true, no_shadow = true, no_focus = true,
  })
  ```

  With a Lua config, ueberzugpp 2.9.10 and older still open the thumbnail
  centered: they move the window with a legacy `hyprctl dispatch`, which Lua
  configs reject. Newer builds (`ueberzugpp-git` on the AUR) support the Lua
  config.

  With a `hyprland.conf`:

  ```ini
  windowrule = float, class:^(ueberzugpp_.*)$
  windowrule = noanim, class:^(ueberzugpp_.*)$
  windowrule = noshadow, class:^(ueberzugpp_.*)$
  windowrule = nofocus, class:^(ueberzugpp_.*)$
  ```
- **Anything else.** Update `yt-dlp` and `mpv`, then try
  `mpv "<video url>"` to see the full error.

## Files

| What | Where |
|---|---|
| Config | `~/.config/ytwatch/config.toml` |
| Thumbnail cache | `~/.cache/ytwatch/thumbnails` (deleted after 7 days, 100 MB at most) |
| Search history | `~/.local/state/ytwatch/history` |

ytwatch stores no subscriptions or watch list. For feeds, a reader such as
[Newsraft](https://codeberg.org/newsraft/newsraft) pairs well with it.

## Development

```sh
make test    # go test ./...
make vet
make fmt
```

## License

[MIT](LICENSE)
