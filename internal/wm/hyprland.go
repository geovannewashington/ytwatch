package wm

import (
	"context"
	"encoding/json"
	"os/exec"
)

type hyprland struct{}

type hyprClient struct {
	Address string `json:"address"`
	Title   string `json:"title"`
	Class   string `json:"class"`
	Mapped  bool   `json:"mapped"`
}

func (hyprland) Windows(ctx context.Context) (map[string]Window, error) {
	out, err := exec.CommandContext(ctx, "hyprctl", "clients", "-j").Output()
	if err != nil {
		return nil, err
	}
	return parseClients(out)
}

func parseClients(data []byte) (map[string]Window, error) {
	var clients []hyprClient
	if err := json.Unmarshal(data, &clients); err != nil {
		return nil, err
	}
	windows := make(map[string]Window, len(clients))
	for _, c := range clients {
		if !c.Mapped || c.Address == "" {
			continue
		}
		windows[c.Address] = Window{ID: c.Address, Title: c.Title, App: c.Class}
	}
	return windows, nil
}

// Move uses the silent dispatcher so the user's focus stays on the current
// workspace. workspace uses Hyprland syntax, for example "3" or "name:video".
func (hyprland) Move(ctx context.Context, id, workspace string) error {
	arg := workspace + ",address:" + id
	return exec.CommandContext(ctx, "hyprctl", "dispatch", "movetoworkspacesilent", arg).Run()
}
