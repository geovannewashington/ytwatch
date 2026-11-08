<div align="center">

<img src="docs/assets/wordmark.svg" width="380" alt="ytwatch">

Search and watch YouTube from your terminal.<br>
No feed, no recommendations, no autoplay.

[![CI](https://github.com/geovannewashington/ytwatch/actions/workflows/ci.yml/badge.svg)](https://github.com/geovannewashington/ytwatch/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/geovannewashington/ytwatch)](https://github.com/geovannewashington/ytwatch/releases/latest)
[![Go](https://img.shields.io/badge/go-1.23+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![License: MIT](https://img.shields.io/badge/license-MIT-green)](LICENSE)
[![Platform](https://img.shields.io/badge/platform-linux-lightgrey)](#install)

https://github.com/user-attachments/assets/f9ed9f1b-6697-43c1-9a8e-53046f06a808

</div>

## Why

Even logged out, youtube.com gives you a home feed, suggestions, autoplay and
Shorts. ytwatch has none of that: you search for what you want, watch it, and
leave. No account, no comments, no likes. Want the comments? `o` opens the
video in your browser.

It is also light. [yt-dlp](https://github.com/yt-dlp/yt-dlp) searches,
[mpv](https://mpv.io) plays, and no browser window stays open while you watch.
A browser is still needed for the chat, for links, and for the cookies that
YouTube soon asks for (see [Cookies](docs/troubleshooting.md#cookies)). On a
tiling window manager the video opens on a workspace you choose, away from
your work.

ytwatch does not manage subscriptions: an RSS reader such as
[Newsraft](https://codeberg.org/newsraft/newsraft) does that well. ytwatch is
for finding new, ordinary content on purpose.

## Features

- **One search for everything:** videos, Shorts, live streams and playlists,
  with filters. Paste a YouTube link to open it directly.
- **Thumbnails** in any terminal: sharp in kitty and Ghostty, half-blocks
  elsewhere, real images with the optional `ueberzugpp`.
- **Live chat**, also the chat replay of past streams, with pictures, badges,
  emoji and Super Chats.
- **Player controls like YouTube's:** red progress bar, "most replayed" graph,
  hover thumbnails, speed, volume and subtitles (`[mpv] subtitles = "en"`).
- **Pick up where you left off:** a thin red bar in the list, and the video
  starts from there. `s` plays it from the start.
- **Links in the description:** `3o` opens link 3, `1y` copies link 1. The idea
  comes from [Newsraft](https://codeberg.org/newsraft/newsraft).
- **Queue and download:** select several results with `V`, then play or
  download them, as video or audio only.
- **Made for Vim users, and rice-friendly:** `hjkl`, counts like `5j`, 8 color
  themes (catppuccin by default) and a `terminal` theme that follows pywal.

## Install

```sh
curl -fsSL https://github.com/geovannewashington/ytwatch/releases/latest/download/install.sh | sh
```

It checks the SHA-256 checksum of the download and asks before it installs
anything else. To update later, run `ytwatch update` (`--check` only looks).
Other ways (Go, by hand, from source)
and the other programs are in [docs/install.md](docs/install.md).

ytwatch needs `yt-dlp`, `mpv`, `ffmpeg` and a browser. Run `ytwatch doctor` to
check them: it prints the command that installs what is missing.

## Usage

```sh
ytwatch                  # opens with the search line active
ytwatch "Go interfaces"  # opens with a search
```

| Key | Action |
|---|---|
| `/` | Search (`r` repeats it, `f` sets filters) |
| `j` `k` `h` `l` | Move / switch between results and preview |
| `Enter` `s` | Play / play from the start |
| `V` | Select several results, then `Enter` plays and `d` downloads them |
| `d` `a` `Q` | Download / audio only / quality |
| `o` `y` | Open / copy the link (`3o`: description link 3) |
| `?` `q` | Help with all keys / quit |

mpv keeps playing if you quit ytwatch.

## Privacy

No account, no telemetry. ytwatch talks only to YouTube's servers:
`youtube.com`, `i.ytimg.com` and `googlevideo.com` (directly and through
`yt-dlp` and `mpv`), and `ggpht.com` and `fonts.gstatic.com` for the pictures
of the chat page. The one other place is GitHub, and only when you run
`ytwatch update`: it never checks by itself.

It keeps your search history and where you stopped in each video for 30 days,
and thumbnails for 7. Both can be turned off. See
[Cookies](docs/troubleshooting.md#cookies) for the one setting that reads
browser data.

Found a security problem? Please report it privately: the **Security** tab of
the repository, then **Report a vulnerability**.

## More

- [Configuration](docs/configuration.md): every option with its default.
- [Install](docs/install.md): other ways to install, and the programs needed.
- [Troubleshooting](docs/troubleshooting.md): playback errors, cookies,
  thumbnails, tmux and Hyprland.

## License

[MIT](LICENSE). Contributions are welcome: open an issue or a pull request.
