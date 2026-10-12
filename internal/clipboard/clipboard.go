// Package clipboard copies text with whichever clipboard tool the session
// has: wl-copy on Wayland, xclip or xsel on X11.
package clipboard

import (
	"errors"
	"os"
	"os/exec"
	"strings"
)

// ErrNoTool is returned when no clipboard tool is installed.
var ErrNoTool = errors.New("no clipboard tool found | install wl-clipboard (Wayland) or xclip / xsel (X11)")

var (
	wayland = [][]string{{"wl-copy"}}
	x11     = [][]string{{"xclip", "-selection", "clipboard"}, {"xsel", "--clipboard", "--input"}}
)

// candidates lists the commands to try, the ones for the current session type
// first. Both kinds are listed because a Wayland session can run X11 programs
// and clipboard tools often work across the two.
func candidates(getenv func(string) string) [][]string {
	if getenv("WAYLAND_DISPLAY") != "" {
		return append(append([][]string{}, wayland...), x11...)
	}
	return append(append([][]string{}, x11...), wayland...)
}

// pick returns the first candidate whose program is installed.
func pick(getenv func(string) string, lookPath func(string) (string, error)) ([]string, error) {
	for _, argv := range candidates(getenv) {
		if _, err := lookPath(argv[0]); err == nil {
			return argv, nil
		}
	}
	return nil, ErrNoTool
}

// Tool names the program Copy would use, or returns ErrNoTool.
func Tool() (string, error) {
	argv, err := pick(os.Getenv, exec.LookPath)
	if err != nil {
		return "", err
	}
	return argv[0], nil
}

// Copy puts text on the clipboard. The tool keeps running in the background
// where needed (xclip serves the text until another program takes over), so
// its output is discarded and ytwatch does not wait for it.
func Copy(text string) error {
	argv, err := pick(os.Getenv, exec.LookPath)
	if err != nil {
		return err
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdin = strings.NewReader(text)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
