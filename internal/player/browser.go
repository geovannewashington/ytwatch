package player

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// chatBrowser is the resolved command used to open the chat window.
type chatBrowser struct {
	path string
	args []string // placed before the URL
	// name identifies the browser's windows so they can be moved to a
	// workspace. It is empty for xdg-open, whose browser is unknown.
	name string
}

// desktopAlias maps default-browser desktop file names to the command that
// differs from them.
var desktopAlias = map[string]string{
	"org.mozilla.firefox":   "firefox",
	"org.chromium.Chromium": "chromium",
	"google-chrome":         "google-chrome-stable",
	"brave-browser":         "brave",
}

// newWindowBrowsers accept --new-window. The check is a substring match on
// the command name, so it also covers variants such as google-chrome-stable.
var newWindowBrowsers = []string{
	"firefox", "librewolf", "floorp", "zen", "chrom", "brave", "vivaldi", "edge",
}

// resolveBrowser picks the browser for the chat window. Order: the configured
// command, $BROWSER, the desktop's default browser (desktop is the file name
// from "xdg-settings get default-web-browser"), then xdg-open. A configured
// browser that is missing is an error instead of falling back, so a typo is
// not hidden.
func resolveBrowser(configured, envBrowser, desktop string, lookPath func(string) (string, error)) (chatBrowser, error) {
	if fields := strings.Fields(configured); len(fields) > 0 {
		path, err := lookPath(fields[0])
		if err != nil {
			return chatBrowser{}, fmt.Errorf("%s is not installed", fields[0])
		}
		return newChatBrowser(path, fields[0], fields[1:]), nil
	}
	for _, entry := range strings.Split(envBrowser, ":") {
		fields := withoutPlaceholder(strings.Fields(entry))
		if len(fields) == 0 {
			continue
		}
		if path, err := lookPath(fields[0]); err == nil {
			return newChatBrowser(path, fields[0], fields[1:]), nil
		}
	}
	if name := strings.TrimSuffix(strings.TrimSpace(desktop), ".desktop"); name != "" {
		candidates := []string{name}
		if alias, ok := desktopAlias[name]; ok {
			candidates = append(candidates, alias)
		}
		for _, c := range candidates {
			if path, err := lookPath(c); err == nil {
				return newChatBrowser(path, c, nil), nil
			}
		}
	}
	path, err := lookPath("xdg-open")
	if err != nil {
		return chatBrowser{}, fmt.Errorf("no browser found: set chat.browser in config.toml")
	}
	return chatBrowser{path: path}, nil
}

// withoutPlaceholder drops the %s URL placeholder some $BROWSER values use.
func withoutPlaceholder(fields []string) []string {
	out := fields[:0:0]
	for _, f := range fields {
		if f != "%s" {
			out = append(out, f)
		}
	}
	return out
}

func newChatBrowser(path, command string, extra []string) chatBrowser {
	name := strings.ToLower(filepath.Base(command))
	args := append([]string(nil), extra...)
	for _, known := range newWindowBrowsers {
		if strings.Contains(name, known) {
			args = append(args, "--new-window")
			break
		}
	}
	name = strings.TrimSuffix(strings.TrimSuffix(name, "-stable"), "-bin")
	return chatBrowser{path: path, args: args, name: name}
}

// defaultBrowserDesktop returns the desktop file name of the default browser,
// or an empty string when it cannot be read.
func defaultBrowserDesktop() string {
	out, err := exec.Command("xdg-settings", "get", "default-web-browser").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
