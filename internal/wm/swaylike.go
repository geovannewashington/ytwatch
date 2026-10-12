package wm

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// swayLike drives sway and i3, which share the same IPC tree format and
// command syntax. bin is "swaymsg" or "i3-msg".
type swayLike struct {
	bin string
}

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

func (s swayLike) Windows(ctx context.Context) (map[string]Window, error) {
	out, err := exec.CommandContext(ctx, s.bin, "-t", "get_tree", "--raw").Output()
	if err != nil {
		return nil, err
	}
	return parseTree(out)
}

func parseTree(data []byte) (map[string]Window, error) {
	var root node
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	windows := make(map[string]Window)
	var walk func(n node)
	walk = func(n node) {
		if len(n.Nodes) == 0 && len(n.FloatingNodes) == 0 && (n.AppID != "" || n.WindowProperties.Class != "") {
			app := n.AppID
			if app == "" {
				app = n.WindowProperties.Class
			}
			id := strconv.FormatInt(n.ID, 10)
			windows[id] = Window{ID: id, Title: n.Name, App: app}
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

func (s swayLike) Move(ctx context.Context, id, workspace string) error {
	cmd := fmt.Sprintf("[con_id=%s] move container to workspace %s", id, quote(workspace))
	return exec.CommandContext(ctx, s.bin, cmd).Run()
}

func quote(workspace string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(workspace) + `"`
}
