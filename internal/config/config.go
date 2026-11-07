// Package config loads the optional user configuration from
// ${XDG_CONFIG_HOME:-~/.config}/ytwatch/config.toml.
//
// Only a small TOML subset is supported: [sections], key = value pairs,
// "strings", booleans, integers, arrays of strings and # comments.
package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/geovannewashington/ytwatch/internal/locale"
	"github.com/geovannewashington/ytwatch/internal/media"
	"github.com/geovannewashington/ytwatch/internal/quality"
	"github.com/geovannewashington/ytwatch/internal/theme"
)

type Config struct {
	General  General
	MPV      MPV
	Chat     Chat
	Download Download
	Theme    Theme
	Preview  Preview
	History  History
	Cache    Cache
	UI       UI
	Search   Search
	// Links are the programs that open description links, tried in the order
	// they appear in the file. See LinkHandler.
	Links []LinkHandler
}

// LinkHandler opens the description links that match a pattern, for example
// images in nsxiv:
//
//	[link.images]
//	match = '\.(png|jpe?g|gif|webp)(\?.*)?$'
//	command = ["nsxiv", "%l"]
//
// match is a Go regular expression and %l in command is the link. The command
// is run directly, never through a shell. Links without a handler open in the
// browser.
type LinkHandler struct {
	Name    string
	Match   *regexp.Regexp
	Command []string
}

// PreviewOutputs are the accepted values of [preview] output besides "auto"
// and "off". kitty and halfblocks are drawn by ytwatch itself; ueberzugpp and
// the rest are output methods of ueberzugpp.
var PreviewOutputs = []string{"kitty", "halfblocks", "ueberzugpp", "x11", "wayland", "sixel", "iterm2", "chafa"}

// Preview configures the thumbnails.
type Preview struct {
	// Output is how thumbnails are drawn. "auto" (the default) uses kitty in
	// kitty and Ghostty, else ueberzugpp if it is installed, else halfblocks.
	// "off" shows no thumbnails. kitty (the kitty graphics protocol) and
	// halfblocks (coloured characters, works in any terminal) need no other
	// program. ueberzugpp uses ueberzugpp with a wayland or x11 layer chosen
	// from the session; set x11 or wayland when the terminal runs under the
	// other kind of server, for example an X11 terminal inside a Wayland
	// session. sixel, iterm2 and chafa make ueberzugpp draw inside the
	// terminal, which also works over SSH.
	Output string
}

// Search sets the language and region of YouTube searches. Both are optional;
// when empty, YouTube decides from the connection.
type Search struct {
	// Language is a code such as "pt", "ja" or "pt-BR".
	Language string
	// Region is a two-letter country code such as "BR". yt-dlp sends it as a
	// fake X-Forwarded-For address, which YouTube may ignore.
	Region string
	// Hide lists kinds of results that searches never show: shorts, playlists,
	// live, upcoming and vods. A filter that asks for a kind shows it anyway.
	Hide []media.Kind
}

// History configures the list of past searches, which Up and Down walk through
// in the search line. It is a small text file; entries older than MaxAgeDays
// and entries beyond MaxEntries are deleted every time ytwatch starts.
type History struct {
	Enabled    bool
	MaxAgeDays int
	MaxEntries int
}

// UI configures how the interface moves. Animations is "full" (the default),
// "minimal" (only the ones that show progress) or "off".
type UI struct {
	Animations string
}

// Cache limits the thumbnail cache. Files older than MaxAgeDays are deleted
// every time ytwatch starts; then the oldest files go until the folder is
// smaller than MaxSizeMB.
type Cache struct {
	MaxAgeDays int
	MaxSizeMB  int
}

// Theme selects the colours. Name is a preset (see package theme) and Colors
// overrides single roles on top of it, for example accent = "#88c0d0".
type Theme struct {
	Name   string
	Colors map[string]string
}

