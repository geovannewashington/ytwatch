package tui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/geovannewashington/ytwatch/internal/config"
	"github.com/geovannewashington/ytwatch/internal/history"
	"github.com/geovannewashington/ytwatch/internal/media"
	"github.com/geovannewashington/ytwatch/internal/preview"
	"github.com/geovannewashington/ytwatch/internal/theme"
	"github.com/geovannewashington/ytwatch/internal/youtube"
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

func TestSlashOpensTheCurrentQueryForEditing(t *testing.T) {
	m := New("previous search")
	m.searchMode = false
	m.handleKey(runeKey('/'))

	if !m.searchMode {
		t.Fatal("/ should activate search mode")
	}
	if m.searchInput != "previous search" || m.searchCursor != len("previous search") {
		t.Fatalf("input = %q cursor %d, want the query with the cursor at its end", m.searchInput, m.searchCursor)
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
		for _, want := range []string{"KEYS", "quit", "Vim motions"} {
			if want == "Vim motions" && size[1] < 20 {
				continue // the note is the first thing to go in a short terminal
			}
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
		"110 videos · updated today",
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

func TestThemeConfigAndCycling(t *testing.T) {
	t.Cleanup(func() {
		def, _ := theme.Lookup(theme.Default)
		ApplyTheme(def)
	})
	m := New("test")
	if m.themeName != theme.Default {
		t.Fatalf("default theme = %q", m.themeName)
	}

	cfg := config.Default()
	cfg.Theme = config.Theme{Name: "nord", Colors: map[string]string{"accent": "#112233"}}
	m.SetConfig(cfg)
	if m.themeName != "nord" {
		t.Fatalf("configured theme = %q, want nord", m.themeName)
	}

	m.handleKey(runeKey('t'))
	names := theme.Names()
	want := names[(slices.Index(names, "nord")+1)%len(names)]
	if m.themeName != want || !strings.Contains(m.status, want) {
		t.Fatalf("after t: theme %q status %q, want %q", m.themeName, m.status, want)
	}
}

func linkTestModel(t *testing.T) (*Model, *[]string) {
	t.Helper()
	m := New("test")
	m.width, m.height = 140, 40
	m.items = []media.Video{{
		ID: "video0001", Title: "Song", Channel: "Chan", Duration: 60,
		Description: "Site: http://www.band.com. Cover https://img.example.com/cover.jpg?w=1 and http://www.band.com again",
	}, {ID: "video0002", Title: "No links", Duration: 60, Description: "plain text"}}
	m.metadataDone = true
	var calls []string
	m.openURL = func(url string, _ config.Config) error { calls = append(calls, "browser "+url); return nil }
	m.runCommand = func(argv []string) error { calls = append(calls, strings.Join(argv, " ")); return nil }
	cfg := config.Default()
	cfg.Links = []config.LinkHandler{{
		Name: "images", Match: regexp.MustCompile(`\.(png|jpe?g)(\?.*)?$`), Command: []string{"nsxiv", "%l"},
	}}
	m.SetConfig(cfg)
	return m, &calls
}

func TestPreviewNumbersDescriptionLinks(t *testing.T) {
	m, _ := linkTestModel(t)
	preview := m.renderPreview(100, 30)
	for _, want := range []string{"[1] http://www.band.com.", "[2] https://img.example.com/cover.jpg?w=1", "[1] http://www.band.com again"} {
		if !strings.Contains(preview, want) {
			t.Errorf("preview is missing %q:\n%s", want, preview)
		}
	}
}

func TestOpenKeyOpensTheVideoOrANumberedLink(t *testing.T) {
	m, calls := linkTestModel(t)

	m.handleKey(runeKey('o')) // no count: the video, in the browser
	m.handleKey(runeKey('1'))
	m.handleKey(runeKey('o')) // link 1 has no handler: browser
	m.handleKey(runeKey('2'))
	m.handleKey(runeKey('o')) // link 2 is an image: the handler
	want := []string{
		"browser https://www.youtube.com/watch?v=video0001",
		"browser http://www.band.com",
		"nsxiv https://img.example.com/cover.jpg?w=1",
	}
	if !reflect.DeepEqual(*calls, want) {
		t.Fatalf("calls = %q\nwant   %q", *calls, want)
	}
	if !strings.Contains(m.status, "Opened link 2") || !strings.Contains(m.status, "images") {
		t.Fatalf("status = %q", m.status)
	}
}

func TestLinkNumberOutOfRangeAndNoLinks(t *testing.T) {
	m, calls := linkTestModel(t)
	m.handleKey(runeKey('5'))
	m.handleKey(runeKey('o'))
	if !strings.Contains(m.status, "No link 5") || !strings.Contains(m.status, "2 links") {
		t.Fatalf("status = %q", m.status)
	}
	m.handleKey(runeKey('j')) // the video without links
	m.handleKey(runeKey('1'))
	m.handleKey(runeKey('y'))
	if !strings.Contains(m.status, "has none") {
		t.Fatalf("status = %q", m.status)
	}
	if len(*calls) != 0 {
		t.Fatalf("nothing should have been opened: %q", *calls)
	}
}

func TestFilterMenuAppliesOnEnterAndCancelsOnEsc(t *testing.T) {
	m := New("go tutorial")
	m.items = sampleVideos()
	m.width, m.height = 120, 30

	m.handleKey(runeKey('f'))
	if !m.showFilters {
		t.Fatal("f should open the filter menu")
	}
	// Column 0 is Type; move to Duration (l) and pick the third option (j, j).
	m.handleFilterKey(runeKey('l'))
	m.handleFilterKey(runeKey('j'))
	m.handleFilterKey(runeKey('j'))
	if m.filters.Active() {
		t.Fatal("changes must not apply before Enter")
	}
	if m.filterDraft.Duration != 2 {
		t.Fatalf("draft duration = %d, want 2", m.filterDraft.Duration)
	}

	// Esc discards the draft.
	m.handleFilterKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.showFilters || m.filters.Active() {
		t.Fatal("Esc should close the menu without applying")
	}

	// Enter applies the draft and starts a search with it.
	m.handleKey(runeKey('f'))
	m.handleFilterKey(runeKey('l'))
	m.handleFilterKey(runeKey('l'))
	m.handleFilterKey(runeKey('j')) // Upload date: Today
	cmd := m.handleFilterKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil || m.showFilters || m.filters.Date != 1 {
		t.Fatalf("Enter should apply and search: cmd=%v shown=%v filters=%+v", cmd != nil, m.showFilters, m.filters)
	}
	if !m.searching || m.query != "go tutorial" {
		t.Fatalf("a new search should be running for the same query: searching=%v query=%q", m.searching, m.query)
	}
	m.cancel()

	// x resets the draft.
	m.handleKey(runeKey('f'))
	m.handleFilterKey(runeKey('x'))
	if m.filterDraft.Active() {
		t.Fatal("x should reset the draft")
	}
}

func TestFiltersShowInTheFooterAndKeepAfterNewSearch(t *testing.T) {
	m := New("test")
	m.width, m.height = 120, 30
	m.filters = youtube.Filters{Type: youtube.TypeLive, Date: 2}
	if footer := m.renderFooter(); !strings.Contains(footer, "Live · This week") {
		t.Fatalf("footer should list the filters:\n%s", footer)
	}
	m.query = "x"
	m.beginSearch("another query")
	if m.filters != (youtube.Filters{Type: youtube.TypeLive, Date: 2}) {
		t.Fatal("a new search must keep the filters")
	}
	m.cancel()
}

func TestFilterMenuFitsTheTerminal(t *testing.T) {
	for _, size := range [][2]int{{80, 18}, {100, 30}, {150, 40}} {
		m := New("test")
		m.width, m.height = size[0], size[1]
		m.showFilters = true
		m.filterDraft = youtube.Filters{Type: youtube.TypePlaylists, Duration: 3, Date: 4}
		out := m.View()
		lines := strings.Split(out, "\n")
		if len(lines) != size[1] {
			t.Errorf("%v: menu is %d lines tall, want %d", size, len(lines), size[1])
		}
		for i, line := range lines {
			if w := lipgloss.Width(line); w > size[0] {
				t.Errorf("%v: line %d is %d wide", size, i, w)
			}
		}
		for _, want := range []string{"SEARCH FILTERS", "DURATION", "UPLOAD DATE", "● Playlists", "● Over 20 minutes", "● This year", "What to search for", "Esc cancel"} {
			if !strings.Contains(out, want) {
				t.Errorf("%v: menu is missing %q", size, want)
			}
		}
	}
}

func TestFilterMenuExplainsTheFocusedRow(t *testing.T) {
	m := New("test")
	m.width, m.height = 100, 30
	m.showFilters = true
	m.filterCol = youtube.RowType
	out := m.View()
	for _, want := range []string{"streams happening now"} {
		if !strings.Contains(strings.Join(strings.Fields(out), " "), strings.Join(strings.Fields(want), " ")) {
			t.Errorf("the Live help is missing %q:\n%s", want, out)
		}
	}
}

func TestFilterMenuKeepsItsSizeOnEveryRow(t *testing.T) {
	m := New("test")
	m.width, m.height = 100, 30
	m.showFilters = true
	var height, width int
	for row := range youtube.Rows {
		m.filterCol = row
		out := m.View()
		h := strings.Count(out, "┌") + strings.Count(out, "│")
		w := 0
		for _, line := range strings.Split(out, "\n") {
			if i := strings.Index(line, "┌"); i >= 0 {
				w = lipgloss.Width(line[i:])
			}
		}
		if row == 0 {
			height, width = h, w
		} else if h != height || w != width {
			t.Errorf("row %d: box is %dx%d, row 0 is %dx%d", row, w, h, width, height)
		}
	}
}

func keyOf(s string) tea.KeyMsg {
	switch s {
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "home":
		return tea.KeyMsg{Type: tea.KeyHome}
	case "delete":
		return tea.KeyMsg{Type: tea.KeyDelete}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func TestSearchInputEditsAtTheCursor(t *testing.T) {
	m := New("golang tutorial")
	m.handleKey(runeKey('/'))
	for _, k := range []string{"left", "left", "left", "left", "left", "left", "left", "left", "backspace"} {
		m.handleSearchInput(keyOf(k))
	}
	m.handleSearchInput(keyOf("é"))
	if m.searchInput != "golangétutorial" {
		t.Fatalf("input = %q", m.searchInput)
	}
	m.handleSearchInput(keyOf("home"))
	m.handleSearchInput(keyOf("delete"))
	if m.searchInput != "olangétutorial" || m.searchCursor != 0 {
		t.Fatalf("input = %q cursor %d", m.searchInput, m.searchCursor)
	}
	if line := m.renderHeader(); !strings.Contains(line, "langétutorial") {
		t.Fatalf("header should show the input:\n%s", line)
	}
}

func TestUpAndDownWalkThroughTheSearchHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history")
	now := time.Now()
	data := fmt.Sprintf("%d\talpha\n%d\tbeta\n%d\tgamma\n", now.Add(-3*time.Hour).Unix(), now.Add(-2*time.Hour).Unix(), now.Add(-time.Hour).Unix())
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	m := New("current")
	m.history = history.Load(path, 24*time.Hour, 10, now)
	up, down := tea.KeyMsg{Type: tea.KeyUp}, tea.KeyMsg{Type: tea.KeyDown}

	m.handleKey(runeKey('/'))
	m.handleSearchInput(keyOf("!")) // typed text is kept as a draft
	for _, want := range []string{"gamma", "beta", "alpha", "alpha"} {
		m.handleSearchInput(up)
		if m.searchInput != want {
			t.Fatalf("Up gave %q, want %q", m.searchInput, want)
		}
	}
	for _, want := range []string{"beta", "gamma", "current!", "current!"} {
		m.handleSearchInput(down)
		if m.searchInput != want {
			t.Fatalf("Down gave %q, want %q", m.searchInput, want)
		}
	}
	// A recalled search can be edited before it runs.
	m.handleSearchInput(up)
	m.handleSearchInput(keyOf("2"))
	if m.searchInput != "gamma2" {
		t.Fatalf("input = %q", m.searchInput)
	}
	m.handleSearchInput(keyOf("enter"))
	m.cancel()
	if got := m.history.Queries(); got[len(got)-1] != "gamma2" {
		t.Fatalf("the new search should be saved: %q", got)
	}
}

func TestAPastedLinkOpensTheVideoOrPlaylistWithoutAHistoryEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history")
	m := New("")
	m.history = history.Load(path, 24*time.Hour, 10, time.Now())

	for link, status := range map[string]string{
		"https://youtu.be/dQw4w9WgXcQ":                           "Opening video...",
		"https://www.youtube.com/playlist?list=PL4cUxeGkcC9gC":   "Opening playlist...",
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ&list=PLabc": "Opening video...",
	} {
		if cmd := m.beginSearch(link); cmd == nil {
			t.Fatalf("%s: nothing started", link)
		}
		m.cancel()
		if m.status != status || m.query != link {
			t.Errorf("%s: status = %q, query = %q", link, m.status, m.query)
		}
	}
	if got := m.history.Queries(); len(got) != 0 {
		t.Fatalf("links must not be saved as searches: %q", got)
	}

	m.beginSearch("programming")
	m.cancel()
	if m.status != "Searching..." {
		t.Fatalf("a word that looks like an ID is a search, status = %q", m.status)
	}
}

func TestNoHistoryMeansUpAndDownDoNothing(t *testing.T) {
	m := New("current")
	m.handleKey(runeKey('/'))
	m.handleSearchInput(tea.KeyMsg{Type: tea.KeyUp})
	if m.searchInput != "current" {
		t.Fatalf("input = %q", m.searchInput)
	}
}

func TestQualityMenuPicksAndShowsInTheFooter(t *testing.T) {
	m := New("test")
	m.width, m.height = 100, 30
	if m.quality != "" && m.quality != "best" {
		t.Fatalf("quality = %q", m.quality)
	}
	m.handleKey(runeKey('Q'))
	if !m.showQuality {
		t.Fatal("Q should open the quality menu")
	}
	out := m.View()
	for _, want := range []string{"VIDEO QUALITY", "Best available", "Up to 1080p", "Up to 360p", "Esc cancel"} {
		if !strings.Contains(out, want) {
			t.Errorf("menu is missing %q", want)
		}
	}
	m.handleQualityKey(runeKey('j'))
	m.handleQualityKey(runeKey('j')) // Up to 720p
	m.handleQualityKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.showQuality || m.quality != "720p" {
		t.Fatalf("quality = %q, menu open = %v", m.quality, m.showQuality)
	}
	if !strings.Contains(m.renderFooter(), "720p") {
		t.Fatalf("footer should show the quality: %q", m.renderFooter())
	}
	// Esc leaves it unchanged, and the menu opens on the current level.
	m.handleKey(runeKey('Q'))
	m.handleQualityKey(runeKey('j'))
	m.handleQualityKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.quality != "720p" {
		t.Fatalf("Esc changed the quality to %q", m.quality)
	}
	m.handleKey(runeKey('Q'))
	if m.qualityDraft != 2 {
		t.Fatalf("menu should start on the current level, got %d", m.qualityDraft)
	}
}

func TestResultsAreInsertedByRankAndTheSelectionStays(t *testing.T) {
	m := New("test")
	m.width, m.height = 120, 30
	add := func(id string, rank int) {
		m.handleSearchEvent(youtube.Event{Kind: youtube.EventResult, Video: media.Video{ID: id, Title: id}, Rank: rank})
	}
	// They arrive out of order, as the queries finish at different times.
	add("extra0001", 400)
	add("third0001", 20)
	add("first0001", 0)
	add("second001", 10)
	var ids []string
	for _, v := range m.items {
		ids = append(ids, v.ID)
	}
	if got := strings.Join(ids, ","); got != "first0001,second001,third0001,extra0001" {
		t.Fatalf("order = %s", got)
	}

	// The user moved to "third"; a result that lands above it must not change
	// what is selected.
	m.selected = 2
	add("between01", 15)
	if v, _ := m.selectedVideo(); v.ID != "third0001" {
		t.Fatalf("selection moved to %s", v.ID)
	}
	// One that lands below does not move it either.
	add("last00001", 900)
	if v, _ := m.selectedVideo(); v.ID != "third0001" {
		t.Fatalf("selection moved to %s", v.ID)
	}
	// Equal ranks keep their arrival order.
	add("same00001", 15)
	if m.items[2].ID != "between01" || m.items[3].ID != "same00001" {
		t.Fatalf("equal ranks should keep arrival order: %v", m.items)
	}
}

func TestWrapDescriptionKeepsLineBreaksAndBlankLines(t *testing.T) {
	text := "\n\nFirst line\r\nSecond line is long enough to wrap around\n\n\n\nChapters\n0:00 Intro\n\n"
	got := wrapDescription(text, 20)
	want := []string{
		"First line",
		"Second line is long",
		"enough to wrap",
		"around",
		"",
		"Chapters",
		"0:00 Intro",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
	if got := wrapDescription("   \n\n", 20); len(got) != 0 {
		t.Fatalf("a blank description has no lines: %q", got)
	}
}

func TestDescriptionKeepsItsBlankLinesInThePreview(t *testing.T) {
	m := New("test")
	m.width, m.height = 120, 40
	m.items = []media.Video{{ID: "video0001", Title: "T", Description: "Intro text\n\nSecond paragraph"}}
	out := m.renderPreview(50, 30)
	lines := strings.Split(out, "\n")
	for i, line := range lines {
		if strings.Contains(line, "Intro text") {
			if !strings.Contains(lines[i+2], "Second paragraph") || strings.TrimSpace(lines[i+1]) != "" {
				t.Fatalf("the blank line between paragraphs is lost:\n%s", strings.Join(lines[i:i+3], "\n"))
			}
			return
		}
	}
	t.Fatalf("description not found:\n%s", out)
}

func playlistVideo() media.Video {
	return media.Video{ID: "PLplaylist0001", Title: "My playlist", URL: "https://www.youtube.com/playlist?list=PLplaylist0001", PlaylistCount: 12}
}

func TestBOpensAPlaylistAndEscComesBack(t *testing.T) {
	m := New("test")
	m.width, m.height = 120, 30
	m.items = []media.Video{{ID: "video0001", Title: "One"}, playlistVideo(), {ID: "video0003", Title: "Three"}}
	m.ranks = []int{0, 10, 20}
	m.selected, m.top = 1, 0
	m.searchDone, m.metadataDone = true, true

	cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'B'}})
	if cmd == nil || len(m.parents) != 1 || m.listTitle != "My playlist" {
		t.Fatalf("B should open the playlist: cmd=%v parents=%d title=%q", cmd != nil, len(m.parents), m.listTitle)
	}
	if len(m.items) != 0 || !m.searching {
		t.Fatalf("the list should be empty while the playlist loads: %d items, searching=%v", len(m.items), m.searching)
	}
	if !strings.Contains(m.renderHeader(), "My playlist") || !strings.Contains(m.renderHeader(), "test") {
		t.Fatalf("the header should show where you are:\n%s", m.renderHeader())
	}
	if !strings.Contains(m.renderHeader(), "Esc back") {
		t.Fatalf("the header should say how to go back:\n%s", m.renderHeader())
	}
	// Videos of the playlist arrive.
	m.handleSearchEvent(youtube.Event{Kind: youtube.EventResult, Video: media.Video{ID: "inside0001", Title: "Inside"}, Rank: 0})
	if len(m.items) != 1 {
		t.Fatal("the video should be listed")
	}
	// Enter on a video plays it and does not open anything.
	generation := m.generation

	m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if len(m.parents) != 0 || m.listTitle != "" || len(m.items) != 3 || m.selected != 1 {
		t.Fatalf("Esc should restore the results: parents=%d title=%q items=%d selected=%d", len(m.parents), m.listTitle, len(m.items), m.selected)
	}
	if m.generation == generation {
		t.Fatal("events of the closed playlist must be ignored")
	}
	if m.searching || m.status != "3 results" {
		t.Fatalf("searching=%v status=%q", m.searching, m.status)
	}
	// Esc with nothing open does nothing special.
	m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if len(m.items) != 3 {
		t.Fatal("Esc must not clear the results")
	}
	m.cancel()
}

func TestEscInVisualModeEndsTheSelectionBeforeLeavingAPlaylist(t *testing.T) {
	m := New("test")
	m.items = []media.Video{{ID: "video0001", Title: "One"}}
	m.parents = []resultsState{{items: []media.Video{playlistVideo()}, ranks: []int{0}}}
	m.listTitle = "My playlist"
	m.handleKey(runeKey('V'))
	m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.visual || len(m.parents) != 1 {
		t.Fatalf("the first Esc should only end the selection: visual=%v parents=%d", m.visual, len(m.parents))
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if len(m.parents) != 0 {
		t.Fatal("the second Esc should leave the playlist")
	}
}

func TestANewSearchLeavesTheOpenPlaylist(t *testing.T) {
	m := New("test")
	m.parents = []resultsState{{}}
	m.listTitle = "My playlist"
	m.beginSearch("something else")
	m.cancel()
	if len(m.parents) != 0 || m.listTitle != "" {
		t.Fatal("a new search should leave the playlist")
	}
}

// The help lists one key for each action. The extra ways that exist are not
// documented, so the screen stays short.
func TestHelpIsShortAndHasNoDuplicateKeys(t *testing.T) {
	m := New("test")
	m.width, m.height = 100, 30
	m.showHelp = true
	out := m.View()
	rows := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(strings.Trim(line, "│┌┐└┘─ ")) != "" {
			rows++
		}
	}
	if rows > 16 {
		t.Errorf("the help has %d lines of text, keep it short", rows)
	}
	for _, duplicate := range []string{"w / b", "n / N", "PageUp", "Ctrl+f", "0 / $", "H / M / L"} {
		if strings.Contains(out, duplicate) {
			t.Errorf("the help should not document %q", duplicate)
		}
	}
}

func TestResultsInsertedAboveAKeepTheSelectionAndTheScrollSteady(t *testing.T) {
	m := New("test")
	m.width, m.height = 120, 30
	for i := 0; i < 20; i++ {
		id := fmt.Sprintf("video%04d", i)
		m.items = append(m.items, media.Video{ID: id, Title: id})
		m.ranks = append(m.ranks, (i+1)*10)
	}
	page := m.pageSize()
	// The cursor is on the last visible row of a scrolled list.
	m.top, m.selected = 5, 5+page-1
	want := m.items[m.selected].ID
	m.insertResult(media.Video{ID: "newtop0001", Title: "new"}, 0)
	if v, _ := m.selectedVideo(); v.ID != want {
		t.Fatalf("the selection moved to %s, want %s", v.ID, want)
	}
	if m.selected < m.top || m.selected >= m.top+page {
		t.Fatalf("the cursor left the window: selected %d, top %d, page %d", m.selected, m.top, page)
	}
	// The rows in view are the same ones as before.
	if m.items[m.top].ID != "video0005" && m.items[m.top].ID != "video0006" {
		t.Fatalf("the window jumped, first row is %s", m.items[m.top].ID)
	}
}

func TestAResultBetweenTheCursorAndTheAnchorKeepsTheSelection(t *testing.T) {
	m := New("test")
	m.width, m.height = 120, 30
	for i := 0; i < 8; i++ {
		id := fmt.Sprintf("video%04d", i)
		m.items = append(m.items, media.Video{ID: id, Title: id})
		m.ranks = append(m.ranks, (i+1)*10)
	}
	// V on row 6, then up to row 1: the selection is rows 1 to 6.
	m.visual, m.visualAnchor, m.selected = true, 6, 1
	m.insertResult(media.Video{ID: "between001", Title: "x"}, 35) // lands at index 3
	lo, hi := m.selection()
	if m.items[lo].ID != "video0001" || m.items[hi].ID != "video0006" {
		t.Fatalf("the selection now spans %s to %s, want video0001 to video0006", m.items[lo].ID, m.items[hi].ID)
	}
	// A better first result while the cursor is on row 0 and the anchor lower.
	m.selected, m.visualAnchor = 0, 4
	anchorID := m.items[4].ID
	m.insertResult(media.Video{ID: "first00001", Title: "y"}, 0)
	if m.items[m.visualAnchor].ID != anchorID {
		t.Fatalf("the anchor moved to %s, want %s", m.items[m.visualAnchor].ID, anchorID)
	}
}

func TestClosingAPlaylistTellsWhenTheSearchWasStoppedEarly(t *testing.T) {
	m := New("test")
	m.items = []media.Video{playlistVideo()}
	m.ranks = []int{0}
	m.searching = true // results were still arriving
	m.openPlaylist(playlistVideo())
	m.closePlaylist()
	m.cancel()
	if !strings.Contains(m.status, "stopped early") {
		t.Fatalf("status = %q", m.status)
	}
}

func TestInputCursorStaysWhereTheTextIsWithRemovedCharacters(t *testing.T) {
	// The emoji is not printed, so the cursor after "a🎉" is after "a".
	before, under, after := inputParts("a🎉b", 2)
	if string(before) != "a" || string(under) != "b" || string(after) != "" {
		t.Fatalf("parts = %q %q %q", string(before), string(under), string(after))
	}
	before, under, after = inputParts("go 🎉 lang", 3)
	if string(before) != "go " || string(under) != "" || string(after) != " lang" {
		t.Fatalf("parts = %q %q %q", string(before), string(under), string(after))
	}
	if got := renderInput("abc", 99, 20); !strings.HasPrefix(got, "abc") {
		t.Fatalf("a cursor past the end goes to the end: %q", got)
	}
}

func TestFilterMenuExplainsThatShortsIgnoreTheDate(t *testing.T) {
	m := New("test")
	m.width, m.height = 100, 30
	m.showFilters = true
	m.filterDraft = youtube.Filters{Type: youtube.TypeShorts, Date: 2}
	m.filterCol = youtube.RowDate
	if out := m.View(); !strings.Contains(out, "no date filter for Shorts") {
		t.Fatalf("the menu should say why the date does nothing:\n%s", out)
	}
	m.filterDraft.Type = youtube.TypeVideos
	if out := m.View(); strings.Contains(out, "no date filter") {
		t.Fatal("the note belongs only to Shorts")
	}
}

func TestALeadingSlashIsNotPartOfTheSearch(t *testing.T) {
	// ytwatch starts with the search open. Typing / there by habit is ignored.
	m := New("")
	m.handleSearchInput(keyOf("/"))
	if m.searchInput != "" {
		t.Fatalf("input = %q, the first / should be ignored", m.searchInput)
	}
	for _, r := range "the primetime" {
		m.handleSearchInput(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m.handleSearchInput(keyOf("/")) // a / in the middle of a query stays
	if m.searchInput != "the primetime/" {
		t.Fatalf("input = %q", m.searchInput)
	}
	// A query that still has one (from an old history, or pasted) loses it.
	m.beginSearch("//the primetime, nobody is readin anything")
	m.cancel()
	if m.query != "the primetime, nobody is readin anything" {
		t.Fatalf("query = %q", m.query)
	}
}

func TestWideTerminalKeepsTheLayoutWidthAndCentersIt(t *testing.T) {
	m := New("test")
	m.Update(tea.WindowSizeMsg{Width: 300, Height: 40})
	if m.width != maxWidth || m.margin != (300-maxWidth)/2 {
		t.Fatalf("width=%d margin=%d", m.width, m.margin)
	}
	for _, line := range strings.Split(m.View(), "\n") {
		if !strings.HasPrefix(line, strings.Repeat(" ", m.margin)) {
			t.Fatalf("line is not indented by the margin: %q", line)
		}
	}
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	if m.width != 120 || m.margin != 0 {
		t.Fatalf("a terminal under the limit has no margin: width=%d margin=%d", m.width, m.margin)
	}
}

func TestStartScreenHasOnePaneAndSaysTypeToSearch(t *testing.T) {
	m := New("")
	m.width, m.height = 120, 30
	out := m.View()
	if strings.Contains(out, "PREVIEW") {
		t.Fatalf("the start screen should not show a preview pane:\n%s", out)
	}
	if !strings.Contains(out, "Type your search and press Enter") {
		t.Fatalf("the start screen should say to type:\n%s", out)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if out := m.View(); !strings.Contains(out, "Press / to search") {
		t.Fatalf("after Esc the screen should say to press /:\n%s", out)
	}
}

func TestKeyHintsFollowTheSelection(t *testing.T) {
	m := New("test")
	m.items = []media.Video{{ID: "video0001", Title: "One"}, playlistVideo()}
	m.ranks = []int{0, 10}
	if hints := m.keyHints(); !strings.Contains(hints, "d download") || strings.Contains(hints, "B browse") {
		t.Fatalf("video hints: %q", hints)
	}
	m.selected = 1
	if hints := m.keyHints(); !strings.Contains(hints, "Enter play all") || !strings.Contains(hints, "B browse") {
		t.Fatalf("playlist hints: %q", hints)
	}
	m.startVisual()
	if hints := m.keyHints(); !strings.Contains(hints, "Esc cancel") {
		t.Fatalf("visual hints: %q", hints)
	}
}

func TestDurationIsLeftOutUntilDetailsArrive(t *testing.T) {
	if _, ok := durationPart(media.Video{ID: "video0001"}); ok {
		t.Error("a result without details should not claim an unknown duration")
	}
	if text, ok := durationPart(media.Video{ID: "video0001", Detailed: true}); !ok || text != "duration unknown" {
		t.Errorf("a detailed result with no length: %q %v", text, ok)
	}
	if text, ok := durationPart(media.Video{ID: "video0001", Duration: 65}); !ok || text != "1:05" {
		t.Errorf("a known length: %q %v", text, ok)
	}
}

func TestSanitizeDropsCombiningMarks(t *testing.T) {
	cases := map[string]string{
		"গো বাংলা":   "গ বল",
		"plain text": "plain text",
		"é":         "e",
	}
	for in, want := range cases {
		if got := sanitize(in); got != want {
			t.Errorf("sanitize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStatusNoteIsSeparatedByADot(t *testing.T) {
	got := renderStatus("Saved"+noteMark+"/tmp/a.mp4", statusStyle, 40)
	if !strings.Contains(got, "Saved") || !strings.Contains(got, " · /tmp/a.mp4") {
		t.Errorf("the note should follow a dot, got %q", got)
	}
}

func TestBlurHidesTheThumbnailUntilFocusReturns(t *testing.T) {
	m := New("")
	m.hideOnBlur = true
	m.Update(tea.BlurMsg{})
	if !m.blurred {
		t.Fatal("a blur should mark the model as blurred")
	}
	seq := m.settleSeq
	m.Update(tea.FocusMsg{})
	if m.blurred || m.settleSeq == seq {
		t.Error("focus should clear the blur and schedule a redraw")
	}
}

func TestBlurIsIgnoredOutsideTmux(t *testing.T) {
	m := New("")
	m.hideOnBlur = false
	m.Update(tea.BlurMsg{})
	if m.blurred {
		t.Error("outside tmux the image stays when the terminal loses focus")
	}
}

func prefetchModel(n, selected int) *Model {
	m := New("")
	for i := 0; i < n; i++ {
		m.items = append(m.items, media.Video{ID: fmt.Sprintf("vid%05d", i)})
	}
	m.selected = selected
	return m
}

func targetIDs(m *Model) []string {
	var ids []string
	for _, v := range m.prefetchTargets() {
		ids = append(ids, v.ID)
	}
	return ids
}

func TestPrefetchTargetsAreTheNeighborsNearestFirst(t *testing.T) {
	m := prefetchModel(20, 10)
	got := strings.Join(targetIDs(m), " ")
	want := "vid00011 vid00009 vid00012 vid00008 vid00013 vid00014 vid00015"
	if got != want {
		t.Errorf("targets = %s\nwant      %s", got, want)
	}
}

func TestPrefetchTargetsStayInsideTheListAndSkipKnownRows(t *testing.T) {
	m := prefetchModel(4, 0)
	m.thumbnailPaths["vid00001"] = "/cache/a.jpg"       // already in memory
	m.prefetchTried = map[string]bool{"vid00002": true} // already asked for
	m.thumbFailures["vid00003"] = time.Now()            // failed recently
	if got := targetIDs(m); len(got) != 0 {
		t.Errorf("nothing is left to prefetch, got %v", got)
	}
	m = prefetchModel(3, 2)
	if got := strings.Join(targetIDs(m), " "); got != "vid00001 vid00000" {
		t.Errorf("at the end of the list, got %s", got)
	}
}

func TestNewSearchClearsFiltersButRepeatingKeepsThem(t *testing.T) {
	m := New("")
	m.query = "old"
	m.searchMode = true
	m.setInput("new query")
	m.filters = youtube.Filters{Type: youtube.TypeLive}
	m.handleSearchInput(tea.KeyMsg{Type: tea.KeyEnter})
	if m.filters.Active() {
		t.Errorf("a new search should clear the filters, got %+v", m.filters)
	}
	m.Close()

	m = New("")
	m.query = "old"
	m.filters = youtube.Filters{Type: youtube.TypeLive}
	m.beginSearch(m.query) // what r and applying the filter menu do
	if !m.filters.Active() {
		t.Error("repeating the search should keep the filters")
	}
	m.Close()
}

func TestOnlyOldThumbnailsOfStreamsAreStale(t *testing.T) {
	m := New("")
	live := media.Video{ID: "live0000001", LiveStatus: "is_live"}
	recording := media.Video{ID: "vod00000001"}
	m.setThumbnail(live.ID, "/cache/live.jpg")
	m.setThumbnail(recording.ID, "/cache/vod.jpg")
	if m.thumbStale(live) {
		t.Error("a thumbnail just downloaded is not stale")
	}
	m.thumbLoadedAt[live.ID] = time.Now().Add(-2 * preview.LiveTTL)
	m.thumbLoadedAt[recording.ID] = time.Now().Add(-2 * preview.LiveTTL)
	if !m.thumbStale(live) {
		t.Error("an old thumbnail of a stream on air is stale")
	}
	if m.thumbStale(recording) {
		t.Error("the thumbnail of a recording never goes stale")
	}
}

func TestSearchResultsAreCappedAtTheLimitKeepingTheBestRanked(t *testing.T) {
	m := New("")
	// The worst results arrive first, as they can when queries finish in any order.
	for i := youtube.ResultLimit + 9; i >= 0; i-- {
		m.insertResult(media.Video{ID: fmt.Sprintf("vid%05d", i)}, i*10)
	}
	if len(m.items) != youtube.ResultLimit || len(m.ranks) != youtube.ResultLimit {
		t.Fatalf("got %d items and %d ranks, want %d", len(m.items), len(m.ranks), youtube.ResultLimit)
	}
	if m.items[0].ID != "vid00000" || m.items[len(m.items)-1].ID != fmt.Sprintf("vid%05d", youtube.ResultLimit-1) {
		t.Errorf("the best ranked results should stay, got first %s last %s", m.items[0].ID, m.items[len(m.items)-1].ID)
	}
	m.insertResult(media.Video{ID: "late0000001"}, 1<<20) // worse than all of them
	if len(m.items) != youtube.ResultLimit {
		t.Errorf("a result ranked beyond the limit must be ignored, got %d items", len(m.items))
	}
	m.selected = len(m.items) - 1
	m.insertResult(media.Video{ID: "best0000001"}, -1)
	if m.selected >= len(m.items) {
		t.Errorf("the selection must stay inside the list, got %d of %d", m.selected, len(m.items))
	}
}

func TestPlaylistListsAreNotCapped(t *testing.T) {
	m := New("")
	m.parents = []resultsState{{}} // inside a playlist
	for i := 0; i < youtube.ResultLimit+20; i++ {
		m.insertResult(media.Video{ID: fmt.Sprintf("vid%05d", i)}, i*10)
	}
	if len(m.items) != youtube.ResultLimit+20 {
		t.Errorf("a playlist keeps all its videos, got %d", len(m.items))
	}
}

func detailModel(n, selected int) *Model {
	m := prefetchModel(n, selected)
	m.metadataDone = true

	return m
}

func detailTargetIDs(m *Model) string {
	var ids []string
	for _, v := range m.detailTargets() {
		ids = append(ids, v.ID)
	}

	return strings.Join(ids, " ")
}

func TestDetailTargetsAreTheSelectedRowThenItsNeighbors(t *testing.T) {
	m := detailModel(20, 10)

	want := "vid00010 vid00011 vid00009 vid00012 vid00008 vid00013 vid00014 vid00015"
	if got := detailTargetIDs(m); got != want {
		t.Errorf("targets = %s\nwant      %s", got, want)
	}

	m.items[10].Detailed = true
	m.detailsAsked = map[string]bool{"vid00012": true}
	m.items[13].Detailed = true

	want = "vid00011 vid00009 vid00008 vid00014 vid00015"
	if got := detailTargetIDs(m); got != want {
		t.Errorf("rows with details or asked for are left out:\ngot  %s\nwant %s", got, want)
	}
}

func TestDetailTargetsLookUpOnlyTheSelectedPlaylist(t *testing.T) {
	m := detailModel(6, 2)
	for _, i := range []int{1, 2, 3} {
		m.items[i].WebpageURL = "https://www.youtube.com/playlist?list=PL12345678"
	}

	got := detailTargetIDs(m)
	if !strings.Contains(got, "vid00002") {
		t.Errorf("the selected playlist should be looked up, got %s", got)
	}
	if strings.Contains(got, "vid00001") || strings.Contains(got, "vid00003") {
		t.Errorf("neighbor playlists cost a yt-dlp run each and must wait, got %s", got)
	}
}

func TestNoDetailLookupBeforeTheSearchEndsOrWithoutAProgram(t *testing.T) {
	m := detailModel(5, 0)
	m.send = func(tea.Msg) {}

	m.metadataDone = false
	if m.lookUpDetails() != nil {
		t.Error("lookups wait for the search to finish")
	}

	m.metadataDone = true
	m.send = nil
	if m.lookUpDetails() != nil {
		t.Error("without a program to send messages to there is nothing to do")
	}

	m.send = func(tea.Msg) {}
	if m.lookUpDetails() == nil {
		t.Error("with a finished search and rows without details, a lookup should start")
	}
	if !m.detailsAsked["vid00000"] {
		t.Error("the rows asked for must be remembered, so they are not asked again")
	}
	m.Close()
}

func TestPreviewSaysDetailsAreLoadingUntilTheyArriveOrFail(t *testing.T) {
	m := detailModel(3, 0)
	m.width, m.height = 140, 40

	render := func() string { return m.renderPreview(60, 30) }

	if out := render(); !strings.Contains(out, "Loading details...") {
		t.Errorf("a row without details should say they are loading:\n%s", out)
	}

	m.detailsFailed = map[string]bool{"vid00000": true}
	if out := render(); strings.Contains(out, "Loading details...") || !strings.Contains(out, "No description") {
		t.Errorf("after a failed lookup the preview should stop waiting:\n%s", out)
	}

	m.items[0].Detailed = true
	m.items[0].Description = "A real description."
	if out := render(); !strings.Contains(out, "A real description.") {
		t.Errorf("the description should show once it is known:\n%s", out)
	}
}

// Titles, channel names and descriptions come from the network. The tests run
// with colors off, so any escape or bell byte in the screen came from them.
func TestTextFromYouTubeCannotInjectTerminalCodes(t *testing.T) {
	evil := "x\x1b]0;pwned\x07\x1b[2J\x1b[31m\u009b31m\x07y"

	m := New(evil) // the query given on the command line
	m.width, m.height = 140, 40
	m.searchMode = false
	m.searchDone, m.metadataDone = true, true

	m.items = []media.Video{
		{
			ID: "vid00000001", Title: evil, Channel: evil, Detailed: true,
			Description: evil + "\n" + evil, Duration: 60, ViewCount: 5,
		},
		{
			ID: "PL12345678", Title: evil, Channel: evil, WebpageURL: "https://www.youtube.com/playlist?list=PL12345678",
			Entries: []media.PlaylistEntry{{Title: evil}},
		},
	}
	m.ranks = []int{0, 10}

	for _, selected := range []int{0, 1} {
		m.selected = selected

		out := m.View()
		if strings.ContainsAny(out, "\x1b\x07\u009b") {
			t.Errorf("row %d: a terminal control code reached the screen: %q", selected, out)
		}
	}
}

// Text typed, pasted or read from the history file goes through the search line.
func TestSearchLineCannotInjectTerminalCodes(t *testing.T) {
	evil := "x\x1b]0;pwned\x07\x1b[2J\u009b31m\ny"

	m := New("")
	m.width, m.height = 140, 40

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(evil), Paste: true})
	if strings.ContainsAny(m.searchInput, "\x1b\x07\u009b\n") {
		t.Errorf("a pasted control code is in the search line: %q", m.searchInput)
	}
	if !strings.HasPrefix(m.searchInput, "x") || !strings.HasSuffix(m.searchInput, "y") {
		t.Errorf("the text around the control codes should stay: %q", m.searchInput)
	}

	m.setInput(evil) // a line of the history file, or the current query
	if strings.ContainsAny(m.searchInput, "\x1b\x07\u009b\n") {
		t.Errorf("a control code from setInput is in the search line: %q", m.searchInput)
	}

	if out := m.View(); strings.ContainsAny(out, "\x1b\x07\u009b") {
		t.Errorf("a terminal control code reached the screen: %q", out)
	}

	if got := stripControls("a\tb\nc"); got != "a b c" {
		t.Errorf("tabs and line breaks become spaces, got %q", got)
	}
}

// nativeModel is a model with two results, a PNG thumbnail for the first, and
// the given native mode.
func nativeModel(t *testing.T, mode preview.Mode, kitty *preview.Kitty) (*Model, string) {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, 64, 36))
	for y := range 36 {
		for x := range 64 {
			img.Set(x, y, color.RGBA{200, uint8(4 * y), 30, 255})
		}
	}

	path := filepath.Join(t.TempDir(), "thumb.png")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(file, img); err != nil {
		t.Fatal(err)
	}
	file.Close()

	m := prefetchModel(2, 0)
	m.output = preview.Output{Mode: mode}
	m.kitty = kitty
	m.thumbnailPaths["vid00000"] = path

	return m, path
}

func previewWidth(m *Model) int {
	_, width := m.paneWidths()
	return width
}

func TestHalfBlockThumbnailFillsTheReservedRows(t *testing.T) {
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(previous)

	m, _ := nativeModel(t, preview.ModeHalfBlocks, nil)
	if !m.thumbnailsOn() {
		t.Fatal("half-blocks need no program")
	}

	m.showSelectedThumbnail()
	out := m.renderPreview(previewWidth(m), 30)

	if !strings.Contains(out, "▀") || strings.Contains(out, "install ueberzugpp") {
		t.Errorf("the preview should hold half-blocks and no install note:\n%s", out)
	}

	// The reserved rows keep the text below at the same place with or without
	// the image.
	lines := strings.Split(out, "\n")
	m.selected = 1
	other := strings.Split(m.renderPreview(previewWidth(m), 30), "\n")
	if strings.Contains(strings.Join(other, "\n"), "▀") {
		t.Error("another result must not show the first one's thumbnail")
	}
	if len(lines) < m.thumbRows()+1 || len(other) != len(lines) {
		t.Errorf("the thumbnail rows are not reserved: %d and %d lines", len(lines), len(other))
	}
}

func TestKittyThumbnailIsSentOnceOutsideView(t *testing.T) {
	var sent bytes.Buffer
	k := preview.NewKitty(&sent, false)

	m, _ := nativeModel(t, preview.ModeKitty, k)
	m.showSelectedThumbnail()
	if !strings.Contains(sent.String(), "\x1b_Ga=T,U=1,f=100,t=d,i=42") {
		t.Fatalf("the image should be sent when it is shown: %.60q", sent.String())
	}

	size := sent.Len()
	out := m.renderPreview(previewWidth(m), 30)
	m.showSelectedThumbnail()
	m.renderPreview(previewWidth(m), 30)

	if sent.Len() != size {
		t.Error("drawing the preview or showing the same thumbnail again must not resend the image")
	}
	if !strings.Contains(out, "\U0010EEEE") {
		t.Errorf("the preview should hold placeholders:\n%s", out)
	}
	for i, line := range strings.Split(out, "\n") {
		if w := lipgloss.Width(line); w > previewWidth(m) {
			t.Errorf("line %d is %d columns wide, the preview is %d", i, w, previewWidth(m))
		}
	}

	m.Close()
	if !strings.HasSuffix(sent.String(), "a=d,d=I,i=42,q=2\x1b\\") {
		t.Error("closing the model should delete the image")
	}
}

func TestNativeModesIgnoreBlurAndNeedNoSettling(t *testing.T) {
	for _, mode := range []preview.Mode{preview.ModeKitty, preview.ModeHalfBlocks} {
		m := New("")
		m.output = preview.Output{Mode: mode}
		m.hideOnBlur = true

		m.Update(tea.BlurMsg{})
		if m.blurred {
			t.Errorf("mode %d: nothing is hidden on blur, the image is part of the screen", mode)
		}
		if cmd := m.settleThumbnail(); cmd != nil {
			t.Errorf("mode %d: nothing needs to be redrawn after a layout change", mode)
		}
	}
}

func TestThumbnailsOffReservesNoRowsAndDrawsNothing(t *testing.T) {
	m, _ := nativeModel(t, preview.ModeOff, nil)
	m.showSelectedThumbnail()

	if m.thumbnailsOn() || m.thumbRows() != 0 {
		t.Error("off means no thumbnails and no reserved rows")
	}
	if strings.Contains(m.renderPreview(previewWidth(m), 30), "▀") {
		t.Error("off draws nothing")
	}
}

func TestSetConfigPicksTheRendererFromTheSetting(t *testing.T) {
	m := New("")
	cfg := config.Default()

	cfg.Preview.Output = "halfblocks"
	m.SetConfig(cfg)
	if m.output.Mode != preview.ModeHalfBlocks {
		t.Errorf("halfblocks: %+v", m.output)
	}

	cfg.Preview.Output = "kitty"
	m.SetConfig(cfg)
	if m.output.Mode != preview.ModeKitty {
		t.Errorf("kitty: %+v", m.output)
	}

	cfg.Preview.Output = "off"
	m.SetConfig(cfg)
	if m.output.Mode != preview.ModeOff {
		t.Errorf("off: %+v", m.output)
	}
}

func TestChannelTextShowsTheMarkOfTheChannel(t *testing.T) {
	for name, tc := range map[string]struct {
		video media.Video
		want  string
	}{
		"plain":    {media.Video{Channel: "Some Channel"}, "Some Channel"},
		"verified": {media.Video{Channel: "Some Channel", ChannelVerified: true}, "Some Channel \uf058"},
		"artist":   {media.Video{Channel: "Taylor Swift", ChannelVerified: true, ChannelArtist: true}, "Taylor Swift \uf001"},
		"unknown":  {media.Video{ChannelVerified: true}, "Unknown channel \uf058"},
	} {
		if got := channelText(tc.video); got != tc.want {
			t.Errorf("%s: channelText = %q, want %q", name, got, tc.want)
		}
	}
}

const (
	testChannelID  = "UCZwW2PXNovXy_xUIhmYv7cg"
	testChannelURL = "https://www.youtube.com/channel/" + testChannelID
)

func channelResult() media.Video {
	return media.Video{
		ID:              "UCZwW2PXNovXy_xUIhmYv7cg",
		Title:           "Tiago",
		URL:             testChannelURL,
		UploaderID:      "@TiagoTaquelim",
		SubscriberCount: 46300,
	}
}

func TestChannelRowAndPreviewShowTheHandleAndSubscribers(t *testing.T) {
	m := New("test")
	m.width, m.height = 120, 30
	m.items = []media.Video{channelResult()}
	m.ranks = []int{0}

	if label, _ := badgeInfo(channelResult()); label != "CHANNEL" {
		t.Errorf("badge = %q", label)
	}
	rows := m.renderResultLines(60, 10)
	preview := m.renderPreview(50, 20)
	for _, want := range []string{"Tiago", "@TiagoTaquelim", "46.3K subscribers", "CHANNEL"} {
		if !strings.Contains(rows+preview, want) {
			t.Errorf("missing %q:\n%s\n%s", want, rows, preview)
		}
	}
	if strings.Contains(preview, "Loading details") || len(m.detailTargets()) != 0 {
		t.Error("a channel has no details to look up")
	}
	if hint := m.keyHints(); !strings.Contains(hint, "Enter browse channel") {
		t.Errorf("hint = %q", hint)
	}
}

func TestCOpensTheChannelOfTheSelectedVideo(t *testing.T) {
	m := New("test")
	m.width, m.height = 120, 30
	video := media.Video{ID: "video0001", Title: "One", Channel: "The X", ChannelURL: "https://www.youtube.com/@x"}
	m.items = []media.Video{video, {ID: "video0002", Title: "Two"}}
	m.ranks = []int{0, 10}
	m.searchDone, m.metadataDone = true, true

	cmd := m.handleKey(runeKey('c'))
	if cmd == nil || len(m.parents) != 1 || m.channel == nil {
		t.Fatalf("c should open the channel: cmd=%v parents=%d", cmd != nil, len(m.parents))
	}
	if m.channel.url != "https://www.youtube.com/@x" || m.channel.name != "The X" || m.channel.tab != youtube.ChannelVideos {
		t.Fatalf("channel = %+v", *m.channel)
	}
	if len(m.items) != 0 || !m.searching || m.status != "Opening videos..." {
		t.Fatalf("the list should load: %d items, searching=%v, status=%q", len(m.items), m.searching, m.status)
	}
	screen := m.renderHeader() + m.renderResultLines(100, 20)
	for _, want := range []string{"@x", "[1 Videos]", "2 Shorts", "3 Live", "4 Podcasts", "5 Playlists"} {
		if !strings.Contains(screen, want) {
			t.Errorf("the screen should show %q:\n%s", want, screen)
		}
	}
	m.cancel()

	// A video that does not say its channel has none to open.
	m2 := New("test")
	m2.items = []media.Video{{ID: "video0002", Title: "Two"}}
	if cmd := m2.handleKey(runeKey('c')); cmd != nil || m2.status != "No channel link for this video" || m2.channel != nil {
		t.Fatalf("cmd=%v status=%q", cmd != nil, m2.status)
	}

	// The channel ID is enough.
	m3 := New("test")
	m3.items = []media.Video{{ID: "video0003", ChannelID: "UCZwW2PXNovXy_xUIhmYv7cg"}}
	m3.handleKey(runeKey('c'))
	m3.cancel()
	if m3.channel == nil || m3.channel.url != testChannelURL {
		t.Fatalf("channel = %+v", m3.channel)
	}
}

func TestEnterAndBOpenAChannelRowAndPlayAndDownloadRefuseIt(t *testing.T) {
	for _, key := range []tea.KeyMsg{{Type: tea.KeyEnter}, runeKey('B'), runeKey('c'), runeKey('s')} {
		m := New("test")
		m.items = []media.Video{channelResult()}
		m.ranks = []int{0}
		if cmd := m.handleKey(key); cmd == nil || m.channel == nil || m.channel.name != "Tiago" || m.channel.url != testChannelURL {
			t.Errorf("%v should open the channel: %+v", key, m.channel)
		}
		m.cancel()
	}

	m := New("test")
	m.items = []media.Video{channelResult(), {ID: "video0001", URL: "https://www.youtube.com/watch?v=video0001"}}
	if cmd := m.handleKey(runeKey('d')); cmd != nil || !strings.Contains(m.status, "Channels can't be downloaded") {
		t.Errorf("status = %q", m.status)
	}
	m.handleKey(runeKey('V'))
	m.selected = 0
	m.visualAnchor = 0
	if videos, skip := m.selectedVideos(false); len(videos) != 0 || skip.channels != 1 || skip.text() != "skipped 1 channel" {
		t.Errorf("a selection leaves channels out: %v %+v", videos, skip)
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !strings.Contains(m.status, "Nothing to play") || !strings.Contains(m.status, "1 channel") {
		t.Errorf("status = %q", m.status)
	}
}

func TestTabSwitchesTheChannelTabAndEscComesBack(t *testing.T) {
	m := New("test")
	m.width, m.height = 120, 30
	results := []media.Video{{ID: "video0001", Title: "One", ChannelURL: "https://www.youtube.com/@x"}, playlistVideo()}
	m.items, m.ranks = results, []int{0, 10}
	m.selected = 1
	m.query = "golang"
	m.searchDone, m.metadataDone = true, true

	m.selected = 0
	m.handleKey(runeKey('c'))
	m.handleSearchEvent(youtube.Event{Kind: youtube.EventResult, Video: media.Video{ID: "inside0001", Title: "Inside"}, Rank: 0})
	if len(m.items) != 1 || len(m.parents) != 1 {
		t.Fatalf("items=%d parents=%d", len(m.items), len(m.parents))
	}

	generation := m.generation
	if cmd := m.handleKey(tabKey); cmd == nil || m.channel.tab != youtube.ChannelShorts {
		t.Fatalf("Tab should go to Shorts: %+v", m.channel)
	}
	if len(m.parents) != 1 || len(m.items) != 0 || m.generation == generation || m.status != "Opening shorts..." {
		t.Fatalf("a tab replaces the list without a new parent: parents=%d items=%d status=%q", len(m.parents), len(m.items), m.status)
	}
	if bar := m.renderResultLines(60, 20); !strings.Contains(bar, "[2 Shorts]") {
		t.Errorf("the current tab is marked:\n%s", bar)
	}
	m.handleKey(tabKey)
	m.handleKey(tabKey)
	m.handleKey(tabKey)
	if m.channel.tab != youtube.ChannelPlaylists {
		t.Fatalf("tab = %d", m.channel.tab)
	}
	m.handleKey(tabKey) // wraps around
	if m.channel.tab != youtube.ChannelVideos {
		t.Fatalf("Tab after the last tab = %d, want the first", m.channel.tab)
	}
	m.handleKey(shiftTabKey)
	if m.channel.tab != youtube.ChannelPlaylists {
		t.Fatalf("Shift-Tab on the first tab = %d, want the last", m.channel.tab)
	}

	// A playlist of the channel opens inside it, and Esc comes back to the
	// channel at the same tab and row.
	m.handleSearchEvent(youtube.Event{Kind: youtube.EventResult, Video: playlistVideo(), Rank: 0})
	m.handleSearchEvent(youtube.Event{Kind: youtube.EventResult, Video: media.Video{ID: "list0000002", Title: "Other list", URL: "https://www.youtube.com/playlist?list=list0000002"}, Rank: 10})
	m.selected = 1
	m.handleKey(runeKey('B'))
	if m.channel != nil || len(m.parents) != 2 || m.listTitle != "Other list" {
		t.Fatalf("a playlist is not a channel: channel=%+v parents=%d title=%q", m.channel, len(m.parents), m.listTitle)
	}
	m.handleKey(shiftTabKey)
	if m.status != "No channel open"+noteMark+"c opens the channel of a video" {
		t.Errorf("Shift-Tab outside a channel: %q", m.status)
	}
	focus := m.focus
	m.handleKey(tabKey)
	if m.focus == focus && m.width >= wideWidth {
		t.Error("Tab outside a channel still switches the pane")
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.channel == nil || m.channel.tab != youtube.ChannelPlaylists || m.selected != 1 || len(m.items) != 2 || len(m.parents) != 1 {
		t.Fatalf("Esc should return to the Playlists tab: channel=%+v selected=%d items=%d parents=%d", m.channel, m.selected, len(m.items), len(m.parents))
	}

	m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.channel != nil || len(m.parents) != 0 || len(m.items) != 2 || m.items[0].ID != "video0001" || m.status != "2 results" {
		t.Fatalf("Esc should return to the search: channel=%+v parents=%d items=%d status=%q", m.channel, len(m.parents), len(m.items), m.status)
	}
	m.handleKey(runeKey('q'))
	if !m.confirmQuit {
		t.Error("q on the main page asks to quit")
	}
	m.cancel()
}

func TestAPastedChannelLinkOpensTheChannelWithoutAHistoryEntry(t *testing.T) {
	m := New("")
	m.history = history.Load(filepath.Join(t.TempDir(), "history"), 24*time.Hour, 10, time.Now())
	m.filters = youtube.Filters{Type: youtube.TypeLive}

	if cmd := m.beginSearch("https://www.youtube.com/@GoogleDevelopers/videos"); cmd == nil {
		t.Fatal("nothing started")
	}
	m.cancel()
	if m.channel == nil || m.channel.url != "https://www.youtube.com/@GoogleDevelopers" || m.channel.tab != youtube.ChannelVideos {
		t.Fatalf("channel = %+v", m.channel)
	}
	if m.status != "Opening videos..." || len(m.parents) != 0 {
		t.Errorf("status=%q parents=%d", m.status, len(m.parents))
	}
	if got := m.history.Queries(); len(got) != 0 {
		t.Fatalf("links must not be saved as searches: %q", got)
	}

	// The name comes from the first result, and the list is not capped.
	m.handleSearchEvent(youtube.Event{Kind: youtube.EventResult, Video: media.Video{ID: "video0001", Title: "One", Channel: "Google for Developers"}})
	if m.channel.name != "Google for Developers" || !strings.Contains(m.renderHeader(), "@GoogleDevelopers") {
		t.Errorf("name = %q", m.channel.name)
	}
	for i := 0; i < youtube.ResultLimit+5; i++ {
		m.handleSearchEvent(youtube.Event{Kind: youtube.EventResult, Video: media.Video{ID: fmt.Sprintf("vid%07d", i), Title: "V"}, Rank: 10 + i})
	}
	if len(m.items) != youtube.ResultLimit+6 {
		t.Errorf("a channel tab is not capped: %d items", len(m.items))
	}

	// A new search leaves the channel.
	m.beginSearch("something else")
	m.cancel()
	if m.channel != nil {
		t.Error("a new search should leave the channel")
	}
}

func TestChannelFilterMenuExplainsThatDurationAndDateAreIgnored(t *testing.T) {
	m := New("test")
	m.width, m.height = 100, 30
	m.showFilters = true
	m.filterDraft = youtube.Filters{Type: youtube.TypeChannels, Duration: 1, Date: 2}
	for _, col := range []int{youtube.RowDuration, youtube.RowDate} {
		m.filterCol = col
		if out := m.View(); !strings.Contains(out, "Channels have no length or date") {
			t.Fatalf("column %d should explain why it does nothing:\n%s", col, out)
		}
	}
	if !strings.Contains(m.View(), "Channels") {
		t.Error("the Type column lists Channels")
	}
	if got := m.filterDraft.Summary(); got != "Channels" {
		t.Errorf("summary = %q", got)
	}
}

func TestCInsideTheOpenChannelDoesNotOpenItAgain(t *testing.T) {
	m := New("test")
	m.width, m.height = 120, 30
	m.items, m.ranks = []media.Video{{ID: "video0001", Title: "One", ChannelURL: "https://www.youtube.com/@x"}}, []int{0}
	m.query = "golang"
	m.searchDone, m.metadataDone = true, true

	m.handleKey(runeKey('c'))
	if m.channel == nil || len(m.parents) != 1 {
		t.Fatalf("c should open the channel: %+v parents=%d", m.channel, len(m.parents))
	}

	// A row of the tab, by its link.
	m.handleSearchEvent(youtube.Event{Kind: youtube.EventResult, Video: media.Video{ID: "inside0001", Title: "Inside", ChannelURL: "https://www.youtube.com/@x"}})
	if cmd := m.handleKey(runeKey('c')); cmd != nil || len(m.parents) != 1 || !strings.HasPrefix(m.status, "Already in this channel") {
		t.Fatalf("c in the same channel: parents=%d status=%q", len(m.parents), m.status)
	}

	// A row whose details changed its link to the /channel/ form, by its ID.
	m.applyChannelInfo(channelInfoMsg{url: "https://www.youtube.com/@x", info: youtube.ChannelInfo{ID: testChannelID, Name: "The X"}})
	m.items[0] = media.Video{ID: "inside0001", Title: "Inside", ChannelURL: testChannelURL, ChannelID: testChannelID}
	if cmd := m.handleKey(runeKey('c')); cmd != nil || len(m.parents) != 1 {
		t.Fatalf("c on a video of the channel by ID: parents=%d status=%q", len(m.parents), m.status)
	}

	// Another channel opens on top.
	m.items[0] = media.Video{ID: "other00001", Title: "Other", ChannelURL: "https://www.youtube.com/@y"}
	if cmd := m.handleKey(runeKey('c')); cmd == nil || len(m.parents) != 2 || m.channel.url != "https://www.youtube.com/@y" {
		t.Fatalf("c on another channel: parents=%d channel=%+v", len(m.parents), m.channel)
	}
	m.cancel()
}

func TestTheChannelPageLimitsTheTabs(t *testing.T) {
	m := New("test")
	m.width, m.height = 120, 30
	m.items, m.ranks = []media.Video{{ID: "video0001", Title: "One", ChannelURL: "https://www.youtube.com/@x"}}, []int{0}
	m.query = "golang"
	m.handleKey(runeKey('c'))

	// The answer for a channel that is not open any more is dropped.
	if cmd := m.applyChannelInfo(channelInfoMsg{url: "https://www.youtube.com/@old", info: youtube.ChannelInfo{ID: "UCold", Tabs: []int{youtube.ChannelLive}}}); cmd != nil || m.channel.tabs != nil {
		t.Fatalf("stale info applied: %+v", m.channel)
	}

	// Without Videos, the channel moves to its first tab.
	info := youtube.ChannelInfo{ID: testChannelID, Name: "The X", Tabs: []int{youtube.ChannelShorts, youtube.ChannelPlaylists}}
	if cmd := m.applyChannelInfo(channelInfoMsg{url: "https://www.youtube.com/@x", info: info}); cmd == nil || m.channel.tab != youtube.ChannelShorts {
		t.Fatalf("the channel should open Shorts: %+v", m.channel)
	}
	if bar := m.renderResultLines(60, 20); !strings.Contains(bar, "[1 Shorts]  2 Playlists") || strings.Contains(bar, "Videos") || strings.Contains(bar, "Live") {
		t.Errorf("only the tabs the channel has are shown:\n%s", bar)
	}
	m.handleKey(tabKey)
	if m.channel.tab != youtube.ChannelPlaylists {
		t.Fatalf("Tab = %d, want Playlists", m.channel.tab)
	}
	m.handleKey(tabKey)
	if m.channel.tab != youtube.ChannelShorts {
		t.Fatalf("Tab wraps to %d, want Shorts", m.channel.tab)
	}

	// With one tab, Tab says so.
	m.channel.tabs, m.channel.tab = []int{youtube.ChannelVideos}, youtube.ChannelVideos
	if cmd := m.handleKey(tabKey); cmd != nil || m.status != "This channel has only Videos" {
		t.Errorf("one tab: %q", m.status)
	}
	m.cancel()
}

var (
	tabKey      = tea.KeyMsg{Type: tea.KeyTab}
	shiftTabKey = tea.KeyMsg{Type: tea.KeyShiftTab}
)

func TestAltDigitsJumpToAChannelTab(t *testing.T) {
	m := New("test")
	m.width, m.height = 120, 30
	m.items, m.ranks = []media.Video{{ID: "video0001", Title: "One", ChannelURL: "https://www.youtube.com/@x"}}, []int{0}
	m.handleKey(runeKey('c'))
	m.cancel()
	m.channel.tabs = []int{youtube.ChannelVideos, youtube.ChannelPodcasts, youtube.ChannelPlaylists}

	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}, Alt: true})
	if m.channel.tab != youtube.ChannelPlaylists {
		t.Fatalf("Alt+3 = tab %d, want Playlists", m.channel.tab)
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}, Alt: true})
	if m.channel.tab != youtube.ChannelPodcasts {
		t.Fatalf("Alt+2 = tab %d, want Podcasts", m.channel.tab)
	}
	generation := m.generation
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'9'}, Alt: true})
	if m.channel.tab != youtube.ChannelPodcasts || m.generation != generation || !strings.Contains(m.status, "only 3 tabs") {
		t.Errorf("a tab that does not exist: tab=%d status=%q", m.channel.tab, m.status)
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}, Alt: true})
	if m.generation != generation {
		t.Error("the open tab is not loaded again")
	}
	// A plain digit is still the count of a motion.
	m.handleKey(runeKey('2'))
	if m.channel.tab != youtube.ChannelPodcasts {
		t.Error("a plain digit must not switch tabs")
	}
}

