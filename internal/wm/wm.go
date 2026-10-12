// Package wm moves newly opened windows to a configured workspace. It talks
// to sway, i3, Hyprland, niri, bspwm or any X11 window manager with EWMH
// support (through wmctrl); on any other session everything here is a no-op.
package wm

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Window is a window as seen by the window manager.
type Window struct {
	ID    string
	Title string
	App   string // app_id or X11 class
}

// Backend is a window manager that can list and move windows.
type Backend interface {
	Windows(ctx context.Context) (map[string]Window, error)
	Move(ctx context.Context, id, workspace string) error
}

// Detect returns the backend for the current session, or nil when the
// session is not a supported window manager.
func Detect() Backend {
	switch {
	case isSet("SWAYSOCK") && hasBinary("swaymsg"):
		return swayLike{bin: "swaymsg"}
	case isSet("HYPRLAND_INSTANCE_SIGNATURE") && hasBinary("hyprctl"):
		return hyprland{}
	case isSet("NIRI_SOCKET") && hasBinary("niri"):
		return niri{}
	case isSet("I3SOCK") && hasBinary("i3-msg"):
		return swayLike{bin: "i3-msg"}
	}
	return detectX11()
}

// detectX11 covers X11 sessions. It is skipped on Wayland, where wmctrl only
// sees Xwayland windows and would never find a native mpv window.
func detectX11() Backend {
	if !isSet("DISPLAY") || isSet("WAYLAND_DISPLAY") || !hasBinary("wmctrl") {
		return nil
	}
	if hasBinary("bspc") && exec.Command("bspc", "query", "-M").Run() == nil {
		return bspwm{}
	}
	return ewmh{}
}

func isSet(name string) bool {
	return os.Getenv(name) != ""
}

func hasBinary(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// Snapshot returns the current windows, or nil when b is nil or fails.
func Snapshot(b Backend) map[string]Window {
	if b == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	windows, err := b.Windows(ctx)
	if err != nil {
		return nil
	}
	return windows
}

// pollInterval and moveTimeout are variables so tests can shorten them.
var (
	pollInterval = 150 * time.Millisecond
	moveTimeout  = 15 * time.Second
)

// MoveNew waits for a window that was not in before and satisfies match, then
// moves it to the given workspace. It gives up after a timeout. Run it in a
// goroutine, after taking the before snapshot and before starting the program.
func MoveNew(b Backend, before map[string]Window, match func(Window) bool, workspace string) {
	if b == nil || workspace == "" || before == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), moveTimeout)
	defer cancel()
	for {
		current, err := b.Windows(ctx)
		if err == nil {
			for id, w := range current {
				if _, existed := before[id]; !existed && match(w) {
					_ = b.Move(ctx, id, workspace)
					return
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(pollInterval):
		}
	}
}

// Name describes a backend for messages, such as "sway" or "Hyprland". It is
// empty for a nil backend.
func Name(b Backend) string {
	switch v := b.(type) {
	case swayLike:
		return strings.TrimSuffix(strings.TrimSuffix(v.bin, "msg"), "-")
	case hyprland:
		return "Hyprland"
	case niri:
		return "niri"
	case bspwm:
		return "bspwm"
	case ewmh:
		return "X11 (EWMH, via wmctrl)"
	}
	return ""
}
