package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime/debug"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/geovannewashington/ytwatch/internal/config"
	"github.com/geovannewashington/ytwatch/internal/preview"
	"github.com/geovannewashington/ytwatch/internal/resume"
	"github.com/geovannewashington/ytwatch/internal/tui"
)

// version is set when a release is built (-ldflags "-X main.version=..."). It
// stays empty for "go install", which records the module version instead; see
// versionString.
var version string

// versionString is the version to show, without a leading "v": the release
// version, else the module version of "go install pkg@v0.1.0", else "dev" for
// a build from a checkout.
func versionString() string {
	if version != "" {
		return version
	}

	if info, ok := debug.ReadBuildInfo(); ok {
		if v := info.Main.Version; v != "" && v != "(devel)" {
			return strings.TrimPrefix(v, "v")
		}
	}

	return "dev"
}

func main() {
	args := os.Args[1:]
	if len(args) == 1 && (args[0] == "-h" || args[0] == "--help" || args[0] == "help") {
		printHelp(versionString())
		return
	}

	if len(args) == 1 && (args[0] == "-v" || args[0] == "--version") {
		fmt.Printf("ytwatch %s\n", versionString())
		return
	}

	installOnly := len(args) == 2 && args[1] == "--install-command"
	if (len(args) == 1 || installOnly) && (args[0] == "doctor" || args[0] == "--doctor") {
		cfg, err := config.Load()
		if err != nil {
			fmt.Fprintln(os.Stderr, "ytwatch: config error:", err)
			cfg = config.Default()
		}

		// install.sh asks for just the command that installs what is missing.
		if installOnly {
			if command := installCommand(cfg, systemProbe(cfg)); command != "" {
				fmt.Println(command)
			}

			return
		}

		os.Exit(runDoctor(os.Stdout, cfg, systemProbe(cfg)))
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

	// The terminal is asked before Bubble Tea starts reading its input.
	detected := preview.WantsKittyQuery(cfg.Preview.Output, os.Getenv) &&
		preview.QueryKitty(os.Stdin, os.Stdout, os.Getenv("TMUX") != "")

	// The program and the kitty images share one locked writer, so an image
	// never lands in the middle of a frame.
	terminal := preview.NewSyncWriter(os.Stdout)

	model := tui.New(query)
	model.SetKitty(preview.NewKitty(terminal, os.Getenv("TMUX") != ""), detected)
	model.SetConfig(cfg)

	// mpv remembers where each video was left, in a folder of ytwatch, and the
	// old positions are deleted when they are older than the history is kept.
	if cfg.History.Resumes() {
		if dir := resume.Dir(); resume.Prepare(dir) == nil {
			resume.Prune(dir, time.Duration(cfg.History.MaxAgeDays)*24*time.Hour, time.Now())
			model.SetResume(dir)
		}
	}
	program := tea.NewProgram(model, tea.WithOutput(terminal), tea.WithAltScreen(), tea.WithReportFocus())
	model.Attach(program.Send)

	if _, err := program.Run(); err != nil {
		model.Close()
		fmt.Fprintln(os.Stderr, "ytwatch:", err)
		os.Exit(1)
	}
	model.Close()
}
