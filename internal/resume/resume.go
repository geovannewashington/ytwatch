// Package resume keeps where the user stopped in each video.
//
// mpv does the work: with --save-position-on-quit it writes the position to a
// "watch later" file when it quits, deletes the file when the video ends, and
// starts from it the next time the same address is played. ytwatch only gives
// mpv a folder of its own for these files, shows what is in it, and deletes the
// old ones. The folder is private to ytwatch, so the watch later files of the
// user's own mpv are left alone.
package resume

import (
	"bufio"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	// maxFileSize bounds what is read from a file: they are a few lines.
	maxFileSize = 4 << 10

	// maxFiles bounds how many files are read or kept.
	maxFiles = 2000

	// Finished is the part of a video after which it counts as watched, like
	// on YouTube: a video seen to its credits is not resumed.
	Finished = 0.95

	// MinPosition is how far into a video the user must be for it to count.
	MinPosition = 10 * time.Second
)

var videoID = regexp.MustCompile(`^[A-Za-z0-9_-]{6,20}$`)

// Dir returns the folder of the watch later files: $XDG_STATE_HOME/ytwatch/
// watch_later, or ~/.local/state/ytwatch/watch_later. It is empty when the home
// of the user is not known.
func Dir() string {
	dir := os.Getenv("XDG_STATE_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, ".local", "state")
	}

	return filepath.Join(dir, "ytwatch", "watch_later")
}

// Prepare makes the folder, which only the user can open: it holds what the
// user watched.
func Prepare(dir string) error {
	if dir == "" {
		return os.ErrInvalid
	}

	return os.MkdirAll(dir, 0o700)
}

// Load reads the positions in dir, by video id. A file that is not a watch
// later file of a YouTube video is skipped, and so is one that cannot be read.
func Load(dir string) map[string]time.Duration {
	positions := map[string]time.Duration{}
	if dir == "" {
		return positions
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return positions
	}

	for i, entry := range entries {
		if i >= maxFiles {
			break
		}
		if !entry.Type().IsRegular() {
			continue
		}

		if id, position, ok := readFile(filepath.Join(dir, entry.Name())); ok {
			positions[id] = position
		}
	}

	return positions
}

// readFile reads one watch later file: a line "# <address>" that
// --write-filename-in-watch-later-config puts first, and "start=<seconds>".
func readFile(path string) (id string, position time.Duration, ok bool) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, false
	}
	defer file.Close()

	var address string
	var seconds float64
	var haveStart bool

	scanner := bufio.NewScanner(&limited{file: file, left: maxFileSize})
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		switch {
		case strings.HasPrefix(line, "# "):
			if address == "" {
				address = strings.TrimPrefix(line, "# ")
			}
		case strings.HasPrefix(line, "start="):
			value, err := strconv.ParseFloat(strings.TrimPrefix(line, "start="), 64)
			if err == nil && value > 0 && value < 1e7 {
				seconds, haveStart = value, true
			}
		}
	}

	id, found := VideoID(address)
	if !found || !haveStart {
		return "", 0, false
	}

	return id, time.Duration(seconds * float64(time.Second)), true
}

type limited struct {
	file *os.File
	left int
}

func (l *limited) Read(p []byte) (int, error) {
	if l.left <= 0 {
		return 0, os.ErrClosed
	}
	if len(p) > l.left {
		p = p[:l.left]
	}
	n, err := l.file.Read(p)
	l.left -= n

	return n, err
}

// VideoID returns the id of the video at a YouTube watch address.
func VideoID(address string) (string, bool) {
	u, err := url.Parse(address)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return "", false
	}

	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	if host != "youtube.com" || u.Path != "/watch" {
		return "", false
	}

	id := u.Query().Get("v")
	if !videoID.MatchString(id) {
		return "", false
	}

	return id, true
}

// Fraction is how much of a video of length duration is behind position, from 0
// to 1, or 0 when the length is not known.
func Fraction(position, duration time.Duration) float64 {
	if duration <= 0 || position <= 0 {
		return 0
	}

	return min(1, float64(position)/float64(duration))
}

// Resumable tells whether a video that was left at position should start there:
// the user was past MinPosition, and not so near the end that it counts as
// watched. A video of unknown length is resumed.
func Resumable(position, duration time.Duration) bool {
	if position < MinPosition {
		return false
	}

	return duration <= 0 || Fraction(position, duration) < Finished
}

// Prune deletes the files of dir that are older than maxAge, and the oldest
// ones when there are more than maxFiles. Only regular files are touched.
func Prune(dir string, maxAge time.Duration, now time.Time) {
	if dir == "" || maxAge <= 0 {
		return
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	type file struct {
		path string
		time time.Time
	}
	var kept []file

	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		path := filepath.Join(dir, entry.Name())
		if now.Sub(info.ModTime()) > maxAge {
			_ = os.Remove(path)
			continue
		}
		kept = append(kept, file{path, info.ModTime()})
	}

	if surplus := len(kept) - maxFiles; surplus > 0 {
		sort.Slice(kept, func(i, j int) bool { return kept[i].time.Before(kept[j].time) })
		for _, old := range kept[:surplus] {
			_ = os.Remove(old.path)
		}
	}
}
