package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/geovannewashington/ytwatch/internal/config"
	"github.com/geovannewashington/ytwatch/internal/theme"
)

// helpStyles color the help page with the same roles as the interface, so
// the page follows the theme in config.toml. On a terminal without colors, or
// when the output is piped or NO_COLOR is set, the renderer leaves the text
// plain.
type helpStyles struct {
	title   lipgloss.Style // the program name
	heading lipgloss.Style // USAGE, EXAMPLES, ...
	name    lipgloss.Style // commands and program names
	text    lipgloss.Style // what they do
}

func newHelpStyles(r *lipgloss.Renderer, t theme.Theme) helpStyles {
	color := func(role string) lipgloss.Color { return lipgloss.Color(t.Color(role)) }

	return helpStyles{
		title:   r.NewStyle().Bold(true).Foreground(color("accent")),
		heading: r.NewStyle().Bold(true).Foreground(color("accent")),
		name:    r.NewStyle().Foreground(color("secondary")),
		text:    r.NewStyle().Foreground(color("muted")),
	}
}

// helpRow is a name and what it is for.
type helpRow struct{ name, text string }

// helpSection is a heading with rows under it. A row without a name is a plain
// line of text.
type helpSection struct {
	heading string
	rows    []helpRow
}

// requirements are the programs ytwatch needs. All of them are required:
// without ffmpeg a video download is saved as two files that are not merged,
// and without ueberzugpp there are no thumbnails.
var requirements = []helpRow{
	{"yt-dlp", "finds the videos and downloads them"},
	{"mpv", "plays them"},
	{"ffmpeg", "merges video and audio in downloads"},
	{"ueberzugpp", "shows the thumbnails"},
	{"a web browser", "opens links and the live chat"},
	{"wl-clipboard (Wayland)", "copies links (on X11: xclip or xsel)"},
	{"wmctrl (X11, not i3)", "moves the mpv and chat windows to a workspace"},
}

// usageText is the help page: ytwatch --help.
func usageText(version string, r *lipgloss.Renderer, t theme.Theme) string {
	s := newHelpStyles(r, t)

	sections := []helpSection{
		{"USAGE", []helpRow{
			{"ytwatch [search query]", "open ytwatch and search right away"},
			{"ytwatch doctor", "check the programs and the session ytwatch uses"},
			{"ytwatch --version", "print the version"},
		}},
		{"EXAMPLES", []helpRow{
			{"ytwatch", ""},
			{"ytwatch Go interfaces", ""},
			{"ytwatch live coding Go", ""},
		}},
		{"REQUIRES", requirements},
		{"CONFIG", []helpRow{
			{"~/.config/ytwatch/config.toml", ""},
			{"", "theme, workspaces, mpv args, download folder, link handlers, thumbnails"},
		}},
	}

	var b strings.Builder

	b.WriteString(s.title.Render("ytwatch "+version) + s.text.Render(" - search YouTube from your terminal") + "\n")

	for _, section := range sections {
		b.WriteString("\n" + s.heading.Render(section.heading) + "\n")

		width := 0
		for _, row := range section.rows {
			width = max(width, len(row.name))
		}

		for _, row := range section.rows {
			if row.name == "" {
				b.WriteString("  " + s.text.Render(row.text) + "\n")
				continue
			}

			// Pad only when something follows, so lines have no trailing spaces.
			if row.text == "" {
				b.WriteString("  " + s.name.Render(row.name) + "\n")
				continue
			}

			b.WriteString("  " + s.name.Render(fmt.Sprintf("%-*s", width, row.name)))
			b.WriteString("  " + s.text.Render(row.text) + "\n")
		}
	}

	b.WriteString("\n" + s.text.Render("Run ") + s.name.Render("ytwatch doctor") +
		s.text.Render(" to see which programs are installed.") + "\n")
	b.WriteString(s.text.Render("Inside ytwatch, press ") + s.name.Render("?") +
		s.text.Render(" for the keyboard help."))

	return b.String()
}

// printHelp prints the help page in the colors of the configured theme.
func printHelp(version string) {
	cfg, err := config.Load()
	if err != nil {
		cfg = config.Default()
	}

	t, err := theme.Resolve(cfg.Theme.Name, cfg.Theme.Colors)
	if err != nil {
		t, _ = theme.Lookup(theme.Default)
	}

	fmt.Println(usageText(version, lipgloss.DefaultRenderer(), t))
}
