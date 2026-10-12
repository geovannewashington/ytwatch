package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/geovannewashington/ytwatch/internal/config"
	"github.com/geovannewashington/ytwatch/internal/tui"
)

const version = "0.2.0"

const usage = `ytwatch - YouTube search TUI

Usage:
  ytwatch [search query]
  ytwatch doctor          check the programs and session ytwatch uses

Examples:
  ytwatch
  ytwatch Go interfaces
  ytwatch live coding Go

Dependencies:
  yt-dlp, mpv
  Optional: ueberzugpp (thumbnails), a web browser (links, live chat),
    wl-clipboard (Wayland) or xclip / xsel (X11) for copying,
    wmctrl (workspace placement on X11), ffmpeg (merging downloads)

Config: ~/.config/ytwatch/config.toml (theme, workspaces, mpv args,
  download folder, link handlers, thumbnail output)

Inside ytwatch, press ? for keyboard help.`

func main() {
	args := os.Args[1:]
	if len(args) == 1 && (args[0] == "-h" || args[0] == "--help" || args[0] == "help") {
		fmt.Println(usage)
		return
	}
	if len(args) == 1 && (args[0] == "-v" || args[0] == "--version") {
		fmt.Printf("ytwatch %s\n", version)
		return
	}
	if len(args) == 1 && (args[0] == "doctor" || args[0] == "--doctor") {
		cfg, err := config.Load()
		if err != nil {
			fmt.Fprintln(os.Stderr, "ytwatch: config error:", err)
			cfg = config.Default()
		}
		os.Exit(runDoctor(os.Stdout, cfg, systemProbe()))
	}
	if len(args) > 0 && args[0] == "-s" {
		args = args[1:]
	}
	query := strings.TrimSpace(strings.Join(args, " "))
	if _, err := exec.LookPath("yt-dlp"); err != nil {
		fmt.Fprintln(os.Stderr, "ytwatch: yt-dlp is required. Install it with your package manager (pacman -S yt-dlp, dnf install yt-dlp, brew install yt-dlp) or pipx install yt-dlp.")
		fmt.Fprintln(os.Stderr, "On Debian and Ubuntu the packaged yt-dlp is often too old for YouTube, so prefer pipx or the release binary.")
		fmt.Fprintln(os.Stderr, "Run 'ytwatch doctor' to check everything ytwatch needs.")
		os.Exit(1)
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "ytwatch: config error:", err)
		os.Exit(1)
	}

	model := tui.New(query)
	model.SetConfig(cfg)
	program := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := program.Run(); err != nil {
		model.Close()
		fmt.Fprintln(os.Stderr, "ytwatch:", err)
		os.Exit(1)
	}
	model.Close()
}