type General struct {
	// Browser is the command that opens links ("o"), with optional arguments.
	// Empty uses $BROWSER, then the desktop's default browser. The chat window
	// uses it too unless chat.browser is set.
	Browser string
	// AudioOnly starts ytwatch in audio-only mode. It applies to playback and
	// downloads, and can be toggled with "a".
	AudioOnly bool
	// Quality is the video quality for playing and downloading: best (the
	// default), 1080p, 720p, 480p or 360p. It can be changed with "Q".
	Quality string
}

type Download struct {
	// Dir is where downloads are saved. A leading ~ is the home directory.
	Dir string
	// Args are extra command-line arguments passed to yt-dlp. They come last,
	// so they can override the defaults, for example ["-f", "bv*[height<=720]+ba"].
	Args []string
}

type MPV struct {
	// Workspace is the workspace mpv windows are moved to. Supported: sway,
	// i3, Hyprland, niri, bspwm, and X11 window managers with EWMH support
	// (needs wmctrl). The value uses the window manager's own syntax:
	// sway/i3/niri accept a name or number, Hyprland "3" or "name:video",
	// bspwm a desktop name or "^2", wmctrl a number counted from 1. Empty
	// leaves the window where the window manager puts it.
	Workspace string
	// Args are extra command-line arguments passed to mpv.
	Args []string
	// CookiesFromBrowser is a browser name (for example "firefox") whose
	// YouTube cookies yt-dlp uses inside mpv. It avoids YouTube's "confirm
	// you're not a bot" and rate-limit errors. Empty disables it.
	CookiesFromBrowser string
}

type Chat struct {
	// Enabled controls whether live streams open YouTube's chat pop-out.
	Enabled bool
	// Workspace is the workspace the chat window is moved to. See MPV.Workspace.
	// Empty uses the mpv workspace, so chat opens next to the video.
	Workspace string
	// Browser is the command used to open the chat window, with optional
	// arguments. Empty uses $BROWSER, then the desktop's default browser.
	Browser string
}

func Default() Config {
	return Config{
		General:  General{Quality: quality.Best},
		Chat:     Chat{Enabled: true},
		Download: Download{Dir: "~/Downloads/ytwatch"},
		History:  History{Enabled: true, MaxAgeDays: 30, MaxEntries: 100},
		Cache:    Cache{MaxAgeDays: 7, MaxSizeMB: 100},
		UI:       UI{Animations: "full"},
	}
}

// Path returns the location of the config file.
func Path() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, ".config")
	}

	return filepath.Join(dir, "ytwatch", "config.toml")
}

// Load reads the config file. A missing file is not an error and yields the
// defaults.
func Load() (Config, error) {
	path := Path()
	if path == "" {
		return Default(), nil
	}

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Default(), nil
	}
	if err != nil {
		return Default(), err
	}

	cfg, err := Parse(string(data))
	if err != nil {
		return Default(), fmt.Errorf("%s: %w", path, err)
	}

	return cfg, nil
}

func Parse(text string) (Config, error) {
	cfg := Default()
	section := ""
	scanner := bufio.NewScanner(strings.NewReader(text))

	for lineNo := 1; scanner.Scan(); lineNo++ {
		line := strings.TrimSpace(stripComment(scanner.Text()))
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(line[1 : len(line)-1])
			continue
		}

		key, raw, ok := strings.Cut(line, "=")
		if !ok {
			return cfg, fmt.Errorf("line %d: expected key = value", lineNo)
		}

		key, raw = strings.TrimSpace(key), strings.TrimSpace(raw)
		if err := cfg.set(section, key, raw); err != nil {
			return cfg, fmt.Errorf("line %d: %w", lineNo, err)
		}
	}

	if err := scanner.Err(); err != nil {
		return cfg, err
	}

	return cfg, cfg.checkLinks()
}

