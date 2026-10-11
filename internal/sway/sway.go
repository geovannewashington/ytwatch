// Package sway moves newly opened windows to a configured workspace using
// swaymsg. Everything here is a no-op outside a sway session.
package sway

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

type node struct {
	ID               int64  `json:"id"`
	Name             string `json:"name"`
	AppID            string `json:"app_id"`
	WindowProperties struct {
		Class string `json:"class"`
	} `json:"window_properties"`
	Nodes         []node `json:"nodes"`
	FloatingNodes []node `json:"floating_nodes"`
}

// Window is a sway window as seen in the layout tree.
type Window struct {
	ID    int64
	Title string
	App   string // app_id, or the X11 class under Xwayland
}

// Available reports whether we are inside a sway session with swaymsg.
func Available() bool {
	if os.Getenv("SWAYSOCK") == "" {
		return false
	}
	_, err := exec.LookPath("swaymsg")
	return err == nil
}

// Windows returns the ids of all current windows.
func Windows(ctx context.Context) (map[int64]Window, error) {
	out, err := exec.CommandContext(ctx, "swaymsg", "-t", "get_tree", "--raw").Output()
	if err != nil {
		return nil, err
	}
	var root node
	if err := json.Unmarshal(out, &root); err != nil {
		return nil, err
	}
	windows := make(map[int64]Window)
	var walk func(n node)
	walk = func(n node) {
		if len(n.Nodes) == 0 && len(n.FloatingNodes) == 0 && (n.AppID != "" || n.WindowProperties.Class != "") {
			app := n.AppID
			if app == "" {
				app = n.WindowProperties.Class
			}
			windows[n.ID] = Window{ID: n.ID, Title: n.Name, App: app}
		}
		for _, child := range n.Nodes {
			walk(child)
		}
		for _, child := range n.FloatingNodes {
			walk(child)
		}
	}
	walk(root)
	return windows, nil
}

// MoveNew waits for a window that was not in before and satisfies match, then
// moves it to the given workspace. It gives up after a timeout. Run it in a
// goroutine, after taking the before snapshot and before starting the program.
func MoveNew(before map[int64]Window, match func(Window) bool, workspace string) {
	if workspace == "" || before == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	for {
		current, err := Windows(ctx)
		if err == nil {
			for id, w := range current {
				if _, existed := before[id]; !existed && match(w) {
					target := fmt.Sprintf("[con_id=%d] move container to workspace %s", id, quote(workspace))
					_ = exec.CommandContext(ctx, "swaymsg", target).Run()
					return
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(150 * time.Millisecond):
		}
	}
}

// Snapshot returns the current windows, or nil when sway is unavailable.
func Snapshot() map[int64]Window {
	if !Available() {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	windows, err := Windows(ctx)
	if err != nil {
		return nil
	}
	return windows
}

func quote(workspace string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(workspace) + `"`
}