func TestTheChannelHeaderShowsTheHandleAndTheTabBarTopsTheResultsPane(t *testing.T) {
	m := New("test")
	m.width, m.height = 120, 30
	m.items, m.ranks = []media.Video{{ID: "video0001", Title: "One", Channel: "The X", ChannelURL: "https://www.youtube.com/@TheX"}}, []int{0}
	m.query = "golang"
	height, page := m.bodyHeight(), m.pageSize()
	m.handleKey(runeKey('c'))
	m.cancel()

	header := m.renderHeader()
	if !strings.Contains(header, "golang › @TheX") || strings.Contains(header, "@ The X") {
		t.Errorf("the header shows the handle:\n%s", header)
	}
	if lines := strings.Count(header, "\n") + 1; lines != 3 || m.bodyHeight() != height || m.pageSize() >= page {
		t.Errorf("the bar is in the results pane: header lines=%d body %d -> %d, rows %d -> %d", lines, height, m.bodyHeight(), page, m.pageSize())
	}
	pane := strings.Split(m.renderResultLines(60, 20), "\n")
	if len(pane) < 2 || !strings.Contains(pane[0], "[1 Videos]") || !strings.Contains(pane[1], "──") {
		t.Errorf("the pane starts with the tabs and a rule:\n%v", pane)
	}

	// The page can give a handle the link did not have.
	m2 := New("test")
	m2.items, m2.ranks = []media.Video{{ID: "video0001", Title: "One", Channel: "The X", ChannelURL: testChannelURL}}, []int{0}
	m2.handleKey(runeKey('c'))
	m2.cancel()
	m2.applyChannelInfo(channelInfoMsg{url: testChannelURL, info: youtube.ChannelInfo{ID: testChannelID, Handle: "@TheX", Tabs: []int{youtube.ChannelVideos}}})
	if m2.channel.handle != "@TheX" {
		t.Errorf("handle = %q", m2.channel.handle)
	}
}

