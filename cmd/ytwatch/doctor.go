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
	// tmuxPassthrough is tmux's allow-passthrough option: "on", "all", "off"
	// or "".
	tmuxPassthrough func() string
	// kittyDetected asks the terminal whether it speaks the kitty graphics
	// protocol, see preview.QueryKitty.
	kittyDetected func() bool
}

func tmuxOption(name string) func() string {
	return func() string {
		out, err := exec.Command("tmux", "show", "-gv", name).Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(out))
	}
}

func systemProbe(cfg config.Config) probe {
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
		windowManager:   func() string { return wm.Name(wm.Detect()) },
		clipboard:       clipboard.Tool,
		browser:         player.DescribeBrowser,
		tmuxFocusEvents: tmuxOption("focus-events"),
		tmuxPassthrough: tmuxOption("allow-passthrough"),
		kittyDetected: func() bool {
			return preview.WantsKittyQuery(cfg.Preview.Output, os.Getenv) &&
				preview.QueryKitty(os.Stdin, os.Stdout, os.Getenv("TMUX") != "")
		},
	}
}

// doctorResult is what the checks found.
type doctorResult struct {
	missing []string // names of the required programs that were not found
	wayland bool     // the session is Wayland, which decides the clipboard tool
}

// runDoctor prints what ytwatch finds on this system and returns the exit
// code: 1 when a required program is missing. It then says how to install
// what is missing.
func runDoctor(w io.Writer, cfg config.Config, p probe) int {
	result := diagnose(w, cfg, p)

	if len(result.missing) == 0 {
		fmt.Fprintln(w, "\nAll required programs found.")

		return 0
	}

	noun := "programs"
	if len(result.missing) == 1 {
		noun = "program"
	}

	fmt.Fprintf(w, "\n%d required %s missing.\n", len(result.missing), noun)

	plan := planInstall(result.missing, result.wayland, p.lookPath)
	if plan.command != "" {
		fmt.Fprintf(w, "\nInstall what is missing with:\n  %s\n", plan.command)
	}

	if len(plan.notes) > 0 {
		fmt.Fprintln(w)

		for _, note := range plan.notes {
			fmt.Fprintf(w, "  %s\n", note)
		}
	}

	return 1
}

// installCommand is the command that installs the missing programs, or "" when
// nothing is missing or no single command covers it. install.sh uses it.
func installCommand(cfg config.Config, p probe) string {
	result := diagnose(io.Discard, cfg, p)

	return planInstall(result.missing, result.wayland, p.lookPath).command
}

// diagnose checks the session and the required programs, and prints what it
// finds to w.
func diagnose(w io.Writer, cfg config.Config, p probe) doctorResult {
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

	_, err := p.lookPath("ueberzugpp")
	ueberzugpp := err == nil
	detected := p.kittyDetected != nil && p.kittyDetected()
	output := preview.ResolveOutput(cfg.Preview.Output, p.getenv, detected, ueberzugpp)
	inTmux := p.getenv("TMUX") != ""

	switch output.Mode {
	case preview.ModeOff:
		info(w, "thumbnails", "off in config.toml")
	case preview.ModeKitty:
		info(w, "thumbnails", "kitty graphics · "+output.Reason)
		if inTmux && p.tmuxPassthrough != nil {
			if mode := p.tmuxPassthrough(); mode != "on" && mode != "all" {
				info(w, "tmux", "allow-passthrough is off · thumbnails will not show · add: set -g allow-passthrough on")
			}
		}
	case preview.ModeHalfBlocks:
		info(w, "thumbnails", "half-blocks · "+output.Reason+" · ueberzugpp, kitty or Ghostty give sharper images")
	case preview.ModeUeberzug:
		if !ueberzugpp {
			info(w, "thumbnails", "ueberzugpp is not installed · no thumbnails")
			break
		}

		info(w, "thumbnails", "ueberzugpp, output: "+cmp.Or(output.Ueberzug, "chosen by ueberzugpp")+" · "+output.Reason)
		if inTmux && p.tmuxFocusEvents != nil && p.tmuxFocusEvents() != "on" {
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
	// two files that are not merged.
	var missing []string
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
			missing = append(missing, dep.name)

			continue
		}

		row(w, "ok", dep.name, p.version(dep.name, dep.flag))
	}

	if name := p.browser(cfg); name != "" {
		row(w, "ok", "browser", name+" (o opens links, live chat)")
	} else {
		row(w, "missing", "browser", "none found · install one, or set [general] browser in config.toml")
		missing = append(missing, "browser")
	}

	if tool, err := p.clipboard(); err == nil {
		row(w, "ok", "clipboard", tool+" (y copies a link)")
	} else {
		row(w, "missing", "clipboard", "install wl-clipboard (Wayland) or xclip / xsel (X11) for y")
		missing = append(missing, "clipboard")
	}

	// i3 has its own way to move windows; other X11 window managers use wmctrl.
	if server == "X11" && windows != "i3" {
		if _, err := p.lookPath("wmctrl"); err == nil {
			row(w, "ok", "wmctrl", "workspace placement on X11")
		} else {
			row(w, "missing", "wmctrl", "workspace placement on X11 · install wmctrl with your package manager")
			missing = append(missing, "wmctrl")
		}
	}

	fmt.Fprintln(w, "\nOptional")

	if ueberzugpp {
		row(w, "ok", "ueberzugpp", p.version("ueberzugpp", "--version"))
	} else {
		row(w, "none", "ueberzugpp", "sharp thumbnails outside kitty and Ghostty · Arch: pacman -S ueberzugpp · Homebrew: brew install jstkdng/programs/ueberzugpp · other: github.com/jstkdng/ueberzugpp")
	}

	return doctorResult{missing: missing, wayland: server == "Wayland"}
}

func row(w io.Writer, status, name, detail string) {
	fmt.Fprintf(w, "  %-8s %-15s %s\n", status, name, detail)
}

func info(w io.Writer, name, detail string) {
	fmt.Fprintf(w, "  %-17s %s\n", name, detail)
}
