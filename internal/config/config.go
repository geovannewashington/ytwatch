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
	"strconv"
	"strings"
)

type Config struct {
	MPV  MPV
	Chat Chat
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
	Workspace string
	// Browser is the command used to open the chat window.
	Browser string
}

func Default() Config {
	return Config{Chat: Chat{Enabled: true, Browser: "firefox"}}
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
	return cfg, scanner.Err()
}

func (c *Config) set(section, key, raw string) error {
	var err error
	switch section + "." + key {
	case "mpv.workspace":
		c.MPV.Workspace, err = parseString(raw)
	case "mpv.cookies_from_browser":
		c.MPV.CookiesFromBrowser, err = parseString(raw)
	case "mpv.args":
		c.MPV.Args, err = parseStringArray(raw)
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

// parseString accepts "quoted" strings and bare integers (for workspace = 3).
func parseString(raw string) (string, error) {
	if strings.HasPrefix(raw, `"`) {
		return strconv.Unquote(raw)
	}
	if _, err := strconv.Atoi(raw); err == nil {
		return raw, nil
	}
	return "", fmt.Errorf("expected a quoted string, got %s", raw)
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
	inString := false
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '\\':
			if inString {
				i++
			}
		case '"':
			inString = !inString
		case '#':
			if !inString {
				return line[:i]
			}
		}
	}
	return line
}