func TestAChannelRowShowsTheVerifiedMarkAndOneLineOfFacts(t *testing.T) {
	v := channelResult()
	v.ChannelVerified = true
	if got := strings.Join(channelFacts(v), " · "); got != "@TiagoTaquelim · 46.3K subscribers" {
		t.Errorf("facts = %q", got)
	}
	if got := channelTitle(v); got != "Tiago "+v.ChannelBadge() {
		t.Errorf("title = %q", got)
	}
	m := New("test")
	m.width, m.height = 120, 30
	m.items, m.ranks = []media.Video{v}, []int{0}
	if rows := m.renderResultLines(60, 10); !strings.Contains(rows, v.ChannelBadge()) || strings.Contains(rows, "channel ·") {
		t.Errorf("the row:\n%s", rows)
	}
	if preview := m.renderPreview(50, 20); !strings.Contains(preview, "@TiagoTaquelim · 46.3K subscribers") || strings.Contains(preview, "CHANNEL") {
		t.Errorf("the preview:\n%s", preview)
	}
	if got := strings.Join(channelFacts(media.Video{}), " "); got != "channel" {
		t.Errorf("a channel without facts = %q", got)
	}
}

func TestAChannelPictureIsSmallerThanAThumbnail(t *testing.T) {
	if got := channelPictureRows(8); got != 5 {
		t.Errorf("rows = %d, want 5", got)
	}
	if got := channelPictureRows(4); got != 3 || channelPictureRows(2) != 2 || channelPictureRows(0) != 0 {
		t.Errorf("small sizes: %d %d %d", channelPictureRows(4), channelPictureRows(2), channelPictureRows(0))
	}

	m := New("test")
	m.width, m.height = 120, 40
	m.output.Mode = preview.ModeHalfBlocks
	m.items, m.ranks = []media.Video{{ID: "video0001", Title: "One"}, channelResult()}, []int{0, 10}
	_, videoWidth, videoRows, _ := m.previewGeometry()
	m.selected = 1
	_, width, rows, _ := m.previewGeometry()
	if rows >= videoRows || width >= videoWidth || width != rows*2 {
		t.Errorf("channel picture %dx%d, thumbnail %dx%d", width, rows, videoWidth, videoRows)
	}
}

