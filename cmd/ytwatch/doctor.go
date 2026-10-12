package main

import (
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
		info(w, "window manager", windows+" | mpv and chat windows can be moved to a workspace")
	case server == "X11":
		info(w, "window manager", "none found | install wmctrl to place windows on X11 (optional)")
	default:
		info(w, "window manager", "none supported | windows are not moved (sway, i3, Hyprland, niri, bspwm and X11 work)")
	}
	output, enabled := preview.ResolveOutput(cfg.Preview.Output, p.getenv)
	if output == "" {
		output = "chosen by ueberzugpp"
	}
	switch _, err := p.lookPath("ueberzugpp"); {
	case !enabled:
		info(w, "thumbnails", "off in config.toml")
	case err != nil:
		info(w, "thumbnails", "ueberzugpp not installed | no thumbnails (see the Optional list)")
	default:
		info(w, "thumbnails", "ueberzugpp, output: "+output)
	}

	missing := 0
	fmt.Fprintln(w, "\nRequired")
	for _, dep := range []struct{ name, flag, hint string }{
		{"yt-dlp", "--version", "pacman -S yt-dlp | dnf install yt-dlp | brew install yt-dlp | pipx install yt-dlp (apt's version is often too old for YouTube)"},
		{"mpv", "--version", "install mpv with your package manager"},
	} {
		if _, err := p.lookPath(dep.name); err != nil {
			row(w, "MISSING", dep.name, dep.hint)
			missing++
			continue
		}
		row(w, "ok", dep.name, p.version(dep.name, dep.flag))
	}

	fmt.Fprintln(w, "\nOptional")
	if tool, err := p.clipboard(); err == nil {
		row(w, "ok", "clipboard", tool+" (y copies a URL)")
	} else {
		row(w, "missing", "clipboard", "install wl-clipboard (Wayland) or xclip / xsel (X11) for y")
	}
	if name := p.browser(cfg); name != "" {
		row(w, "ok", "browser", name+" (o opens links, live chat)")
	} else {
		row(w, "missing", "browser", "none found | set [general] browser in config.toml")
	}
	if _, err := p.lookPath("ueberzugpp"); err == nil {
		row(w, "ok", "ueberzugpp", p.version("ueberzugpp", "--version"))
	} else {
		row(w, "missing", "ueberzugpp", "thumbnails | Arch: pacman -S ueberzugpp | Homebrew: brew install jstkdng/programs/ueberzugpp | other: github.com/jstkdng/ueberzugpp")
	}
	if _, err := p.lookPath("ffmpeg"); err == nil {
		row(w, "ok", "ffmpeg", "downloads can merge video and audio")
	} else {
		row(w, "missing", "ffmpeg", "needed to merge video and audio in downloads (audio-only downloads work without it)")
	}
	if server == "X11" || windows == "" && server != "Wayland" {
		if _, err := p.lookPath("wmctrl"); err == nil {
			row(w, "ok", "wmctrl", "workspace placement on X11")
		} else {
			row(w, "missing", "wmctrl", "workspace placement on X11")
		}
	}

	if missing > 0 {
		fmt.Fprintf(w, "\n%d required program(s) missing.\n", missing)
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
