# Troubleshooting

- **Playback fails.** The status line shows why. Update `yt-dlp` and `mpv`, then
  see [Cookies](#cookies).
- **Boxes after channel names.** Verified and artist channels get a Nerd Font
  glyph. Use a [Nerd Font](https://www.nerdfonts.com) in your terminal to see it.
- **Thumbnails are blocky.** They are half-blocks, which every terminal can
  show. Install `ueberzugpp`, or use kitty or Ghostty. `ytwatch doctor` says
  which one is in use and why.
- **No thumbnails.** Run `ytwatch doctor`. With `ueberzugpp`, if your terminal
  runs under another display server than your session (an X11 terminal in
  Wayland), set `[preview] output` to `x11` or `wayland`.
- **kitty thumbnails do not show in tmux.** Add `set -g allow-passthrough on`
  to `tmux.conf`, and set `[preview] output = "kitty"`, because tmux hides the
  answer that `auto` looks for. `ytwatch doctor` checks the option.
- **With ueberzugpp, the thumbnail shows over other tmux windows or panes.**
  The image is a separate desktop window, so tmux cannot hide it. ytwatch hides
  it when the pane loses focus, but tmux only reports that with
  `set -g focus-events on` in `tmux.conf`. `ytwatch doctor` checks it.
- **With ueberzugpp, the thumbnail is a tiled window on Hyprland.** Add a window
  rule, see [hyprland.md](hyprland.md).
- **Results differ from youtube.com.** ytwatch is not logged in, and YouTube
  personalizes by account. Set `language` and `region` under `[search]`. The
  region is sent as a fake address, which YouTube may ignore.

## Cookies

YouTube increasingly treats anonymous `yt-dlp` requests as bots. Sooner or later
you see "Sign in to confirm you're not a bot", HTTP 429 or HTTP 403, and mpv or
a download fails. Setting `cookies_from_browser` to a browser where you are
logged in to YouTube (`"firefox"`, `"chromium"`, `"brave"` and so on) makes
`yt-dlp` look like a normal visitor, and these errors go away. It applies to
playing and downloading.

It is off by default because it reads that browser's cookies, and because a
logged-in view can feed that account's recommendations, which is what ytwatch
tries to avoid. Use a browser profile with a separate or throwaway account.