func TestMorePagesLoadNearTheEndOfAPlaylistAndStopWhenTheLastIsShort(t *testing.T) {
	m := New("test")
	m.width, m.height = 120, 30
	var starts []int
	m.startListing("Opening playlist...", youtube.BrowsePageSize, func(_ context.Context, start int, emit func(youtube.Event)) {
		starts = append(starts, start)
	})
	m.cancel()
	for i := 0; i < youtube.BrowsePageSize; i++ {
		m.handleSearchEvent(youtube.Event{Kind: youtube.EventResult, Video: media.Video{ID: fmt.Sprintf("vid%07d", i), Title: "V"}, Rank: i * 10})
	}
	m.handleSearchEvent(youtube.Event{Kind: youtube.EventSearchDone, Count: youtube.BrowsePageSize, More: true})
	if len(m.items) != youtube.BrowsePageSize || m.pager == nil || m.pager.busy || m.pager.done || m.pager.next != 1+youtube.BrowsePageSize {
		t.Fatalf("after the first page: items=%d pager=%+v", len(m.items), m.pager)
	}

	// Far from the end nothing loads.
	m.selected = 5
	if cmd := m.loadMore(); cmd != nil {
		t.Fatal("a page was asked for far from the end")
	}

	// Near the end the next page starts after the first one, and the list keeps
	// its rows and selection.
	m.selected = len(m.items) - 3
	selected := m.selected
	cmd := m.loadMore()
	if cmd == nil || !m.pager.busy || !m.searching || len(m.items) != youtube.BrowsePageSize || m.selected != selected {
		t.Fatalf("loadMore: cmd=%v pager=%+v items=%d", cmd != nil, m.pager, len(m.items))
	}
	if m.loadMore() != nil {
		t.Error("a second page was asked for while one is loading")
	}
	started, ok := cmd().(searchStartedMsg)
	if !ok {
		t.Fatal("loadMore should start a stream")
	}
	for range started.events { // ends when the loader has returned
	}
	if len(starts) != 1 || starts[0] != 1+youtube.BrowsePageSize {
		t.Errorf("page starts = %v", starts)
	}
	m.cancel()

	// The new rows go after the old ones, and a short page is the last.
	m.handleSearchEvent(youtube.Event{Kind: youtube.EventResult, Video: media.Video{ID: "late0000001", Title: "L"}, Rank: youtube.BrowsePageSize * 10})
	m.handleSearchEvent(youtube.Event{Kind: youtube.EventSearchDone, Count: 1, More: false})
	if len(m.items) != youtube.BrowsePageSize+1 || m.items[len(m.items)-1].ID != "late0000001" || !m.pager.done || m.status != plural(len(m.items), "result") {
		t.Fatalf("after the last page: items=%d pager=%+v status=%q", len(m.items), m.pager, m.status)
	}
	m.selected = len(m.items) - 1
	if m.loadMore() != nil {
		t.Error("nothing more to load")
	}

	// A new search is paged by its own page size, and keeps one page of results
	// until the next is loaded.
	m.beginSearch("something else")
	m.cancel()
	if m.pager == nil || m.pager.step != youtube.ResultLimit || m.resultCap != youtube.ResultLimit || m.cap() != youtube.ResultLimit {
		t.Fatalf("a search is paged: pager=%+v cap=%d", m.pager, m.resultCap)
	}
	for i := 0; i < youtube.ResultLimit+5; i++ {
		m.handleSearchEvent(youtube.Event{Kind: youtube.EventResult, Video: media.Video{ID: fmt.Sprintf("srch%07d", i), Title: "S"}, Rank: i * 10})
	}
	m.handleSearchEvent(youtube.Event{Kind: youtube.EventSearchDone, More: true})
	if len(m.items) != youtube.ResultLimit {
		t.Fatalf("a search page keeps its best %d results: %d", youtube.ResultLimit, len(m.items))
	}
	m.selected = len(m.items) - 1
	if m.loadMore() == nil || m.resultCap != 2*youtube.ResultLimit {
		t.Fatalf("the next page raises the cap: cap=%d", m.resultCap)
	}
	m.cancel()
	for i := 0; i < 10; i++ {
		m.handleSearchEvent(youtube.Event{Kind: youtube.EventResult, Video: media.Video{ID: fmt.Sprintf("late%07d", i), Title: "L"}, Rank: 1000 + i})
	}
	if len(m.items) != youtube.ResultLimit+10 {
		t.Errorf("the second page adds its results: %d", len(m.items))
	}

	// A playlist opened from the search is not capped, and Esc brings the cap
	// back.
	m.searching = false
	m.items[0].URL = "https://www.youtube.com/playlist?list=PLabcdef"
	m.selected = 0
	m.openPlaylist(m.items[0])
	m.cancel()
	if m.cap() != 0 {
		t.Errorf("a playlist is not capped: %d", m.cap())
	}
	m.closePlaylist()
	if m.cap() != youtube.ResultLimit || m.pager == nil || m.pager.busy || m.pager.done {
		t.Errorf("the page that was loading is asked for again: cap=%d pager=%+v", m.cap(), m.pager)
	}
}

