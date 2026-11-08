# Install

ytwatch is for Linux, on Wayland or X11, on x86-64 or ARM64. Other systems are
untested.

## The install script

```sh
curl -fsSL https://github.com/geovannewashington/ytwatch/releases/latest/download/install.sh | sh
```

It downloads the latest release, checks its SHA-256 checksum, puts `ytwatch` in
`~/.local/bin`, and shows the command that installs the programs ytwatch needs.
It asks before running that command, and uses `sudo` only if you say yes. The
script is short, so you can [read it first](../install.sh). Options: `--yes` runs
the install command without asking, `--no-deps` installs only ytwatch, and
`VERSION=v0.1.0` picks a release.

**Update:** run the same command again. It installs the latest release and
checks its checksum.

## Other ways

- **With Go 1.23+:** `go install github.com/geovannewashington/ytwatch/cmd/ytwatch@latest`
  (installs to `~/go/bin`).
- **By hand:** download the `.tar.gz` for your system and `checksums.txt` from the
  [releases page](https://github.com/geovannewashington/ytwatch/releases), check
  it with `sha256sum -c --ignore-missing checksums.txt`, and move `ytwatch` to a
  folder in your `PATH`.
- **From source:** `git clone https://github.com/geovannewashington/ytwatch.git`,
  then `cd ytwatch && make install` (needs Go 1.23+; installs to `~/.local/bin`).

## Programs ytwatch needs

All of these are required. Run `ytwatch doctor` to check them: it also prints the
command that installs what is missing.

| Program | Used for |
|---|---|
| `yt-dlp` | searching and downloading. Keep it up to date, because YouTube changes often |
| `mpv` | playing |
| `ffmpeg` | merging video and audio in downloads. Without it you get two separate files |
| `ueberzugpp` (optional) | sharp thumbnails outside kitty and Ghostty. Without it they are half-blocks |
| a web browser | links and the live chat |
| `wl-clipboard` (Wayland) or `xclip` / `xsel` (X11) | copying links |
| `wmctrl` | moving windows to a workspace on X11 (not needed on i3) |

On Arch Linux:

```sh
sudo pacman -S yt-dlp mpv ffmpeg ueberzugpp wl-clipboard
```

On Debian and Ubuntu the packaged `yt-dlp` is too old for YouTube: use
`pipx install yt-dlp`.

