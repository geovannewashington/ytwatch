package wm

import (
	"context"
	"encoding/json"
	"os/exec"
	"strconv"
)

type niri struct{}

type niriWindow struct {
	ID    uint64 `json:"id"`
	Title string `json:"title"`
	AppID string `json:"app_id"`
}

func (niri) Windows(ctx context.Context) (map[string]Window, error) {
	out, err := exec.CommandContext(ctx, "niri", "msg", "--json", "windows").Output()
	if err != nil {
		return nil, err
	}
	return parseNiriWindows(out)
}

func parseNiriWindows(data []byte) (map[string]Window, error) {
	var list []niriWindow
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, err
	}
	windows := make(map[string]Window, len(list))
	for _, w := range list {
		id := strconv.FormatUint(w.ID, 10)
		windows[id] = Window{ID: id, Title: w.Title, App: w.AppID}
	}
	return windows, nil
}

// Move keeps focus where it is. workspace is a niri workspace reference: an
// index such as "2" or a workspace name.
func (niri) Move(ctx context.Context, id, workspace string) error {
	return exec.CommandContext(ctx, "niri", "msg", "action", "move-window-to-workspace",
		"--window-id", id, "--focus", "false", workspace).Run()
}
