# Configuration

The file is `~/.config/ytwatch/config.toml` (`$XDG_CONFIG_HOME` is respected).
Every option is below with its default, so you can copy it as it is. A mistake
stops ytwatch at start with an error that names the line.

```toml
[general]
browser = ""                  # command for o and chat; empty: $BROWSER, then the desktop default
audio_only = false
quality = "best"              # best, 1080p, 720p, 480p, 360p

[mpv]
workspace = ""                # move mpv windows here; empty: leave them where the WM puts them
args = []                     # extra mpv arguments, for example ["--volume=60"]
cookies_from_browser = ""     # for example "firefox"; read [Cookies](troubleshooting.md#cookies)
ui = "ytwatch"                # "ytwatch": controls like YouTube's; "mpv": keep mpv's own (or your uosc)
subtitles = ""                # languages in order of preference, as YouTube names them: "en,pt-BR"; empty: none (j cycles in mpv)
auto_subtitles = false        # also use YouTube's automatic captions for those languages

[chat]
enabled = true                # open the chat of live streams and replays
workspace = ""                # empty: the same as mpv
browser = ""                  # empty: general.browser

[download]
dir = "~/Downloads/ytwatch"
args = []                     # extra yt-dlp arguments, for example ["--embed-metadata"]

[theme]
name = "catppuccin"           # everforest, gruvbox, nord, tokyonight, rose-pine, dracula, terminal
# accent = "#a6e3a1"          # any color: accent, secondary, live, short, vod, muted, subtle, border, error

[preview]
output = "auto"               # auto, off, kitty, halfblocks, ueberzugpp, x11, wayland, sixel, iterm2, chafa
# auto: kitty in kitty and Ghostty, else ueberzugpp if installed, else halfblocks

[search]
language = "en"               # a code such as en, pt-BR or ja; empty: YouTube decides
region = "US"                 # a two-letter country code; empty: YouTube decides
hide = []                     # never show: shorts, playlists, live, upcoming, vods

[history]
enabled = true
max_age_days = 30             # also how long the place in a video is remembered
max_entries = 100
resume = true                 # remember where you stopped in each video (needs enabled = true)

[cache]                       # thumbnails
max_age_days = 7
max_size_mb = 100

[ui]
animations = "full"           # full, minimal (only progress) or off

# Open description links (3o) with a program. The command runs without a
# shell and %l is the link. Links without a match open in the browser.
# [link.images]
# match = '\.(png|jpe?g|gif|webp)(\?.*)?$'
# command = ["nsxiv", "%l"]
```

Workspace names follow your window manager: a name or number for sway, i3 and
niri, `3` or `name:video` for Hyprland, `^2` for bspwm, a number from 1 for
`wmctrl`.

While a video plays, its script, data and the small pictures of the seek bar are
in a private folder `ytwatch-mpv-*` of the temporary folder, removed when mpv
ends. Other files: search history in `~/.local/state/ytwatch/history`, the place where
you stopped in each video in `~/.local/state/ytwatch/watch_later` (mpv writes
them, and your own mpv files are left alone), thumbnails in
`~/.cache/ytwatch/thumbnails`.