func TestTheWholeDescriptionOfAChannelIsLoadedOnceWhenSelected(t *testing.T) {
	m := New("test")
	m.width, m.height = 120, 40
	v := channelResult()
	v.Description = "The start of it..."
	m.items, m.ranks = []media.Video{v, {ID: "video0001", Title: "One"}}, []int{0, 10}

	// Selecting a channel only schedules the lookup, once.
	if m.loadChannelDescription() == nil || m.channelDescs[v.ID] != 0 {
		t.Fatalf("a lookup should wait for the selection to rest: %v", m.channelDescs)
	}
	if m.loadChannelDescription() != nil {
		t.Error("the delay is scheduled once")
	}
	// A channel that was left in the meantime is not read.
	m.selected = 1
	if m.fetchChannelDescription(v.ID) != nil || m.channelDescs[v.ID] != 0 || m.descTick != "" {
		t.Fatalf("a row that is not selected any more asks for nothing: %v", m.channelDescs)
	}
	m.selected = 0
	m.loadChannelDescription()
	cmd := m.fetchChannelDescription(v.ID)
	if cmd == nil || m.channelDescs[v.ID] != descAsked {
		t.Fatalf("the selected channel asks for its description: %v", m.channelDescs)
	}
	if m.loadChannelDescription() != nil || m.fetchChannelDescription(v.ID) != nil {
		t.Error("the description is asked for once")
	}
	if preview := m.renderPreview(50, 30); strings.Contains(preview, "The start of it") == false {
		t.Errorf("the snippet shows while the page loads:\n%s", preview)
	}

	long := strings.Repeat("A long line of the real description. ", 12)
	m.applyChannelDescription(channelDescMsg{id: v.ID, text: long})
	if m.items[0].Description != long || m.channelDescs[v.ID] != descDone {
		t.Fatalf("the description is kept: %q %v", m.items[0].Description, m.channelDescs)
	}
	// It wraps and scrolls like a video description, instead of staying folded.
	if lines := wrapDescription(m.items[0].Description, 40); len(lines) < 5 {
		t.Errorf("lines = %d", len(lines))
	}

	// A page that cannot be read keeps the snippet, and a video asks for nothing.
	m2 := New("test")
	m2.items, m2.ranks = []media.Video{v}, []int{0}
	m2.loadChannelDescription()
	m2.fetchChannelDescription(v.ID)
	m2.applyChannelDescription(channelDescMsg{id: v.ID, err: errors.New("offline")})
	if m2.items[0].Description != "The start of it..." {
		t.Errorf("description = %q", m2.items[0].Description)
	}
	m.selected = 1
	if m.loadChannelDescription() != nil {
		t.Error("a video has no channel description to load")
	}
}

