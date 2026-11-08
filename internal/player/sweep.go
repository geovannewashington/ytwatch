package player

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// maxSocketPath is the longest path a Unix socket can have: the address has
// 108 bytes, and a few are kept free.
const maxSocketPath = 100

// workDirPrefix starts the name of the folder of one playback.
const workDirPrefix = "ytwatch-mpv-"

// SweepStale removes the folders of old playbacks from the temp folder. mpv
// may keep playing after ytwatch quits (that is wanted), so nobody is left to
// remove its folder, which holds the pictures of the seek bar. A folder is
// removed only when it is a real folder (not a link), it is the user's own,
// and it is older than maxAge, so one that is in use is not touched.
func SweepStale(maxAge time.Duration, now time.Time) {
	dir := os.TempDir()

	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), workDirPrefix) || !entry.IsDir() {
			continue
		}

		path := filepath.Join(dir, entry.Name())

		info, err := os.Lstat(path)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			continue
		}

		owner, ok := info.Sys().(*syscall.Stat_t)
		if !ok || int(owner.Uid) != os.Getuid() || now.Sub(info.ModTime()) < maxAge {
			continue
		}

		_ = os.RemoveAll(path)
	}
}
