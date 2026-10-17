package preview

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/geovannewashington/ytwatch/internal/media"
)

var cacheDir = filepath.Join(userCacheDir(), "ytwatch", "thumbnails")

func userCacheDir() string {
	if value := os.Getenv("XDG_CACHE_HOME"); value != "" {
		return value
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return os.TempDir()
	}
	return filepath.Join(home, ".cache")
}

// Prune deletes thumbnails older than maxAge, then the oldest ones until the
// folder is no larger than maxBytes. Errors are ignored: a file that cannot be
// deleted now is tried again at the next start.
func Prune(maxAge time.Duration, maxBytes int64, now time.Time) {
	prune(cacheDir, maxAge, maxBytes, now)
}

func prune(dir string, maxAge time.Duration, maxBytes int64, now time.Time) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type cached struct {
		path string
		size int64
		when time.Time
	}
	var kept []cached
	var total int64
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if now.Sub(info.ModTime()) > maxAge {
			_ = os.Remove(path)
			continue
		}
		kept = append(kept, cached{path, info.Size(), info.ModTime()})
		total += info.Size()
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].when.Before(kept[j].when) })
	for _, f := range kept {
		if total <= maxBytes {
			break
		}
		if os.Remove(f.path) == nil {
			total -= f.size
		}
	}
}

// validFileID reports whether id is safe to use as a cache file name. The limit
// is high enough for playlist ids, which are about 34 characters.
func validFileID(id string) bool {
	if len(id) < 6 || len(id) > 64 {
		return false
	}
	for _, r := range id {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

// cacheVersion is part of the cache file name. Raise it when the choice of
// thumbnails improves, so old low-resolution files are not reused.
const cacheVersion = "q2"

// errUnusable marks a candidate that is missing or in a format the overlay may
// not read, so the next candidate is tried. Network errors are not wrapped in
// it: they fail the download so a bad moment does not cache a worse image.
var errUnusable = errors.New("thumbnail not usable")

// Download saves the best usable thumbnail of v and returns its path.
func Download(ctx context.Context, v media.Video) (string, error) {
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		return "", err
	}
	if !validFileID(v.ID) {
		return "", errors.New("invalid video ID")
	}
	candidates := v.ThumbnailCandidates()
	if len(candidates) == 0 {
		return "", errors.New("no valid HTTPS thumbnail URL")
	}

	destination := filepath.Join(cacheDir, v.ID+"-"+cacheVersion+".jpg")
	if info, err := os.Stat(destination); err == nil && info.Size() > 0 {
		// A file in use stays young, so Prune removes unused files first.
		now := time.Now()
		_ = os.Chtimes(destination, now, now)
		return destination, nil
	}
	var data []byte
	for _, candidate := range candidates {
		image, err := fetchImage(ctx, candidate)
		if errors.Is(err, errUnusable) {
			continue
		}
		if err != nil {
			return "", err
		}
		data = image
		break
	}
	if data == nil {
		return "", errors.New("no usable thumbnail")
	}
	temp := destination + ".tmp-" + fmt.Sprint(os.Getpid())
	if err := os.WriteFile(temp, data, 0o600); err != nil {
		return "", err
	}
	if err := os.Rename(temp, destination); err != nil {
		_ = os.Remove(temp)
		return "", err
	}
	return destination, nil
}

// fetchImage downloads one candidate. WebP is rejected because it is often
// served under a .jpg name and not every ueberzugpp build can read it.
func fetchImage(ctx context.Context, thumbURL string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, thumbURL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) ytwatch/1.0")
	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound, http.StatusForbidden, http.StatusGone:
		return nil, errUnusable
	default:
		return nil, fmt.Errorf("thumbnail server returned %s", response.Status)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 5*1024*1024+1))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || len(data) > 5*1024*1024 {
		return nil, errUnusable
	}
	if http.DetectContentType(data) == "image/webp" {
		return nil, errUnusable
	}
	return data, nil
}

type Overlay struct {
	output string // ueberzugpp --output value; empty lets ueberzugpp choose
	off    bool   // thumbnails are switched off in the config

	mu     sync.Mutex
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	done   chan error
	active bool
}

// Configure applies the [preview] output setting: "auto", "off", or an
// ueberzugpp output such as "x11".
func (o *Overlay) Configure(setting string) {
	var enabled bool
	o.output, enabled = ResolveOutput(setting, os.Getenv)
	o.off = !enabled
}

// Disabled reports whether thumbnails are switched off in the config.
func (o *Overlay) Disabled() bool { return o.off }

func Available() bool {
	_, err := exec.LookPath("ueberzugpp")
	return err == nil
}

func (o *Overlay) startLocked() bool {
	if o.active && o.cmd != nil && o.cmd.Process != nil {
		select {
		case <-o.done:
			// The overlay process exited unexpectedly. Reset the stale handles
			// so the next Show call can start a clean process.
			if o.stdin != nil {
				_ = o.stdin.Close()
			}
			o.cmd, o.stdin, o.done, o.active = nil, nil, nil, false
		default:
			return true
		}
	}
	path, err := exec.LookPath("ueberzugpp")
	if err != nil {
		return false
	}
	args := []string{"layer"}
	if o.output != "" {
		args = append(args, "--output", o.output)
	}
	cmd := exec.Command(path, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return false
	}
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return false
	}
	done := make(chan error, 1)
	o.cmd, o.stdin, o.done, o.active = cmd, stdin, done, true
	go func() { done <- cmd.Wait() }()
	return true
}

func (o *Overlay) send(payload map[string]any) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.startLocked() || o.stdin == nil {
		return false
	}
	return o.writeLocked(payload)
}

func (o *Overlay) writeLocked(payload map[string]any) bool {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return false
	}
	encoded = append(encoded, '\n')
	if _, err := o.stdin.Write(encoded); err != nil {
		if o.stdin != nil {
			_ = o.stdin.Close()
		}
		if o.cmd != nil && o.cmd.Process != nil {
			_ = o.cmd.Process.Kill()
		}
		o.active = false
		o.cmd, o.stdin, o.done = nil, nil, nil
		return false
	}
	return true
}

func (o *Overlay) Show(path string, x, y, width, height int) bool {
	if path == "" || width < 8 || height < 3 {
		o.Remove()
		return false
	}
	// Adding the same identifier updates the layer without a remove/add flash.
	return o.send(map[string]any{
		"action": "add", "identifier": "ytwatch-preview", "path": path,
		"x": x, "y": y, "max_width": width, "max_height": height,
		"scaler": "fit_contain",
	})
}

func (o *Overlay) Remove() {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.active || o.stdin == nil {
		return
	}
	_ = o.writeLocked(map[string]any{"action": "remove", "identifier": "ytwatch-preview"})
}

func (o *Overlay) Close() {
	o.Remove()
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.stdin != nil {
		_ = o.stdin.Close()
	}
	if o.cmd != nil && o.cmd.Process != nil && o.active {
		_ = o.cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-o.done:
		case <-time.After(400 * time.Millisecond):
			_ = o.cmd.Process.Kill()
		}
	}
	o.active = false
}

func DescriptionSnippet(value string, maxRunes int) string {
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	if maxRunes < 2 {
		return string(runes[:maxRunes])
	}
	return string(runes[:maxRunes-1]) + "…"
}
