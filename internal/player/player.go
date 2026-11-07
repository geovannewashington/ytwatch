package player

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"syscall"

	"github.com/geovannewashington/ytwatch/internal/config"
	"github.com/geovannewashington/ytwatch/internal/media"
	"github.com/geovannewashington/ytwatch/internal/quality"
	"github.com/geovannewashington/ytwatch/internal/wm"
)

// mpvTitlePrefix identifies windows opened by ytwatch so they can be placed.
const mpvTitlePrefix = "ytwatch | "

// OnWindowPlaced is called after a window ytwatch opened has been moved to
// its workspace, or when the move was given up. The interface uses it to
// redraw the thumbnail, because the terminal is briefly resized while the new
// window is tiled next to it. It may be nil.
var OnWindowPlaced func()

func windowPlaced() {
	if OnWindowPlaced != nil {
		OnWindowPlaced()
	}
}

// Process is a running mpv instance.
type Process struct {
	cmd *exec.Cmd
	out *os.File // mpv's output; a file, not a pipe, so mpv outlives ytwatch

	cancelled atomic.Bool // set by Cancel, read by Wait in another goroutine

	// socketDir holds mpv's control socket, see Options.ControlSocket. It is
	// removed when mpv ends while ytwatch runs.
	socketDir string
}

// Socket is the path of mpv's control socket, or "" when it has none.
func (p *Process) Socket() string {
	if p.socketDir == "" {
		return ""
	}

	return filepath.Join(p.socketDir, socketName)
}

// ErrCancelled is returned by Wait after Cancel stopped mpv.
var ErrCancelled = errors.New("playback canceled")

// Cancel stops mpv. mpv runs in its own session (Setsid), so its process
// group id is its pid; the negative pid signals the whole group, which also
// ends the yt-dlp process mpv started.
func (p *Process) Cancel() {
	p.cancelled.Store(true)
	_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGTERM)
}

// Failure describes an mpv run that ended with an error.
type Failure struct {
	Code   int
	Reason string
}

func (f *Failure) Error() string {
	if f.Reason != "" {
		return fmt.Sprintf("mpv exited with code %d · %s", f.Code, f.Reason)
	}
	return fmt.Sprintf("mpv exited with code %d", f.Code)
}

// Wait blocks until mpv exits. It returns a *Failure when mpv fails.
func (p *Process) Wait() error {
	err := p.cmd.Wait()
	output := tail(p.out)
	p.out.Close()
	if p.socketDir != "" {
		_ = os.RemoveAll(p.socketDir)
	}

	if p.cancelled.Load() {
		return ErrCancelled
	}
	if err == nil {
		return nil
	}

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return err
	}

	return &Failure{Code: exitErr.ExitCode(), Reason: explain(output)}
}

const tailSize = 8 << 10

// tail returns the last bytes of f.
func tail(f *os.File) string {
	info, err := f.Stat()
	if err != nil {
		return ""
	}

	off := max(0, info.Size()-tailSize)
	buf := make([]byte, info.Size()-off)
	n, _ := f.ReadAt(buf, off)

	return string(buf[:n])
}

// explain turns mpv's error output into a short, actionable reason.
func explain(output string) string {
	lower := strings.ToLower(output)
	if strings.Contains(lower, "403 forbidden") {
		return "YouTube refused the video stream (HTTP 403); try again or update yt-dlp"
	}
	if strings.Contains(lower, "sign in to confirm") || strings.Contains(lower, "429") {
		return "YouTube blocked yt-dlp; set cookies_from_browser under [mpv] in config.toml"
	}

	lines := strings.Split(strings.TrimSpace(output), "\n")
	// The generic "Exiting..." line hides the cause, which is an earlier
	// ERROR line from yt-dlp or mpv.
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.Contains(lines[i], "ERROR") || strings.Contains(lines[i], "Failed to open") {
			return shorten(strings.TrimSpace(lines[i]))
		}
	}

	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		return shorten(line)
	}

	return ""
}

