package main

import (
	"io"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/geovannewashington/ytwatch/internal/theme"
)

func TestHelpPageListsEveryRequirementAndNoOptionalTier(t *testing.T) {
	// A renderer on a writer that is not a terminal gives plain text.
	plain := lipgloss.NewRenderer(io.Discard)
	nord, _ := theme.Lookup("nord")

	text := usageText("0.1.0", plain, nord)

	for _, want := range []string{
		"ytwatch 0.1.0", "USAGE", "EXAMPLES", "REQUIRES", "CONFIG",
		"yt-dlp", "mpv", "ffmpeg", "ueberzugpp", "a web browser", "wl-clipboard", "wmctrl",
		"ytwatch doctor", "press ? for the keyboard help",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the help page is missing %q:\n%s", want, text)
		}
	}

	if strings.Contains(text, "Optional") || strings.Contains(text, "Recommended") {
		t.Errorf("every program is required:\n%s", text)
	}

	if strings.Contains(text, "\x1b") {
		t.Errorf("plain output must not have color codes:\n%q", text)
	}

	for _, line := range strings.Split(text, "\n") {
		if width := lipgloss.Width(line); width > 80 {
			t.Errorf("a help line is %d columns wide: %q", width, line)
		}
		if line != strings.TrimRight(line, " ") {
			t.Errorf("a help line ends with spaces: %q", line)
		}
	}
}

func TestHelpStylesFollowTheTheme(t *testing.T) {
	r := lipgloss.NewRenderer(io.Discard)

	for _, name := range theme.Names() {
		th, _ := theme.Lookup(name)
		styles := newHelpStyles(r, th)

		if got := styles.heading.GetForeground(); got != lipgloss.Color(th.Color("accent")) {
			t.Errorf("%s: heading color = %v, want the accent", name, got)
		}
		if got := styles.name.GetForeground(); got != lipgloss.Color(th.Color("secondary")) {
			t.Errorf("%s: name color = %v, want the secondary color", name, got)
		}
		if got := styles.text.GetForeground(); got != lipgloss.Color(th.Color("muted")) {
			t.Errorf("%s: text color = %v, want the muted color", name, got)
		}
	}
}
