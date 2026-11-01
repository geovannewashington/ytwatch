package tui

import (
	"fmt"
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
	if rows > 15 {
		t.Errorf("the help has %d lines of text, keep it short", rows)
	}
	for _, duplicate := range []string{"w / b", "n / N", "Tab", "PageUp", "Ctrl+f", "0 / $", "H / M / L"} {
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
