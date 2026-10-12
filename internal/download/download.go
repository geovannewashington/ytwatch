// Package download saves videos or their audio with yt-dlp.
package download

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/geovannewashington/ytwatch/internal/config"
	"github.com/geovannewashington/ytwatch/internal/media"
)

// fileTemplate keeps names short and unique: the title is cut to 150 bytes and
// the video id avoids clashes between videos with the same title.
const fileTemplate = "%(title).150B [%(id)s].%(ext)s"

// audioFormat prefers m4a, which most players handle, and needs no ffmpeg.
const audioFormat = "bestaudio[ext=m4a]/bestaudio"

// ExpandDir turns a leading ~ into the home directory.
func ExpandDir(dir string) (string, error) {
	if dir != "~" && !strings.HasPrefix(dir, "~/") {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, strings.TrimPrefix(dir, "~")), nil
}

// buildArgs returns the yt-dlp arguments. User arguments come last so they
// can override the defaults, for example -f.
func buildArgs(url, dir string, cfg config.Config, audioOnly bool) []string {
	args := []string{
		"--no-playlist", "--no-warnings", "--quiet",
		// --print makes yt-dlp skip the download unless told otherwise.
		"--no-simulate", "--print", "after_move:filepath",
		"-P", dir, "-o", fileTemplate,
	}
	if audioOnly {
		args = append(args, "-f", audioFormat)
	}
	if cfg.MPV.CookiesFromBrowser != "" {
		args = append(args, "--cookies-from-browser", cfg.MPV.CookiesFromBrowser)
	}
	args = append(args, cfg.Download.Args...)
	return append(args, "--", url)
}

// Run downloads v and returns the path of the saved file. It blocks until
// yt-dlp ends, and stops it when ctx is cancelled.
func Run(ctx context.Context, v media.Video, cfg config.Config, audioOnly bool) (string, error) {
	if v.IsCurrentlyLive() || v.IsUpcoming() {
		return "", errors.New("live and upcoming streams cannot be downloaded")
	}
	url := v.CanonicalURL()
	if url == "" {
		return "", errors.New("this result has no downloadable URL")
	}
	dir, err := ExpandDir(cfg.Download.Dir)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create download folder: %w", err)
	}
	cmd := exec.CommandContext(ctx, "yt-dlp", buildArgs(url, dir, cfg, audioOnly)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", errors.New(failureReason(stderr.String(), err))
	}
	return lastLine(stdout.String()), nil
}

// failureReason picks the last "ERROR:" line of yt-dlp's output.
func failureReason(stderr string, err error) string {
	lines := strings.Split(stderr, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(lines[i]), "ERROR:"); ok {
			reason := strings.TrimSpace(rest)
			if strings.Contains(reason, "Sign in to confirm") {
				return "YouTube blocked yt-dlp; set cookies_from_browser under [mpv] in config.toml"
			}
			return reason
		}
	}
	return err.Error()
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}