func (c *Config) set(section, key, raw string) error {
	if section == "theme" {
		return c.setTheme(key, raw)
	}
	if name, ok := strings.CutPrefix(section, "link."); ok {
		return c.setLink(name, key, raw)
	}

	var err error

	switch section + "." + key {
	case "preview.output":
		var value string
		value, err = parseString(raw)
		if err == nil {
			value = strings.ToLower(value)
			if value != "auto" && value != "off" && !slices.Contains(PreviewOutputs, value) {
				return fmt.Errorf(
					"invalid preview output %q (auto, off, %s)",
					value,
					strings.Join(PreviewOutputs, ", "),
				)
			}
			c.Preview.Output = value
		}

	case "general.browser":
		c.General.Browser, err = parseString(raw)
	case "general.audio_only":
		c.General.AudioOnly, err = strconv.ParseBool(raw)
	case "general.quality":
		var value string
		value, err = parseString(raw)
		if err == nil {
			value = quality.Normalize(value)
			if !quality.Valid(value) {
				return fmt.Errorf("invalid quality %q (%s)", value, strings.Join(quality.Levels, ", "))
			}
			c.General.Quality = value
		}

	case "download.dir":
		c.Download.Dir, err = parseString(raw)
	case "download.args":
		c.Download.Args, err = parseStringArray(raw)

	case "mpv.workspace":
		c.MPV.Workspace, err = parseString(raw)
	case "mpv.cookies_from_browser":
		c.MPV.CookiesFromBrowser, err = parseString(raw)
	case "mpv.args":
		c.MPV.Args, err = parseStringArray(raw)

	case "search.language":
		var value string
		if value, err = parseString(raw); err == nil {
			c.Search.Language, err = locale.NormalizeLanguage(value)
		}

	case "search.hide":
		var names []string
		if names, err = parseStringArray(raw); err == nil {
			c.Search.Hide, err = parseKinds(names)
		}

	case "search.region":
		var value string
		if value, err = parseString(raw); err == nil {
			c.Search.Region, err = locale.NormalizeRegion(value)
		}

	case "history.enabled":
		c.History.Enabled, err = strconv.ParseBool(raw)
	case "history.max_age_days":
		c.History.MaxAgeDays, err = parseBoundedInt(raw, 36500) // about 100 years
	case "history.max_entries":
		c.History.MaxEntries, err = parseBoundedInt(raw, 100000)

	case "ui.animations":
		var value string
		if value, err = parseString(raw); err == nil {
			switch value = strings.ToLower(strings.TrimSpace(value)); value {
			case "full", "minimal", "off":
				c.UI.Animations = value
			default:
				err = fmt.Errorf("%q is not one of full, minimal, off", value)
			}
		}

	case "cache.max_age_days":
		c.Cache.MaxAgeDays, err = parseBoundedInt(raw, 36500)
	case "cache.max_size_mb":
		c.Cache.MaxSizeMB, err = parseBoundedInt(raw, 100000)

	case "chat.enabled":
		c.Chat.Enabled, err = strconv.ParseBool(raw)
	case "chat.workspace":
		c.Chat.Workspace, err = parseString(raw)
	case "chat.browser":
		c.Chat.Browser, err = parseString(raw)

	default:
		return fmt.Errorf("unknown setting %q in section [%s]", key, section)
	}

	if err != nil {
		return fmt.Errorf("invalid value for %s: %w", key, err)
	}

	return nil
}

func (c *Config) setTheme(key, raw string) error {
	value, err := parseString(raw)
	if err != nil {
		return fmt.Errorf("invalid value for %s: %w", key, err)
	}

	if key == "name" {
		if _, ok := theme.Lookup(value); !ok {
			return theme.UnknownThemeError(value)
		}
		c.Theme.Name = strings.ToLower(strings.TrimSpace(value))
		return nil
	}

	if !slices.Contains(theme.Roles, key) {
		return fmt.Errorf(
			"unknown setting %q in section [theme] (name, or a colour: %s)",
			key,
			strings.Join(theme.Roles, ", "),
		)
	}
	if !theme.ValidColor(value) {
		return theme.InvalidColorError(value)
	}

	if c.Theme.Colors == nil {
		c.Theme.Colors = make(map[string]string)
	}
	c.Theme.Colors[key] = value

	return nil
}

