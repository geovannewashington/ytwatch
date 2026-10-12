package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Low-contrast green palette in the spirit of Everforest. Body text uses the
// terminal's own foreground; only accents and the selected title are coloured.
var (
	green      = lipgloss.Color("#a7c080")
	aqua       = lipgloss.Color("#83c092")
	yellow     = lipgloss.Color("#dbbc7f")
	orange     = lipgloss.Color("#e69875")
	red        = lipgloss.Color("#e67e80")
	grey       = lipgloss.Color("#859289")
	greyLight  = lipgloss.Color("#9da9a0")
	borderGrey = lipgloss.Color("#4f5b58")

	titleStyle         = lipgloss.NewStyle().Bold(true).Foreground(green)
	taglineStyle       = lipgloss.NewStyle().Foreground(grey)
	subtitleStyle      = lipgloss.NewStyle().Foreground(grey)
	searchStyle        = lipgloss.NewStyle().Foreground(aqua).Bold(true)
	helpStyle          = lipgloss.NewStyle().Foreground(grey)
	statusStyle        = lipgloss.NewStyle().Foreground(greyLight)
	errorStyle         = lipgloss.NewStyle().Foreground(red).Bold(true)
	panelTitleStyle    = lipgloss.NewStyle().Foreground(green).Bold(true)
	selectedTitleStyle = lipgloss.NewStyle().Foreground(green).Bold(true)
	selectedMetaStyle  = lipgloss.NewStyle().Foreground(greyLight)
	normalTitleStyle   = lipgloss.NewStyle()
	metaStyle          = lipgloss.NewStyle().Foreground(grey)
	descriptionStyle   = lipgloss.NewStyle()
	liveBadgeStyle     = lipgloss.NewStyle().Foreground(red).Bold(true)
	shortBadgeStyle    = lipgloss.NewStyle().Foreground(yellow).Bold(true)
	upcomingBadgeStyle = lipgloss.NewStyle().Foreground(aqua).Bold(true)
	pastLiveBadgeStyle = lipgloss.NewStyle().Foreground(orange).Bold(true)
	playingStyle       = lipgloss.NewStyle().Foreground(green).Bold(true)
	panelStyle         = lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(borderGrey).Padding(0, 1)
	activePanelStyle   = lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(green).Padding(0, 1)
	footerStyle        = lipgloss.NewStyle().Foreground(grey)
	helpBoxStyle       = lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(green).Padding(1, 2)
	keyStyle           = lipgloss.NewStyle().Foreground(aqua)
)

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
