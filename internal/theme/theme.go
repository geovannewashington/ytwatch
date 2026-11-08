// Package theme holds the colour themes of the interface. A theme maps
// semantic roles (accent, live, muted, ...) to colours, so a preset can be
// switched or a single colour overridden in config.toml.
package theme

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Roles are the colour names a theme defines, in the order they are listed.
var Roles = []string{
	"accent",    // titles, selection, active borders, playlists, playback
	"secondary", // search, keys, upcoming streams
	"live",      // live badge
	"short",     // Shorts badge
	"vod",       // past live streams
	"muted",     // secondary text
	"subtle",    // status line and selected details
	"border",    // inactive borders
	"error",     // error messages
}

// Default is the theme used when none is configured.
const Default = "catppuccin"

// Theme is a named set of colours. A colour is a hex value such as "#a7c080"
// or a terminal palette number from 0 to 255 such as "2".
type Theme struct {
	Name   string
	Colors map[string]string
}

// Color returns the colour of a role.
func (t Theme) Color(role string) string { return t.Colors[role] }

func colors(accent, secondary, live, short, vod, muted, subtle, border, errorColor string) map[string]string {
	return map[string]string{
		"accent": accent, "secondary": secondary, "live": live, "short": short,
		"vod": vod, "muted": muted, "subtle": subtle, "border": border, "error": errorColor,
	}
}

var presets = []Theme{
	// catppuccin is the Mocha flavor of Catppuccin, and the default.
	{"catppuccin", colors("#a6e3a1", "#94e2d5", "#f38ba8", "#f9e2af", "#fab387", "#7f849c", "#a6adc8", "#45475a", "#f38ba8")},
	{"everforest", colors("#a7c080", "#83c092", "#e67e80", "#dbbc7f", "#e69875", "#859289", "#9da9a0", "#4f5b58", "#e67e80")},
	{"gruvbox", colors("#b8bb26", "#8ec07c", "#fb4934", "#fabd2f", "#fe8019", "#928374", "#a89984", "#504945", "#fb4934")},
	{"nord", colors("#a3be8c", "#88c0d0", "#bf616a", "#ebcb8b", "#d08770", "#7b88a1", "#9aa5ba", "#4c566a", "#bf616a")},
	{"tokyonight", colors("#9ece6a", "#7dcfff", "#f7768e", "#e0af68", "#ff9e64", "#737aa2", "#a9b1d6", "#3b4261", "#f7768e")},
	{"rose-pine", colors("#9ccfd8", "#c4a7e7", "#eb6f92", "#f6c177", "#ebbcba", "#6e6a86", "#908caa", "#403d52", "#eb6f92")},
	{"dracula", colors("#50fa7b", "#8be9fd", "#ff5555", "#f1fa8c", "#ffb86c", "#6272a4", "#b6b9c9", "#44475a", "#ff5555")},
	// terminal uses the terminal's own 16 colours, so the interface follows
	// whatever palette the terminal or a tool such as pywal sets.
	{"terminal", colors("2", "6", "1", "3", "5", "8", "7", "8", "1")},
}

// Names lists the preset names in a stable order.
func Names() []string {
	names := make([]string, len(presets))
	for i, p := range presets {
		names[i] = p.Name
	}
	return names
}

// Lookup returns a copy of a preset.
func Lookup(name string) (Theme, bool) {
	for _, p := range presets {
		if p.Name == strings.ToLower(strings.TrimSpace(name)) {
			return Theme{Name: p.Name, Colors: cloneColors(p.Colors)}, true
		}
	}
	return Theme{}, false
}

// Next returns the preset after name in the list, wrapping around. An unknown
// name gives the first preset.
func Next(name string) Theme {
	names := Names()
	i := slices.Index(names, name)
	next, _ := Lookup(names[(i+1)%len(names)])
	return next
}

// Resolve builds the theme for a preset name (empty means the default) with
// per-role overrides applied on top. Names and colours are validated, so the
// result is complete.
func Resolve(name string, overrides map[string]string) (Theme, error) {
	if name == "" {
		name = Default
	}
	t, ok := Lookup(name)
	if !ok {
		return Theme{}, UnknownThemeError(name)
	}
	for role, value := range overrides {
		if !slices.Contains(Roles, role) {
			return Theme{}, fmt.Errorf("unknown colour %q (roles: %s)", role, strings.Join(Roles, ", "))
		}

		if !ValidColor(value) {
			return Theme{}, InvalidColorError(value)
		}

		t.Colors[role] = value
	}

	return t, nil
}

// UnknownThemeError lists the available presets.
func UnknownThemeError(name string) error {
	return fmt.Errorf("unknown theme %q (available: %s)", name, strings.Join(Names(), ", "))
}

// InvalidColorError explains the accepted colour formats.
func InvalidColorError(value string) error {
	return fmt.Errorf("invalid colour %q: use a hex value like \"#a7c080\" or a palette number from 0 to 255", value)
}

var hexColor = regexp.MustCompile(`^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)

// ValidColor reports whether value is a hex colour or a 0 to 255 palette
// number.
func ValidColor(value string) bool {
	if hexColor.MatchString(value) {
		return true
	}
	n, err := strconv.Atoi(value)
	return err == nil && n >= 0 && n <= 255
}

func cloneColors(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