func TestAChannelPreviewHasNoBlankLineBetweenFactsAndDescription(t *testing.T) {
	m := New("test")
	m.width, m.height = 120, 40
	m.output.Mode = preview.ModeOff
	v := channelResult()
	v.Description = "About this channel"
	m.items, m.ranks = []media.Video{v}, []int{0}

	lines := strings.Split(m.renderPreview(50, 20), "\n")
	facts := -1
	for i, line := range lines {
		if strings.Contains(line, "46.3K subscribers") {
			facts = i
		}
	}
	if facts < 0 || facts+3 >= len(lines) {
		t.Fatalf("facts line at %d:\n%v", facts, lines)
	}
	if strings.TrimSpace(lines[facts+1]) != "" || !strings.Contains(lines[facts+2], "DESCRIPTION") {
		t.Errorf("one blank line, then the description:\n%q\n%q", lines[facts+1], lines[facts+2])
	}
}

func TestCoursesAndPodcastsHaveTheirOwnMarkAndLabel(t *testing.T) {
	plain := playlistVideo()
	course, podcast := plain, plain
	course.PlaylistType, podcast.PlaylistType = media.PlaylistCourse, media.PlaylistPodcast

	for _, tc := range []struct {
		v           media.Video
		mark, label string
	}{
		{plain, playlistMark, "PLAYLIST"},
		{course, courseMark, "COURSE"},
		{podcast, podcastMark, "PODCAST"},
	} {
		if got := playlistMarkOf(tc.v); got != tc.mark {
			t.Errorf("%s mark = %q", tc.label, got)
		}
		if label, _ := badgeInfo(tc.v); label != tc.label {
			t.Errorf("badge = %q, want %q", label, tc.label)
		}
	}
	if courseMark == podcastMark || courseMark == playlistMark {
		t.Error("the marks differ")
	}

	m := New("test")
	m.width, m.height = 120, 30
	m.items, m.ranks = []media.Video{course, podcast}, []int{0, 10}
	rows := m.renderResultLines(60, 10)
	if !strings.Contains(rows, courseMark) || !strings.Contains(rows, podcastMark) || !strings.Contains(rows, "COURSE") || !strings.Contains(rows, "PODCAST") {
		t.Errorf("rows:\n%s", rows)
	}
	if preview := m.renderPreview(50, 20); !strings.Contains(preview, courseMark+" COURSE") {
		t.Errorf("preview:\n%s", preview)
	}
}