func (c *Config) setLink(name, key, raw string) error {
	if name == "" {
		return fmt.Errorf("a link section needs a name, like [link.images]")
	}

	i := slices.IndexFunc(c.Links, func(h LinkHandler) bool { return h.Name == name })
	if i < 0 {
		c.Links = append(c.Links, LinkHandler{Name: name})
		i = len(c.Links) - 1
	}

	switch key {
	case "match":
		value, err := parseString(raw)
		if err != nil {
			return fmt.Errorf("invalid value for %s: %w", key, err)
		}

		re, err := regexp.Compile(value)
		if err != nil {
			return fmt.Errorf("invalid pattern for [link.%s]: %w", name, err)
		}
		c.Links[i].Match = re

	case "command":
		value, err := parseStringArray(raw)
		if err != nil {
			return fmt.Errorf("invalid value for %s: %w", key, err)
		}

		if len(value) == 0 || value[0] == "" {
			return fmt.Errorf("command of [link.%s] is empty", name)
		}
		c.Links[i].Command = value

	default:
		return fmt.Errorf("unknown setting %q in section [link.%s] (match, command)", key, name)
	}

	return nil
}

// checkLinks reports a link handler that lacks its pattern or command.
func (c *Config) checkLinks() error {
	for _, h := range c.Links {
		if h.Match == nil || len(h.Command) == 0 {
			return fmt.Errorf("[link.%s] needs both match and command", h.Name)
		}
	}
	return nil
}

// parseString accepts "quoted" strings and bare integers (for workspace = 3).
func parseString(raw string) (string, error) {
	if strings.HasPrefix(raw, `"`) {
		return strconv.Unquote(raw)
	}
	// 'literal strings' have no escapes, which suits regular expressions.
	if len(raw) >= 2 && raw[0] == '\'' && raw[len(raw)-1] == '\'' {
		return raw[1 : len(raw)-1], nil
	}
	if _, err := strconv.Atoi(raw); err == nil {
		return raw, nil
	}
	return "", fmt.Errorf("expected a quoted string, got %s", raw)
}

// parseKinds turns the names in [search] hide into kinds, without repeats.
func parseKinds(names []string) ([]media.Kind, error) {
	var kinds []media.Kind

	for _, name := range names {
		kind, ok := media.ParseKind(name)
		if !ok {
			return nil, fmt.Errorf("unknown kind %q, use: %s", name, media.KindNames)
		}
		if !slices.Contains(kinds, kind) {
			kinds = append(kinds, kind)
		}
	}

	return kinds, nil
}

func parseStringArray(raw string) ([]string, error) {
	if !strings.HasPrefix(raw, "[") || !strings.HasSuffix(raw, "]") {
		return nil, fmt.Errorf("expected an array like [\"a\", \"b\"]")
	}
	inner := strings.TrimSpace(raw[1 : len(raw)-1])

	var out []string

	for inner != "" {
		if inner[0] != '"' {
			return nil, fmt.Errorf("array items must be quoted strings")
		}

		end := 1
		for end < len(inner) && (inner[end] != '"' || inner[end-1] == '\\') {
			end++
		}
		if end >= len(inner) {
			return nil, fmt.Errorf("unterminated string")
		}

		item, err := strconv.Unquote(inner[:end+1])
		if err != nil {
			return nil, err
		}
		out = append(out, item)

		inner = strings.TrimSpace(inner[end+1:])
		inner = strings.TrimSpace(strings.TrimPrefix(inner, ","))
	}

	return out, nil
}

// stripComment removes a trailing # comment that is not inside a string.
func stripComment(line string) string {
	var quote byte // the quote that opened the current string, or 0

	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case c == '\\' && quote == '"':
			i++
		case c == '"' || c == '\'':
			switch quote {
			case 0:
				quote = c
			case c:
				quote = 0
			}
		case c == '#' && quote == 0:
			return line[:i]
		}
	}

	return line
}

// parseBoundedInt parses a whole number from 1 to max.
func parseBoundedInt(raw string, max int) (int, error) {
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > max {
		return 0, fmt.Errorf("%q is not a whole number from 1 to %d", raw, max)
	}
	return n, nil
}
