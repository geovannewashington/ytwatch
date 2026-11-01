package wm

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
)

// lister lists windows on any X11 window manager that supports EWMH, by
// reading wmctrl output.
type lister struct{}

func (lister) Windows(ctx context.Context) (map[string]Window, error) {
	out, err := exec.CommandContext(ctx, "wmctrl", "-l", "-x").Output()
	if err != nil {
		return nil, err
	}
	return parseWmctrl(string(out)), nil
}

// parseWmctrl parses "wmctrl -l -x" lines: id, desktop, class, host, title.
func parseWmctrl(out string) map[string]Window {

	windows := make(map[string]Window)
	for _, line := range strings.Split(out, "\n") {
		fields, title := splitFields(line, 4)
		if len(fields) < 4 {
			continue
		}

		id := fields[0]
		windows[id] = Window{ID: id, Title: title, App: fields[2]}
	}

	return windows
}

// splitFields returns the first n space-separated fields and the rest of the
// line, which keeps its inner spacing.
func splitFields(line string, n int) ([]string, string) {
	var fields []string
	rest := strings.TrimLeft(line, " \t")

	for len(fields) < n && rest != "" {
		i := strings.IndexAny(rest, " \t")
		if i < 0 {
			fields = append(fields, rest)
			rest = ""
			break
		}

		fields = append(fields, rest[:i])
		rest = strings.TrimLeft(rest[i:], " \t")
	}

	return fields, rest
}

// ewmh moves windows between desktops with wmctrl. Desktops have no names
// here, so the workspace is a number counted from 1.
type ewmh struct {
	lister
}

func (ewmh) Move(ctx context.Context, id, workspace string) error {
	n, err := strconv.Atoi(workspace)
	if err != nil || n < 1 {
		return err
	}

	return exec.CommandContext(ctx, "wmctrl", "-i", "-r", id, "-t", strconv.Itoa(n-1)).Run()
}

// bspwm lists windows with wmctrl and moves them with bspc, which accepts
// desktop names and selectors such as "^2".
type bspwm struct {
	lister
}

func (bspwm) Move(ctx context.Context, id, workspace string) error {
	return exec.CommandContext(ctx, "bspc", "node", id, "-d", workspace).Run()
}
