package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/geovannewashington/ytwatch/internal/media"
)

func TestTheClockRunsOnlyWhenSomethingMoves(t *testing.T) {
	m := New("")
	m.width, m.height = 120, 30
	m.searchMode = false
	if m.needsTick() {
		t.Fatal("an empty list that is not searching is still")
	}
	m.searching = true
	if !m.needsTick() {
		t.Fatal("Watchy moves while searching")
	}
	m.searching = false

	m.items = []media.Video{{ID: "video0001", Title: "Short title"}}
	if m.needsTick() {
		t.Fatal("a title that fits is still")
	}
	m.items[0].Title = strings.Repeat("A very long title that cannot fit in any pane ", 4)
	if !m.needsTick() {
		t.Fatal("a long title scrolls")
	}

	// Update starts the clock when needed, and only once.
	m.ticking = false
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if cmd == nil || !m.ticking {
		t.Fatal("a long title should start the clock")
	}
	// The tick ends the wait; the next Update starts another while needed.
	m.Update(marqueeMsg{})
	if m.marqueeStep != 1 {
		t.Fatalf("step = %d", m.marqueeStep)
	}
	if !m.ticking {
		t.Fatal("the clock should keep running while the title scrolls")
	}
	m.items[0].Title = "Short"
	m.Update(marqueeMsg{})
	if m.ticking {
		t.Fatal("the clock should stop when nothing moves")
	}
}