func shorten(line string) string {
	if len(line) > 160 {
		return line[:160] + "..."
	}
	return line
}

// Options are the choices made in the interface for one playback.
type Options struct {
	// AudioOnly makes mpv skip the video stream; the window stays so playback
	// can still be controlled.
	AudioOnly bool
	// Quality is a level of package quality; empty or "best" has no limit.
	Quality string
	// WebSafari adds the web_safari YouTube client. It avoids the HTTP 403
	// errors some videos give with the default client, but costs about 0.8
	// seconds of start time, so it is only used to retry after a 403.
	WebSafari bool
	// ControlSocket makes mpv listen on a private socket, so that ytwatch can
	// ask it for the playback time (see PlaybackTime). Only the chat replay
	// needs it.
	ControlSocket bool
}

// IsForbidden reports whether err is mpv failing on an HTTP 403 from YouTube.
func IsForbidden(err error) bool {
	var f *Failure
	return errors.As(err, &f) && strings.Contains(f.Reason, "HTTP 403")
}

// Start opens videos in one mpv. Several videos make a queue that mpv steps
// through like a playlist.
func Start(videos []media.Video, cfg config.Config, opts Options) (*Process, error) {
	if len(videos) == 0 {
		return nil, fmt.Errorf("nothing to play")
	}

	urls := make([]string, len(videos))
	for i, v := range videos {
		if urls[i] = v.CanonicalURL(); urls[i] == "" {
			return nil, fmt.Errorf("could not determine a playable URL")
		}
	}

	path, err := exec.LookPath("mpv")
	if err != nil {
		return nil, fmt.Errorf("mpv is not installed")
	}

	// mpv expands ${...} in a title, so a literal $ is written as $$.
	title := strings.ReplaceAll(strings.ReplaceAll(videos[0].Title, "\n", " "), "$", "$$")
	if len(videos) > 1 {
		// mpv shows the title of whichever video is playing.
		title = "${media-title}"
	}

	// --quiet drops the status line, so the output file only holds messages.
	// Each YouTube client is another request, so only the default one is
	// used at first. skip and player_skip drop requests that are not needed
	// to play. --force-window=immediate opens the window at once and shows
	// it while yt-dlp still looks up the stream.
	// With cookies the default client only gives 360p, so web_safari is
	// always needed then.
	client := "default"
	if opts.WebSafari || cfg.MPV.CookiesFromBrowser != "" {
		client = "default,web_safari"
	}

	args := []string{
		"--force-window=immediate", "--quiet", "--title=" + mpvTitlePrefix + title,
		"--ytdl-raw-options-append=extractor-args=youtube:player_client=" + client +
			";skip=translated_subs;player_skip=configs",
	}
	if opts.AudioOnly {
		args = append(args, "--no-video", "--ytdl-format=bestaudio/best")
	} else if format := quality.Format(opts.Quality); format != "" {
		args = append(args, "--ytdl-format="+format)
	}

	// Forcing the Wayland context would break mpv on X11 sessions.
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		args = append(args, "--gpu-context=wayland")
	}
	if cfg.MPV.CookiesFromBrowser != "" {
		args = append(args, "--ytdl-raw-options-append=cookies-from-browser="+cfg.MPV.CookiesFromBrowser)
	}
	args = append(args, cfg.MPV.Args...)

	// The socket is in a directory only the user can open, because whoever
	// can write to mpv's socket can make mpv run commands.
	var socketDir string
	if opts.ControlSocket {
		if socketDir, err = os.MkdirTemp("", "ytwatch-ipc-"); err != nil {
			return nil, fmt.Errorf("create mpv socket: %w", err)
		}
		args = append(args, "--input-ipc-server="+filepath.Join(socketDir, socketName))
	}

	cmd := exec.Command(path, append(args, append([]string{"--"}, urls...)...)...)
	cmd.Stdin = nil

	// mpv writes its output to an unlinked temp file. A pipe would close when
	// ytwatch exits and kill mpv on its next write; a file lets playback
	// continue after ytwatch quits.
	out, err := os.CreateTemp("", "ytwatch-mpv-*")
	if err != nil {
		return nil, fmt.Errorf("create mpv log: %w", err)
	}
	os.Remove(out.Name())
	cmd.Stdout, cmd.Stderr = out, out
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	backend := wm.Detect()
	before := wm.Snapshot(backend)
	if err := cmd.Start(); err != nil {
		out.Close()
		if socketDir != "" {
			_ = os.RemoveAll(socketDir)
		}

		return nil, fmt.Errorf("launch mpv: %w", err)
	}

	go func() {
		wm.MoveNew(backend, before, func(w wm.Window) bool {
			return strings.HasPrefix(w.Title, mpvTitlePrefix)
		}, cfg.MPV.Workspace)
		windowPlaced()
	}()

	return &Process{cmd: cmd, out: out, socketDir: socketDir}, nil
}