func TestLinkMessagesNameWhatTheResultIs(t *testing.T) {
	course := playlistVideo()
	course.PlaylistType = media.PlaylistCourse
	for _, tc := range []struct {
		v    media.Video
		noun string
	}{
		{media.Video{ID: "video0001", URL: "https://www.youtube.com/watch?v=video0001"}, "video"},
		{channelResult(), "channel"},
		{playlistVideo(), "playlist"},
		{course, "course"},
	} {
		m := New("test")
		m.items, m.ranks = []media.Video{tc.v}, []int{0}
		if got := m.linkNoun(); got != tc.noun {
			t.Errorf("noun = %q, want %q", got, tc.noun)
		}
	}
	if got := New("test").linkNoun(); got != "video" {
		t.Errorf("no result: %q", got)
	}
}

func TestAFailedPageIsAskedForAgainTwiceAndThenGivenUp(t *testing.T) {
	m := New("test")
	m.width, m.height = 120, 30
	m.startListing("Searching...", youtube.ResultLimit, func(context.Context, int, func(youtube.Event)) {})
	m.cancel()
	m.resultCap = youtube.ResultLimit
	for i := 0; i < youtube.ResultLimit; i++ {
		m.handleSearchEvent(youtube.Event{Kind: youtube.EventResult, Video: media.Video{ID: fmt.Sprintf("vid%07d", i), Title: "V"}, Rank: i * 10})
	}
	m.handleSearchEvent(youtube.Event{Kind: youtube.EventSearchDone, More: true})
	m.selected = len(m.items) - 1

	for attempt := 1; attempt <= 2; attempt++ {
		if m.loadMore() == nil {
			t.Fatalf("attempt %d: the page should be asked for", attempt)
		}
		m.cancel()
		if m.resultCap != 2*youtube.ResultLimit {
			t.Fatalf("attempt %d: cap = %d", attempt, m.resultCap)
		}
		m.handleSearchEvent(youtube.Event{Kind: youtube.EventSearchDone, Err: errors.New("timed out")})
		if m.pager.next != 1+youtube.ResultLimit || m.resultCap != youtube.ResultLimit {
			t.Fatalf("attempt %d: a failed page keeps its place and the cap: next=%d cap=%d", attempt, m.pager.next, m.resultCap)
		}
	}
	if !m.pager.done || m.loadMore() != nil {
		t.Errorf("after two failures the list stops asking: %+v", m.pager)
	}
}

