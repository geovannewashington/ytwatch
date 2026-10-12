package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/geovannewashington/ytwatch/internal/media"
)

func mixedVideos() []media.Video {
	return []media.Video{
		{ID: "video0001", Title: "One"},
		{ID: "video0002", Title: "Two"},
		{ID: "PLplaylist0001", Title: "A playlist", URL: "https://www.youtube.com/playlist?list=PLplaylist0001"},
		{ID: "video0004", Title: "Four", LiveStatus: "is_upcoming"},
		{ID: "video0005", Title: "Five", IsLive: true},
		{ID: "video0006", Title: "Six"},
	}
}

func TestVisualSelectionExtendsWithMovement(t *testing.T) {
	m := New("test")
	m.items = mixedVideos()
	m.width, m.height = 120, 30
	m.selected = 1
	m.handleKey(runeKey('V'))
	if !m.visual {
		t.Fatal("V should start the selection")
	}
	m.handleKey(runeKey('j'))
	m.handleKey(runeKey('j'))
	if lo, hi := m.selection(); lo != 1 || hi != 3 {
		t.Fatalf("selection = %d..%d, want 1..3", lo, hi)
	}
	// Moving above the anchor selects upwards.
	m.handleKey(runeKey('k'))
	m.handleKey(runeKey('k'))
	m.handleKey(runeKey('k'))
	if lo, hi := m.selection(); lo != 0 || hi != 1 {
		t.Fatalf("selection = %d..%d, want 0..1", lo, hi)
	}
	if !m.inSelection(0) || !m.inSelection(1) || m.inSelection(2) {
		t.Fatal("inSelection is wrong")
	}
	if status := m.renderFooter(); !strings.Contains(status, "VISUAL") || !strings.Contains(status, "2 selected") {
		t.Fatalf("footer = %q", status)
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.visual || m.inSelection(0) {
		t.Fatal("Esc should end the selection")
	}
	// V again also ends it.
	m.handleKey(runeKey('V'))
	m.handleKey(runeKey('V'))
	if m.visual {
		t.Fatal("V should toggle")
	}
}

func TestSelectedVideosSkipsWhatCannotBeQueued(t *testing.T) {
	m := New("test")
	m.items = mixedVideos()
	m.visual, m.visualAnchor, m.selected = true, 0, 5

	play, skip := m.selectedVideos(false)
	var ids []string
	for _, v := range play {
		ids = append(ids, v.ID)
	}
	if got := strings.Join(ids, ","); got != "video0001,video0002,video0005,video0006" {
		t.Fatalf("play queue = %s", got)
	}
	if skip.playlists != 1 || skip.live != 1 || skip.text() != "skipped 1 playlist, 1 live stream" {
		t.Fatalf("skipped = %+v %q", skip, skip.text())
	}

	// A download also leaves out streams that are on air.
	download, skip := m.selectedVideos(true)
	if len(download) != 3 || skip.live != 2 {
		t.Fatalf("download list = %d, skipped %+v", len(download), skip)
	}
}

func TestVisualEnterWithNothingPlayableSaysSo(t *testing.T) {
	m := New("test")
	m.items = mixedVideos()
	m.visual, m.visualAnchor, m.selected = true, 2, 3 // a playlist and an upcoming stream
	if cmd := m.playSelected(); cmd != nil {
		t.Fatal("nothing should start")
	}
	if !strings.Contains(m.status, "Nothing to play") || !strings.Contains(m.status, "skipped 1 playlist") || m.visual {
		t.Fatalf("status = %q, visual = %v", m.status, m.visual)
	}
}

func TestANewSearchEndsTheSelection(t *testing.T) {
	m := New("test")
	m.items = mixedVideos()
	m.handleKey(runeKey('V'))
	m.beginSearch("something else")
	m.cancel()
	if m.visual {
		t.Fatal("a new search should end the selection")
	}
}

func TestSelectedRowsAreMarked(t *testing.T) {
	m := New("test")
	m.items = mixedVideos()
	m.width, m.height = 120, 30
	m.visual, m.visualAnchor, m.selected = true, 0, 1
	out := m.renderResultLines(60, 20)
	if strings.Count(out, "┃ ") < 1 {
		t.Fatalf("the selected row above the cursor should be marked:\n%s", out)
	}
}