func OpenYouTubeChat(v media.Video, cfg config.Config) error {
	if !v.IsCurrentlyLive() || !cfg.Chat.Enabled {
		return nil
	}
	if !validID(v.ID) {
		return fmt.Errorf("could not identify the live video for chat")
	}

	browser, err := resolveBrowser(
		cmp.Or(cfg.Chat.Browser, cfg.General.Browser),
		os.Getenv("BROWSER"),
		defaultBrowserDesktop(),
		exec.LookPath,
	)
	if err != nil {
		return err
	}

	url := "https://www.youtube.com/live_chat?is_popout=1&v=" + v.ID
	args := browser.args
	if browser.newWindow {
		args = append(slices.Clone(args), "--new-window")
	}

	cmd := exec.Command(browser.path, append(args, url)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, io.Discard, io.Discard
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	backend := wm.Detect()
	before := wm.Snapshot(backend)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("launch chat browser: %w", err)
	}
	go func() { _ = cmd.Wait() }()

	// An already-running browser hands the URL to its main process, so match
	// any new window of that browser rather than the pid we started. With
	// xdg-open the browser is unknown, so the window is not moved.
	if browser.name != "" {
		go func() {
			wm.MoveNew(backend, before, func(w wm.Window) bool {
				return strings.Contains(strings.ToLower(w.App), browser.name)
			}, cmp.Or(cfg.Chat.Workspace, cfg.MPV.Workspace))
			windowPlaced()
		}()
	}

	return nil
}

// OpenURL opens url in the browser (general.browser, $BROWSER, the desktop
// default, then xdg-open), in a new tab where the browser has tabs.
func OpenURL(url string, cfg config.Config) error {
	browser, err := resolveBrowser(
		cfg.General.Browser,
		os.Getenv("BROWSER"),
		defaultBrowserDesktop(),
		exec.LookPath,
	)
	if err != nil {
		return err
	}

	cmd := exec.Command(browser.path, append(slices.Clone(browser.args), url)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, io.Discard, io.Discard
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("launch browser: %w", err)
	}
	go func() { _ = cmd.Wait() }()

	return nil
}

// DescribeBrowser names the browser OpenURL would use, for ytwatch doctor.
func DescribeBrowser(cfg config.Config) string {
	b, err := resolveBrowser(cfg.General.Browser, os.Getenv("BROWSER"), defaultBrowserDesktop(), exec.LookPath)
	if err != nil {
		return ""
	}
	return filepath.Base(b.path)
}

func validID(id string) bool {
	if len(id) < 6 || len(id) > 20 {
		return false
	}

	for _, r := range id {
		if !((r >= 'a' && r <= 'z') ||
			(r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') ||
			r == '_' || r == '-') {
			return false
		}
	}

	return true
}
