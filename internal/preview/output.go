package preview

import "strings"

// Mode is how thumbnails are drawn.
type Mode int

const (
	ModeOff        Mode = iota // no thumbnails
	ModeKitty                  // kitty graphics protocol with Unicode placeholders
	ModeHalfBlocks             // coloured half-block characters, works everywhere
	ModeUeberzug               // an ueberzugpp window or terminal output
)

// Native reports whether the mode draws inside the terminal text, with no
// helper program.
func (m Mode) Native() bool { return m == ModeKitty || m == ModeHalfBlocks }

// Output is the resolved [preview] output setting.
type Output struct {
	Mode Mode
	// Ueberzug is the value for ueberzugpp's --output; empty lets ueberzugpp
	// choose. Only used in Ueberzug mode.
	Ueberzug string
	// Reason says why the mode was chosen, for ytwatch doctor.
	Reason string
}

// KittyHint reports whether the environment names a terminal known to support
// Unicode placeholders. The graphics query alone does not prove it: WezTerm and
// Konsole answer it but cannot draw placeholders.
func KittyHint(getenv func(string) string) bool {
	return getenv("KITTY_WINDOW_ID") != "" ||
		getenv("TERM") == "xterm-kitty" ||
		getenv("GHOSTTY_RESOURCES_DIR") != "" ||
		getenv("TERM_PROGRAM") == "ghostty"
}

// WantsKittyQuery reports whether the setting needs the terminal to be asked
// about the kitty protocol: only "auto" in a terminal that may support it.
func WantsKittyQuery(setting string, getenv func(string) string) bool {
	s := strings.ToLower(strings.TrimSpace(setting))
	return (s == "" || s == "auto") && KittyHint(getenv)
}

// ResolveOutput turns the [preview] output setting into a mode. "auto" (or
// empty) picks the kitty renderer when the terminal answered the query and the
// environment names kitty or Ghostty, else ueberzugpp when it is installed,
// else half-blocks. ueberzugpp's own output follows the session: Wayland gets
// the wayland layer, an X11 session gets x11, and without a display server
// (a Linux console or SSH) ueberzugpp is left to pick a terminal protocol such
// as sixel.
func ResolveOutput(setting string, getenv func(string) string, kittyDetected, ueberzugInstalled bool) Output {
	switch s := strings.ToLower(strings.TrimSpace(setting)); s {
	case "off":
		return Output{Mode: ModeOff, Reason: "set in config.toml"}
	case "kitty":
		return Output{Mode: ModeKitty, Reason: "set in config.toml"}
	case "halfblocks":
		return Output{Mode: ModeHalfBlocks, Reason: "set in config.toml"}
	case "ueberzugpp":
		return Output{Mode: ModeUeberzug, Ueberzug: sessionOutput(getenv), Reason: "set in config.toml"}
	case "", "auto":
		switch {
		case kittyDetected && KittyHint(getenv):
			return Output{Mode: ModeKitty, Reason: "the terminal answered the kitty graphics query"}
		case ueberzugInstalled:
			return Output{Mode: ModeUeberzug, Ueberzug: sessionOutput(getenv), Reason: "ueberzugpp is installed"}
		}
		return Output{Mode: ModeHalfBlocks, Reason: "no kitty graphics and ueberzugpp is not installed"}
	default:
		return Output{Mode: ModeUeberzug, Ueberzug: s, Reason: "set in config.toml"}
	}
}

func sessionOutput(getenv func(string) string) string {
	switch {
	case getenv("WAYLAND_DISPLAY") != "":
		return "wayland"
	case getenv("DISPLAY") != "":
		return "x11"
	}
	return ""
}
