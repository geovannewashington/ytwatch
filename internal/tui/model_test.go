package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/geovannewashington/ytwatch/internal/media"
)

func runeKey(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

func sampleVideos() []media.Video {
	description := strings.Repeat("This is a description with enough words to scroll through. ", 35)
	return []media.Video{
		{ID: "video0001", Title: "First result", Description: description},
		{ID: "video0002", Title: "Second result", Description: description},
		{ID: "video0003", Title: "Third result", Description: description},
		{ID: "video0004", Title: "Fourth result", Description: description},
		{ID: "video0005", Title: "Fifth result", Description: description},
	}
}

func TestSlashOpensCleanSearchPrompt(t *testing.T) {
	m := New("previous search")
	m.searchMode = false
	m.handleKey(runeKey('/'))

	if !m.searchMode {
		t.Fatal("/ should activate search mode")
	}
	if m.searchInput != "" {
		t.Fatalf("search input = %q, want empty input", m.searchInput)
	}
	if m.query != "previous search" {
		t.Fatalf("current query unexpectedly changed: %q", m.query)
	}
}

func TestVimPaneNavigationDoesNotAliasVerticalMotion(t *testing.T) {
	m := New("test")
	m.width, m.height = 120, 30
	m.items = sampleVideos()

	m.handleKey(runeKey('j'))
	if m.selected != 1 {
		t.Fatalf("j selected index %d, want 1", m.selected)
	}

	m.handleKey(runeKey('l'))
	if m.focus != previewPane {
		t.Fatal("l should focus the preview pane")
	}
	if m.selected != 1 {
		t.Fatalf("l changed selected result to %d", m.selected)
	}

	m.handleKey(runeKey('j'))
	if m.previewScroll == 0 {
		t.Fatal("j in the preview pane should scroll the description")
	}
	if m.selected != 1 {
		t.Fatalf("preview scroll changed selected result to %d", m.selected)
	}

	m.handleKey(runeKey('h'))
	if m.focus != resultsPane {
		t.Fatal("h should focus the results pane")
	}
	m.handleKey(runeKey('j'))
	if m.selected != 2 {
		t.Fatalf("j in results pane selected index %d, want 2", m.selected)
	}
}

func TestVimCountsAndFirstLastMotions(t *testing.T) {
	m := New("test")
	m.width, m.height = 120, 30
	m.items = sampleVideos()

	m.handleKey(runeKey('3'))
	m.handleKey(runeKey('j'))
	if m.selected != 3 {
		t.Fatalf("3j selected index %d, want 3", m.selected)
	}

	m.handleKey(runeKey('g'))
	m.handleKey(runeKey('g'))
	if m.selected != 0 {
		t.Fatalf("gg selected index %d, want 0", m.selected)
	}

	m.handleKey(runeKey('G'))
	if m.selected != len(m.items)-1 {
		t.Fatalf("G selected index %d, want last index %d", m.selected, len(m.items)-1)
	}
}

func TestResultRowsKeepTheirWidthWithBadgesAndPlayback(t *testing.T) {
	m := New("test")
	m.width, m.height = 120, 30
	m.items = []media.Video{
		{ID: "video0001", Title: "Plain video"},
		{ID: "video0002", Title: "A very long live stream title that must be cut to fit", IsLive: true},
		{ID: "video0003", Title: "Short clip", Short: true},
		{ID: "video0004", Title: "Past stream", WasLive: true},
	}
	m.playing["video0002"] = "playing"
	const width = 40

	rows := strings.Split(m.renderResultLines(width, 12), "\n")
	for i, row := range rows {
		if got := lipgloss.Width(row); got > width {
			t.Fatalf("row %d width = %d, want at most %d: %q", i, got, width, row)
		}
	}
	for _, label := range []string{"LIVE", "SHORT", "VOD"} {
		if !strings.Contains(strings.Join(rows, "\n"), label) {
			t.Fatalf("missing %s badge in rows", label)
		}
	}
}
