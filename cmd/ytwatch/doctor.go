package main

import (
	"cmp"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/geovannewashington/ytwatch/internal/clipboard"
	"github.com/geovannewashington/ytwatch/internal/config"
	"github.com/geovannewashington/ytwatch/internal/player"
	"github.com/geovannewashington/ytwatch/internal/preview"
	"github.com/geovannewashington/ytwatch/internal/wm"
)

// probe is what the doctor asks of the system. Tests replace it.
type probe struct {
	getenv   func(string) string
	lookPath func(string) (string, error)
	// version returns the first line of "<program> <flag>", or "".
	version func(program, flag string) string
	// windowManager names the detected window manager backend, or "".
	windowManager func() string
	clipboard     func() (string, error)
	browser       func(config.Config) string
	// tmuxFocusEvents is tmux's focus-events option: "on", "off" or "".
	tmuxFocusEvents func() string
}

func systemProbe() probe {
	return probe{
		getenv:   os.Getenv,
		lookPath: exec.LookPath,
		version: func(program, flag string) string {
			out, err := exec.Command(program, flag).Output()
			if err != nil {
				return ""
			}
			line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
			return line
		},
		windowManager: func() string { return wm.Name(wm.Detect()) },
		clipboard:     clipboard.Tool,
		browser:       player.DescribeBrowser,
		tmuxFocusEvents: func() string {
			out, err := exec.Command("tmux", "show", "-gv", "focus-events").Output()
			if err != nil {
				return ""
			}
			return strings.TrimSpace(string(out))
		},
	}
}

// runDoctor prints what ytwatch finds on this system and returns the exit
// code: 1 when a required program is missing.
func runDoctor(w io.Writer, cfg config.Config, p probe) int {
	fmt.Fprintln(w, "ytwatch doctor")

	fmt.Fprintln(w, "\nSession")

	server := "none (a console or SSH session)"
	switch {
	case p.getenv("WAYLAND_DISPLAY") != "":
		server = "Wayland"
	case p.getenv("DISPLAY") != "":
		server = "X11"
	}
	info(w, "display server", server)

	windows := p.windowManager()
	switch {
	case windows != "":
		info(w, "window manager", windows+" · mpv and chat windows can be moved to a workspace")
	case server == "X11":
		info(w, "window manager", "none found · install wmctrl to place windows on X11 (optional)")
	default:
		info(w, "window manager", "none supported · windows are not moved (sway, i3, Hyprland, niri, bspwm and X11 work)")
	}

	output, enabled := preview.ResolveOutput(cfg.Preview.Output, p.getenv)
	if output == "" {
		output = "chosen by ueberzugpp"
	}

	switch _, err := p.lookPath("ueberzugpp"); {
	case !enabled:
		info(w, "thumbnails", "off in config.toml")
	case err != nil:
		info(w, "thumbnails", "ueberzugpp not installed · no thumbnails (see the Required list)")
	default:
		info(w, "thumbnails", "ueberzugpp, output: "+output)
		if p.getenv("TMUX") != "" && p.tmuxFocusEvents != nil && p.tmuxFocusEvents() != "on" {
			info(w, "tmux", "focus-events is off · thumbnails can stay on screen when you change window · add: set -g focus-events on")
		}
	}

	if cfg.Search.Language == "" && cfg.Search.Region == "" {
		info(w, "search", "language and region chosen by YouTube (set [search] in config.toml)")
	} else {
		info(
			w,
			"search",
			"language "+cmp.Or(cfg.Search.Language, "default")+", region "+cmp.Or(cfg.Search.Region, "default"),
		)
	}

	// Everything here is required: without ffmpeg a video download is saved as
	// two files that are not merged, and without ueberzugpp there are no
	// thumbnails.
	missing := 0
	fmt.Fprintln(w, "\nRequired")

	for _, dep := range []struct{ name, flag, hint string }{
		{
			"yt-dlp", "--version",
			"pacman -S yt-dlp · dnf install yt-dlp · brew install yt-dlp · pipx install yt-dlp (apt's version is often too old for YouTube)",
		},
		{"mpv", "--version", "install mpv with your package manager"},
		{
			"ffmpeg", "-version",
			"install ffmpeg with your package manager · without it a video download is saved as two files, video and audio, that are not merged",
		},
	} {
		if _, err := p.lookPath(dep.name); err != nil {
			row(w, "missing", dep.name, dep.hint)
			missing++

			continue
		}

		row(w, "ok", dep.name, p.version(dep.name, dep.flag))
	}

	if _, err := p.lookPath("ueberzugpp"); err == nil {
		row(w, "ok", "ueberzugpp", p.version("ueberzugpp", "--version"))
	} else if !enabled {
		row(w, "ok", "ueberzugpp", "not needed: thumbnails are off in config.toml")
	} else {
		row(
			w,
			"missing",
			"ueberzugpp",
			"thumbnails · Arch: pacman -S ueberzugpp · Homebrew: brew install jstkdng/programs/ueberzugpp · other: github.com/jstkdng/ueberzugpp",
		)
		missing++
	}

	if name := p.browser(cfg); name != "" {
		row(w, "ok", "browser", name+" (o opens links, live chat)")
	} else {
		row(w, "missing", "browser", "none found · install one, or set [general] browser in config.toml")
		missing++
	}

	if tool, err := p.clipboard(); err == nil {
		row(w, "ok", "clipboard", tool+" (y copies a link)")
	} else {
		row(w, "missing", "clipboard", "install wl-clipboard (Wayland) or xclip / xsel (X11) for y")
		missing++
	}

	// i3 has its own way to move windows; other X11 window managers use wmctrl.
	if server == "X11" && windows != "i3" {
		if _, err := p.lookPath("wmctrl"); err == nil {
			row(w, "ok", "wmctrl", "workspace placement on X11")
		} else {
			row(w, "missing", "wmctrl", "workspace placement on X11 · install wmctrl with your package manager")
			missing++
		}
	}

	if missing > 0 {
		noun := "programs"
		if missing == 1 {
			noun = "program"
		}

		fmt.Fprintf(w, "\n%d required %s missing.\n", missing, noun)

		return 1
	}

	fmt.Fprintln(w, "\nAll required programs found.")

	return 0
}

func row(w io.Writer, status, name, detail string) {
	fmt.Fprintf(w, "  %-8s %-15s %s\n", status, name, detail)
}

func info(w io.Writer, name, detail string) {
	fmt.Fprintf(w, "  %-17s %s\n", name, detail)
}
