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

## Update

```sh
ytwatch update           # install the latest release
ytwatch update --check   # only say if there is a newer one
```

It asks GitHub for the latest release, downloads it over https, checks its
SHA-256 against `checksums.txt` of that release, and replaces the program in
one step, so nothing is left half written. It talks to GitHub only when you run
it: ytwatch never checks for updates by itself.

The checksum catches a broken download. It does not protect against someone
who controls the release itself.

It needs write access to the folder of the program. If you have a build from a
checkout, use `git pull && make install`. The install script above also
works for updating.

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

