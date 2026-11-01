package tui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	// marqueeInterval is one scroll step: a title moves one cell per tick, so
	// the speed is easy to read.
	marqueeInterval = 220 * time.Millisecond
	// marqueePause is how many ticks a title rests at its start before it
	// scrolls, so the beginning stays readable.
	marqueePause = 7
	// marqueeGap separates the end of a title from its start when it loops.
	marqueeGap = "   "
)

type marqueeMsg struct{}

func tickMarquee() tea.Cmd {
	return tea.Tick(
		marqueeInterval,
		func(time.Time) tea.Msg { return marqueeMsg{} },
	)
}

// marqueeOffset is how many cells a title of loopCells cells has scrolled
// after step ticks. It rests first, then moves one cell per tick, then repeats.
func marqueeOffset(step, loopCells int) int {
	if loopCells <= 0 {
		return 0
	}
	position := step % (marqueePause + loopCells)
	return max(0, position-marqueePause)
}

// scrollWindow returns exactly width cells of s looping with a gap, starting
// offset cells in. Wide characters (for example Japanese) are never split: a
// half-visible one becomes a space.
func scrollWindow(s string, width, offset int) string {
	type cell struct {
		r rune
		w int
	}

	var cells []cell
	total := 0
	for _, r := range s + marqueeGap {
		w := lipgloss.Width(string(r))
		cells = append(cells, cell{r, w})
		total += w
	}

	if width <= 0 || total == 0 {
		return ""
	}
	offset %= total

	i, position := 0, 0
	for position+cells[i].w <= offset {
		position += cells[i].w
		i++
	}

	var b strings.Builder
	used := 0
	if position < offset { // the window starts inside a wide character
		pad := position + cells[i].w - offset
		b.WriteString(strings.Repeat(" ", pad))
		used += pad
		i++
	}

	for used < width {
		if i >= len(cells) {
			i = 0
		}
		c := cells[i]
		if used+c.w > width { // a wide character does not fit at the edge
			b.WriteString(strings.Repeat(" ", width-used))
			break
		}
		b.WriteRune(c.r)
		used += c.w
		i++
	}

	return b.String()
}

// marqueeText fits text into width cells. Short text is returned unchanged.
// Long text shows its start with "~" while resting and then scrolls.
func (m *Model) marqueeText(text string, width int) string {
	textWidth := lipgloss.Width(text)
	if textWidth <= width {
		return text
	}

	offset := marqueeOffset(m.marqueeStep, textWidth+lipgloss.Width(marqueeGap))
	if offset == 0 {
		return truncate(text, width)
	}

	return scrollWindow(text, width, offset)
}

// needsTick reports whether something on the screen moves with time: a
// selected title that does not fit its space, or Watchy while he waits for
// results. Without a tick the program redraws only when something happens, so
// it uses no CPU when idle.
func (m *Model) needsTick() bool {
	if len(m.items) == 0 {
		return m.searching
	}
	v, ok := m.selectedVideo()
	if !ok {
		return false
	}

	title := lipgloss.Width(clean(v.Title))
	results, preview := m.paneWidths()
	// A badge or the playlist mark takes some of the row, so allow for them.
	if title > results-2-maxRowDecoration {
		return true
	}

	return preview > 0 && title > preview
}

// maxRowDecoration is the most columns a badge and a playlist mark can take
// from a result row. It errs on the large side, so a title is never left
// unscrolled; the cost of a wrong guess is only a few extra redraws.
const maxRowDecoration = 16

// syncMarquee restarts the scroll when the selection changes, so a newly
// selected long title always starts at its beginning.
func (m *Model) syncMarquee() {
	if id := m.selectedID(); id != m.marqueeID {
		m.marqueeID, m.marqueeStep = id, 0
	}
}
