package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/geovannewashington/ytwatch/internal/config"
	"github.com/geovannewashington/ytwatch/internal/preview"
	"github.com/geovannewashington/ytwatch/internal/tui"
)

const version = "0.1.0"

func main() {
	args := os.Args[1:]
	if len(args) == 1 && (args[0] == "-h" || args[0] == "--help" || args[0] == "help") {
		printHelp(version)
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

	// Pruning reads the whole folder, so it runs in the background. It is here
	// and not in SetConfig so tests do not touch the real cache.
	go preview.Prune(
		time.Duration(cfg.Cache.MaxAgeDays)*24*time.Hour,
		int64(cfg.Cache.MaxSizeMB)<<20,
		time.Now(),
	)

	model := tui.New(query)
	model.SetConfig(cfg)
	program := tea.NewProgram(model, tea.WithAltScreen(), tea.WithReportFocus())
	model.Attach(program.Send)

	if _, err := program.Run(); err != nil {
		model.Close()
		fmt.Fprintln(os.Stderr, "ytwatch:", err)
		os.Exit(1)
	}
	model.Close()
}
