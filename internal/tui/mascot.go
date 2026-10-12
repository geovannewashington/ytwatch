package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Watchy is the mascot: a little round face with antennae, like a tiny TV. He
// shows in an empty list. Every glyph is one terminal column wide, which was
// checked in tmux, so the drawing keeps its shape.

type mascotMood int

const (
	moodIdle      mascotMood = iota // smiling
	moodSearching                   // wiggling his antennae, mouth open
	moodSad                         // flat mouth: an error or no results
)

// mascotHeight and mascotWidth are the size of the drawing.
const (
	mascotHeight = 4
	mascotWidth  = 7
)

// mouthColumn is where the mouth is in the last row.
const mouthColumn = 3

// mascotRows returns the rows of the drawing. step changes the pose over time.
func mascotRows(mood mascotMood, step int) []string {
	antennae := "  ╲ ╱  "
	mouth := "◡"
	switch mood {
	case moodSearching:
		if step%2 == 1 {
			antennae = "  ╱ ╲  "
		}
		mouth = "o"
	case moodSad:
		mouth = "─"
	}
	return []string{
		antennae,
		"╭─────╮",
		"│ • • │",
		"╰──" + mouth + "──╯",
	}
}

// renderMascot draws Watchy centered in width columns. step counts the ticks
// of the screen's clock and moves the antennae while he searches. The parts
// are colored by where they are, not by what the character is, because the
// mouth can be the same character as the outline.
func renderMascot(width int, mood mascotMood, step int) []string {
	rows := mascotRows(mood, step/2)
	out := make([]string, len(rows))
	for i, row := range rows {
		var b strings.Builder
		for col, r := range []rune(row) {
			text := string(r)
			switch {
			case r == ' ':
				b.WriteString(text)
			case i == 0:
				b.WriteString(metaStyle.Render(text))
			case i == len(rows)-1 && col == mouthColumn:
				b.WriteString(shortBadgeStyle.Render(text))
			case r == '•':
				b.WriteString(keyStyle.Render(text))
			default:
				b.WriteString(playlistBadgeStyle.Render(text))
			}
		}
		out[i] = lipgloss.PlaceHorizontal(width, lipgloss.Center, b.String())
	}
	return out
}
