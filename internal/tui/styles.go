package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/geovannewashington/ytwatch/internal/media"
	"github.com/geovannewashington/ytwatch/internal/theme"
)

// The styles are set by ApplyTheme, which runs for the default theme at start
// and again when the configured or cycled theme changes. Body text uses the
// terminal's own foreground; only accents and the selected title are coloured.
var (
	titleStyle         lipgloss.Style
	taglineStyle       lipgloss.Style
	subtitleStyle      lipgloss.Style
	searchStyle        lipgloss.Style
	helpStyle          lipgloss.Style
	statusStyle        lipgloss.Style
	errorStyle         lipgloss.Style
	panelTitleStyle    lipgloss.Style
	selectedTitleStyle lipgloss.Style
	selectedMetaStyle  lipgloss.Style
	normalTitleStyle   lipgloss.Style
	metaStyle          lipgloss.Style
	descriptionStyle   lipgloss.Style
	liveBadgeStyle     lipgloss.Style
	progressStyle      lipgloss.Style
	shortBadgeStyle    lipgloss.Style
	upcomingBadgeStyle lipgloss.Style
	pastLiveBadgeStyle lipgloss.Style
	playlistBadgeStyle lipgloss.Style
	playingStyle       lipgloss.Style
	panelStyle         lipgloss.Style
	activePanelStyle   lipgloss.Style
	footerStyle        lipgloss.Style
	helpBoxStyle       lipgloss.Style
	keyStyle           lipgloss.Style
)

func init() {
	def, _ := theme.Lookup(theme.Default)
	ApplyTheme(def)
}

// ApplyTheme rebuilds every style from the theme's colours.
func ApplyTheme(t theme.Theme) {
	color := func(role string) lipgloss.Color { return lipgloss.Color(t.Color(role)) }
	accent, secondary := color("accent"), color("secondary")
	muted, subtle := color("muted"), color("subtle")
	plain := func(c lipgloss.Color) lipgloss.Style { return lipgloss.NewStyle().Foreground(c) }
	box := func(border lipgloss.Color) lipgloss.Style {
		return lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(border)
	}

	titleStyle = plain(accent)
	taglineStyle = plain(muted)
	subtitleStyle = plain(muted)
	searchStyle = plain(secondary)
	helpStyle = plain(muted)
	statusStyle = plain(subtle)
	errorStyle = plain(color("error"))
	panelTitleStyle = plain(accent)
	selectedTitleStyle = plain(accent)
	selectedMetaStyle = plain(subtle)
	normalTitleStyle = lipgloss.NewStyle()
	metaStyle = plain(muted)
	descriptionStyle = lipgloss.NewStyle()
	liveBadgeStyle = plain(color("live"))
	progressStyle = plain(color("live"))
	shortBadgeStyle = plain(color("short"))
	upcomingBadgeStyle = plain(secondary)
	pastLiveBadgeStyle = plain(color("vod"))
	playlistBadgeStyle = plain(accent)
	playingStyle = plain(accent)
	panelStyle = box(color("border")).Padding(0, 1)
	activePanelStyle = box(accent).Padding(0, 1)
	footerStyle = plain(muted)
	helpBoxStyle = box(accent).Padding(1, 2)
	keyStyle = plain(secondary)
}

// eqFrames is one bar of the playing equalizer over time.
var eqFrames = []string{"▁", "▃", "▅", "▇", "▅", "▃"}

// equalizer renders n bars that move out of phase, so playback looks alive.
func equalizer(frame, n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteString(eqFrames[(frame+i*2)%len(eqFrames)])
	}

	return playingStyle.Render(b.String())
}

// playlistMark precedes playlist titles. Pick symbols that every terminal and
// multiplexer measures as one cell: tmux counts U+2630 (☰) as two cells while
// the width libraries count one, which shifted those rows and corrupted the
// screen.
const playlistMark = "≡"

// courseMark and podcastMark are Nerd Font icons (a graduation cap and a
// microphone) that replace playlistMark in front of a course or a podcast, as
// the verified mark is one too. They are Material Design icons (Nerd Fonts
// 3.0 and later), which fill more of the cell than the Font Awesome ones.
const (
	courseMark  = "\U000f0474" // nf-md-school
	podcastMark = "\U000f036c" // nf-md-microphone
)

// playlistMarkOf is the symbol in front of the title of a playlist: a plain
// playlist, a course or a podcast.
func playlistMarkOf(v media.Video) string {
	switch v.PlaylistType {
	case media.PlaylistCourse:
		return courseMark
	case media.PlaylistPodcast:
		return podcastMark
	}

	return playlistMark
}

// playlistLabelOf is the name of the kind of playlist, for its badge.
func playlistLabelOf(v media.Video) string {
	switch v.PlaylistType {
	case media.PlaylistCourse:
		return "COURSE"
	case media.PlaylistPodcast:
		return "PODCAST"
	}

	return "PLAYLIST"
}