func TestTheUnsafeWidthTableIsSortedSoTheSearchCanStopEarly(t *testing.T) {
	for i, span := range unsafeWidth {
		if span[0] > span[1] {
			t.Errorf("span %d is backwards: %U-%U", i, span[0], span[1])
		}
		if i > 0 && span[0] <= unsafeWidth[i-1][1] {
			t.Errorf("span %d (%U) is not after the one before (%U)", i, span[0], unsafeWidth[i-1][1])
		}
	}
	for _, r := range []rune{0xAD, 0x2028, 0x2630, 0x1F1E9, 0x1FAEF} {
		if !widthIsUnsafe(r) {
			t.Errorf("%U should be unsafe", r)
		}
	}
	for _, r := range []rune{'a', 'é', 'ß', '日', '—', '’', 0xAC, 0xAE, 0x2631 + 0x10, 0x1F600} {
		if widthIsUnsafe(r) {
			t.Errorf("%U is drawn the same everywhere", r)
		}
	}
}

func TestCharactersThatTerminalsMeasureDifferentlyAreRemoved(t *testing.T) {
	// A soft hyphen counts 0 cells for the width library and 1 in tmux. A line
	// with one is wider than the screen, the terminal wraps it, and the whole
	// screen is drawn one row lower until it is redrawn.
	if got := clean("Ver­wen­dung der Ge­gen­wart"); got != "Verwendung der Gegenwart" {
		t.Errorf("clean = %q", got)
	}
	// What is typed is kept as it was, and drawn without them.
	typed := "ger\u00adman \u2630 \U0001F1E9\U0001F1EA"
	if got := stripControls(typed); got != typed {
		t.Errorf("stripControls changes what the user typed: %q", got)
	}
	if got := drawable(typed); got != "german  " {
		t.Errorf("drawable = %q", got)
	}
	if got := drawable("tab\there\x1b[2J"); got != "tab here[2J" {
		t.Errorf("drawable keeps stripping controls: %q", got)
	}
	for _, kept := range []string{"Café ß ä ö ü", "日本語を勉強する", "Ünïcödé — “quotes” ’n’ more"} {
		if got := clean(kept); got != kept {
			t.Errorf("clean(%q) = %q", kept, got)
		}
	}
}

func TestNoLineOfTheScreenHoldsACharacterThatTerminalsMeasureDifferently(t *testing.T) {
	m := New("ger­man")
	m.width, m.height = 140, 40
	m.items = []media.Video{
		{ID: "video000001", Title: "Ver­wen­dung", Channel: "Ka­nal ☰", Description: "Ge­gen­wart und mehr", URL: "https://www.youtube.com/watch?v=video000001"},
	}
	m.ranks = []int{0}
	m.query = "ger­man"
	m.searchDone, m.metadataDone = true, true

	for _, line := range strings.Split(m.View(), "\n") {
		for _, r := range line {
			if widthIsUnsafe(r) {
				t.Fatalf("%U is on the screen: %q", r, line)
			}
		}
	}
}
