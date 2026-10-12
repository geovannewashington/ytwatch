package tui

import (
	"strings"
	"testing"
	"time"

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

func TestAudioOnlyToggle(t *testing.T) {
	m := New("test")
	m.items = sampleVideos()
	m.handleKey(runeKey('a'))
	if !m.audioOnly {
		t.Fatal("a should turn audio-only on")
	}
	m.handleKey(runeKey('a'))
	if m.audioOnly {
		t.Fatal("a should turn audio-only off")
	}
}

func TestDownloadRefusesLiveAndDuplicates(t *testing.T) {
	m := New("test")
	m.items = []media.Video{{ID: "video0001", Title: "Live", IsLive: true}, {ID: "video0002", Title: "VOD"}}

	if cmd := m.handleKey(runeKey('d')); cmd != nil || len(m.downloading) != 0 {
		t.Fatal("live stream must not start a download")
	}
	m.selected = 1
	if cmd := m.handleKey(runeKey('d')); cmd == nil || len(m.downloading) != 1 {
		t.Fatal("regular video should start a download")
	}
	if cmd := m.handleKey(runeKey('d')); cmd != nil {
		t.Fatal("same video must not download twice at once")
	}
}

func TestHelpFitsTheTerminal(t *testing.T) {
	for _, size := range [][2]int{{80, 18}, {80, 24}, {100, 30}, {150, 40}} {
		m := New("test")
		m.width, m.height = size[0], size[1]
		m.showHelp = true
		out := m.View()
		lines := strings.Split(out, "\n")
		if len(lines) != size[1] {
			t.Errorf("%v: help is %d lines tall, want %d", size, len(lines), size[1])
		}
		for i, line := range lines {
			if w := lipgloss.Width(line); w > size[0] {
				t.Errorf("%v: help line %d is %d wide", size, i, w)
			}
		}
		for _, want := range []string{"KEYBOARD", "Quit"} {
			if !strings.Contains(out, want) {
				t.Errorf("%v: help is missing %q (cut off)", size, want)
			}
		}
	}
}

func TestRowsFitWithWideCharactersAndPlaylists(t *testing.T) {
	m := New("test")
	m.width, m.height = 120, 30
	m.items = []media.Video{
		{ID: "PLpCqPSEm2Xe8sEY2haMDUVgwbkIs5NCJI", Title: "A playlist with a very long title that has to be cut to fit the pane",
			URL: "https://www.youtube.com/playlist?list=PLpCqPSEm2Xe8sEY2haMDUVgwbkIs5NCJI"},
		{ID: "video0001", Title: "日本語のチュートリアル：ゴーを学ぶための完全ガイド日本語のチュートリアル", Duration: 60},
		{ID: "video0002", Title: "Emoji 🎉 title 👨‍👩‍👧‍👦 that is long enough to be cut off at the edge", Duration: 60},
	}
	const width = 40
	for i, row := range strings.Split(m.renderResultLines(width, 12), "\n") {
		if w := lipgloss.Width(row); w > width {
			t.Errorf("row %d is %d wide, want at most %d: %q", i, w, width, row)
		}
	}
}

func TestPlaylistRowAndPreviewShowYouTubeStyleDetails(t *testing.T) {
	m := New("test")
	m.width, m.height = 140, 40
	today := time.Now().UTC().Format("20060102")
	m.items = []media.Video{{
		ID: "PLpCqPSEm2Xe8sEY2haMDUVgwbkIs5NCJI", Title: "Songs 2026", Channel: "Chillax",
		URL:           "https://www.youtube.com/playlist?list=PLpCqPSEm2Xe8sEY2haMDUVgwbkIs5NCJI",
		PlaylistCount: 110, ModifiedDate: today, Description: "A curated playlist of the best songs.",
		Entries: []media.PlaylistEntry{
			{Title: "Alex Warren - Ordinary (Lyrics)", Duration: 186},
			{Title: "Taylor Swift - Opalite (Lyrics)", Duration: 237},
		},
	}}
	m.metadataDone = true

	row := m.renderResultLines(80, 6)
	for _, want := range []string{"Chillax", "playlist", "110 videos", "updated today", "PLAYLIST"} {
		if !strings.Contains(row, want) {
			t.Errorf("row is missing %q:\n%s", want, row)
		}
	}
	for _, unwanted := range []string{"views", "ago"} {
		if strings.Contains(row, unwanted) {
			t.Errorf("row should not show %q:\n%s", unwanted, row)
		}
	}

	preview := m.renderPreview(70, 30)
	for _, want := range []string{
		"110 videos | updated today",
		"Alex Warren - Ordinary (Lyrics) · 3:06",
		"Taylor Swift - Opalite (Lyrics) · 3:57",
		"DESCRIPTION", "A curated playlist of the best songs.",
	} {
		if !strings.Contains(preview, want) {
			t.Errorf("preview is missing %q:\n%s", want, preview)
		}
	}
}

func TestScrollWindow(t *testing.T) {
	// "abcdef" + 3 spaces loops every 9 cells.
	for offset, want := range map[int]string{0: "abcd", 2: "cdef", 5: "f   ", 7: "  ab", 9: "abcd"} {
		if got := scrollWindow("abcdef", 4, offset); got != want {
			t.Errorf("offset %d: got %q, want %q", offset, got, want)
		}
	}
	// A wide character is never cut in half and the width stays exact.
	for offset := 0; offset < 14; offset++ {
		got := scrollWindow("日本語ab", 5, offset)
		if w := lipgloss.Width(got); w != 5 {
			t.Errorf("wide offset %d: %q is %d cells, want 5", offset, got, w)
		}
	}
}

func TestMarqueeOffsetRestsThenScrollsThenLoops(t *testing.T) {
	loop := 10
	if marqueeOffset(0, loop) != 0 || marqueeOffset(marqueePause, loop) != 0 {
		t.Fatal("title should rest at the start first")
	}
	if got := marqueeOffset(marqueePause+3, loop); got != 3 {
		t.Fatalf("offset = %d, want 3", got)
	}
	if got := marqueeOffset(marqueePause+loop, loop); got != 0 {
		t.Fatalf("offset after one loop = %d, want 0", got)
	}
}

func TestLongSelectedTitleScrollsAndShortOnesStay(t *testing.T) {
	m := New("test")
	m.width, m.height = 120, 30
	m.items = []media.Video{
		{ID: "video0001", Title: "Short title", Duration: 60},
		{ID: "video0002", Title: "A very long video title that cannot fit into the narrow results pane at all", Duration: 60},
	}
	m.selected = 1
	m.syncMarquee()
	const width = 40

	rest := m.renderResultLines(width, 6)
	if !strings.Contains(rest, "~") {
		t.Fatalf("a resting long title shows a ~ cut:\n%s", rest)
	}
	m.marqueeStep = marqueePause + 12
	moved := m.renderResultLines(width, 6)
	if moved == rest || strings.Contains(strings.Split(moved, "\n")[2], "~") {
		t.Fatalf("a long selected title should scroll:\nrest:\n%s\nmoved:\n%s", rest, moved)
	}
	for i, row := range strings.Split(moved, "\n") {
		if w := lipgloss.Width(row); w > width {
			t.Errorf("row %d is %d wide, want at most %d", i, w, width)
		}
	}
	if !strings.Contains(moved, "Short title") {
		t.Fatal("a short title must stay as it is")
	}

	// Moving the selection restarts the scroll.
	m.handleKey(runeKey('k'))
	m.syncMarquee()
	if m.marqueeStep != 0 {
		t.Fatalf("step = %d after the selection changed, want 0", m.marqueeStep)
	}
}
