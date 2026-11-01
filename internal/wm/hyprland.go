package wm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
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

// hyprctl runs one hyprctl command. It is a variable so tests can replace it.
var hyprctl = func(ctx context.Context, args ...string) error {
	return exec.CommandContext(ctx, "hyprctl", args...).Run()
}

// Move uses the silent dispatcher so the user's focus stays on the current
// workspace. workspace uses Hyprland syntax, for example "3" or "name:video".
// Hyprland with a Lua config (as Omarchy ships it) rejects the
// legacy dispatcher, so Move then sends the same move as Lua through eval.
func (hyprland) Move(ctx context.Context, id, workspace string) error {
	arg := workspace + ",address:" + id
	legacyErr := hyprctl(ctx, "dispatch", "movetoworkspacesilent", arg)
	if legacyErr == nil {
		return nil
	}

	lua := fmt.Sprintf("hl.dispatch(hl.dsp.window.move({ workspace = %s, follow = false, window = %s }))",
		luaString(workspace), luaString("address:"+id))
	if err := hyprctl(ctx, "eval", lua); err != nil {
		return errors.Join(legacyErr, err)
	}

	return nil
}

// luaString quotes s as a Lua string literal.
func luaString(s string) string {
	s = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(s)
	return `"` + s + `"`
}
