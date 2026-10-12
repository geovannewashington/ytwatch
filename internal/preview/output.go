package preview

import "strings"

// ResolveOutput turns the [preview] output setting into the value for
// ueberzugpp's --output. "auto" (or empty) follows the session: Wayland gets
// the wayland layer, an X11 session gets x11, and without a display server
// (a Linux console or SSH) ueberzugpp is left to pick a terminal protocol
// such as kitty or sixel. enabled is false for "off".
func ResolveOutput(setting string, getenv func(string) string) (output string, enabled bool) {
	switch s := strings.ToLower(strings.TrimSpace(setting)); s {
	case "off":
		return "", false
	case "", "auto":
		switch {
		case getenv("WAYLAND_DISPLAY") != "":
			return "wayland", true
		case getenv("DISPLAY") != "":
			return "x11", true
		}
		return "", true
	default:
		return s, true
	}
}
