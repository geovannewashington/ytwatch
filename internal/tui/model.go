package tui

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/geovannewashington/ytwatch/internal/config"
	"github.com/geovannewashington/ytwatch/internal/download"
	"github.com/geovannewashington/ytwatch/internal/media"
	"github.com/geovannewashington/ytwatch/internal/player"
	"github.com/geovannewashington/ytwatch/internal/preview"
	"github.com/geovannewashington/ytwatch/internal/youtube"
)

const (
	thumbDebounce     = 140 * time.Millisecond
	animationInterval = 180 * time.Millisecond

	minWidth  = 80
	minHeight = 18
	wideWidth = 100 // below this the preview pane is hidden
)

type searchStartedMsg struct {
	generation int
	events     <-chan youtube.Event
}

type searchEventMsg struct {
	generation int
	event      youtube.Event
}

type searchClosedMsg struct{ generation int }

type thumbnailDelayMsg struct {
	id       string
	sequence int
}

type thumbnailLoadedMsg struct {
	id       string
	sequence int
	path     string
	err      error
}

type playerExitedMsg struct {
	id  string
	err error
}

type downloadDoneMsg struct {
	id    string
	title string
	path  string
	err   error
}

type animationMsg struct{}

type paneFocus int

const (
	resultsPane paneFocus = iota
	previewPane
)

type Model struct {
	ctx    context.Context
	cancel context.CancelFunc

	query       string
	searchInput string
	searchMode  bool
	showHelp    bool
	searching   bool
	searchDone  bool
	searchError string
	status      string

	generation   int
	searchCancel context.CancelFunc
	searchEvents <-chan youtube.Event

	items         []media.Video
	selected      int
	top           int
	width         int
	height        int
	focus         paneFocus
	previewScroll int

	countPrefix string
	pendingG    bool

	metadataDone   bool
	thumbnailPaths map[string]string
	thumbSequence  int
	thumbCancel    context.CancelFunc
	thumbLoadingID string
	thumbFailures  map[string]time.Time
	thumbAttempts  map[string]int
	overlay        *preview.Overlay

	cfg config.Config

	playing      map[string]string
	downloading  map[string]string
	audioOnly    bool
	chatOpened   map[string]bool
	spinnerFrame int
	animating    bool
}

func New(initialQuery string) *Model {
	ctx, cancel := context.WithCancel(context.Background())
	m := &Model{
		ctx: ctx, cancel: cancel,
		query:          strings.TrimSpace(initialQuery),
		status:         "Ready",
		thumbnailPaths: make(map[string]string),
		thumbFailures:  make(map[string]time.Time),
		thumbAttempts:  make(map[string]int),
		playing:        make(map[string]string),
		downloading:    make(map[string]string),
		chatOpened:     make(map[string]bool),
		overlay:        &preview.Overlay{},
		cfg:            config.Default(),
		width:          100, height: 30,
	}
	if m.query == "" {
		m.searchMode = true
		m.searchInput = ""
		m.status = "Type a query and press Enter"
	}
	return m
}

// SetConfig applies the user configuration; call it before running the program.
func (m *Model) SetConfig(cfg config.Config) {
	m.cfg = cfg
	m.audioOnly = cfg.General.AudioOnly
}

func (m *Model) Init() tea.Cmd {
	if m.query != "" {
		return m.beginSearch(m.query)
	}
	return nil
}

func (m *Model) Close() {
	m.cancel()
	if m.searchCancel != nil {
		m.searchCancel()
	}
	if m.thumbCancel != nil {
		m.thumbCancel()
	}
	if m.overlay != nil {
		m.overlay.Close()
	}
}

func (m *Model) beginSearch(query string) tea.Cmd {
	query = strings.TrimSpace(query)
	if query == "" {
		m.searchMode = true
		m.searchInput = m.query
		m.status = "Enter a search query"
		return nil
	}
	if m.searchCancel != nil {
		m.searchCancel()
	}
	if m.thumbCancel != nil {
		m.thumbCancel()
		m.thumbCancel = nil
	}
	m.thumbLoadingID = ""
	m.overlay.Remove()

	ctx, cancel := context.WithCancel(m.ctx)
	m.searchCancel = cancel
	m.generation++
	generation := m.generation
	m.query = query
	m.searchInput = query
	m.searchMode = false
	m.searching = true
	m.searchDone = false
	m.metadataDone = false
	m.searchError = ""
	m.items = nil
	m.selected = 0
	m.top = 0
	m.countPrefix = ""
	m.pendingG = false
	m.thumbSequence++
	m.status = "Searching YouTube..."

	return func() tea.Msg {
		events := make(chan youtube.Event, 256)
		go func() {
			defer close(events)
			youtube.Search(ctx, query, func(event youtube.Event) {
				select {
				case events <- event:
				case <-ctx.Done():
				}
			})
		}()
		return searchStartedMsg{generation: generation, events: events}
	}
}

func waitSearchEvent(generation int, events <-chan youtube.Event) tea.Cmd {
	return func() tea.Msg {
		event, ok := <-events
		if !ok {
			return searchClosedMsg{generation: generation}
		}
		return searchEventMsg{generation: generation, event: event}
	}
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		if m.width < wideWidth {
			m.focus = resultsPane
		}
		m.keepSelectionVisible()
		m.scrollPreview(0)
		if m.width < wideWidth || m.height < minHeight || m.showHelp {
			m.overlay.Remove()
		} else {
			m.showSelectedThumbnail()
		}
		return m, nil

	case searchStartedMsg:
		if msg.generation != m.generation {
			return m, nil
		}
		m.searchEvents = msg.events
		return m, waitSearchEvent(msg.generation, msg.events)

	case searchEventMsg:
		if msg.generation != m.generation {
			return m, nil
		}
		cmds := m.handleSearchEvent(msg.event)
		cmds = append(cmds, waitSearchEvent(msg.generation, m.searchEvents))
		return m, tea.Batch(cmds...)

	case searchClosedMsg:
		if msg.generation == m.generation {
			m.searchEvents = nil
			if !m.searchDone {
				m.searching = false
				m.status = "Search stopped"
			}
			if !m.metadataDone {
				m.metadataDone = true
			}
		}
		return m, nil

	case thumbnailDelayMsg:
		if msg.sequence != m.thumbSequence || m.selectedID() != msg.id {
			return m, nil
		}
		v, ok := m.selectedVideo()
		if !ok {
			return m, nil
		}
		ctx, cancel := context.WithTimeout(m.ctx, 8*time.Second)
		m.thumbCancel = cancel
		m.thumbLoadingID = msg.id
		return m, loadThumbnail(ctx, v, msg.sequence)

	case thumbnailLoadedMsg:
		if msg.sequence != m.thumbSequence {
			return m, nil
		}
		m.thumbLoadingID = ""
		if m.thumbCancel != nil {
			m.thumbCancel()
			m.thumbCancel = nil
		}
		if msg.err == nil && msg.path != "" {
			m.thumbnailPaths[msg.id] = msg.path
			delete(m.thumbFailures, msg.id)
			m.thumbAttempts[msg.id] = 0
			if m.selectedID() == msg.id {
				m.showSelectedThumbnail()
			}
		} else if msg.err != nil && m.selectedID() == msg.id && m.ctx.Err() == nil {
			// Rapid navigation often cancels the previous fetch. Don't treat stale
			// cancellation as a failure, but retry the final selected item briefly.
			m.thumbAttempts[msg.id]++
			if m.thumbAttempts[msg.id] <= 2 {
				id, sequence := msg.id, m.thumbSequence
				delay := time.Duration(m.thumbAttempts[msg.id]) * 350 * time.Millisecond
				m.status = "Loading thumbnail..."
				return m, tea.Tick(delay, func(time.Time) tea.Msg {
					return thumbnailDelayMsg{id: id, sequence: sequence}
				})
			}
			m.thumbFailures[msg.id] = time.Now()
			m.status = "Thumbnail unavailable; selection and playback still work"
		}
		return m, nil

	case playerExitedMsg:
		delete(m.playing, msg.id)
		if len(m.playing) == 0 {
			m.status = "Playback ended"
		} else {
			m.status = fmt.Sprintf("▶ %d player(s) running", len(m.playing))
		}
		if msg.err != nil {
			m.status = msg.err.Error()
		}
		// The running animation tick keeps itself alive; do not start another.
		return m, nil

	case downloadDoneMsg:
		delete(m.downloading, msg.id)
		switch {
		case errors.Is(msg.err, context.Canceled):
			// ytwatch is quitting; yt-dlp resumes the partial file next time.
		case msg.err != nil:
			m.status = "Download failed | " + clean(msg.err.Error())
		default:
			m.status = "✔ Saved | " + msg.path
		}
		return m, nil

	case animationMsg:
		if len(m.playing) == 0 {
			m.animating = false
			return m, nil
		}
		m.spinnerFrame = (m.spinnerFrame + 1) % len(eqFrames)
		return m, tickAnimation()

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			m.cancel()
			return m, tea.Quit
		}
		if m.searchMode {
			return m, m.handleSearchInput(msg)
		}
		if m.showHelp {
			if msg.String() == "?" || msg.String() == "esc" || msg.String() == "q" || msg.String() == "enter" {
				m.showHelp = false
				m.showSelectedThumbnail()
			}
			return m, nil
		}
		return m, m.handleKey(msg)
	}
	return m, nil
}

func (m *Model) handleSearchEvent(event youtube.Event) []tea.Cmd {
	switch event.Kind {
	case youtube.EventResult:
		for _, existing := range m.items {
			if existing.ID == event.Video.ID {
				return nil
			}
		}
		m.items = append(m.items, event.Video)
		if len(m.items) == 1 {
			m.selected, m.top = 0, 0
			return []tea.Cmd{m.scheduleThumbnail()}
		}
	case youtube.EventDetails:
		for i := range m.items {
			if m.items[i].ID == event.Video.ID {
				m.items[i] = media.MergeDetails(m.items[i], event.Video)
				break
			}
		}
		if _, isPlaying := m.playing[event.Video.ID]; isPlaying && event.Video.IsCurrentlyLive() && !m.chatOpened[event.Video.ID] {
			if err := player.OpenYouTubeChat(event.Video, m.cfg); err == nil {
				m.chatOpened[event.Video.ID] = true
				m.status = "Live stream | opened YouTube chat"
			}
		}
		if m.selectedID() == event.Video.ID && m.thumbnailPaths[event.Video.ID] == "" && m.thumbLoadingID != event.Video.ID {
			return []tea.Cmd{m.scheduleThumbnail()}
		}
	case youtube.EventSearchDone:
		m.searchDone = true
		m.searching = false
		if event.Err != nil && len(m.items) == 0 {
			m.searchError = clean(event.Err.Error())
			m.status = "Search failed | / to retry"
		} else if len(m.items) == 0 {
			m.status = "No results | press / to search"
		} else {
			m.status = fmt.Sprintf("%d results | resolving live status...", len(m.items))
		}
	case youtube.EventDetailsEnd:
		m.metadataDone = true
		if len(m.items) > 0 && len(m.playing) == 0 {
			m.status = fmt.Sprintf("%d results", len(m.items))
		}
	}
	return nil
}

func (m *Model) handleSearchInput(msg tea.KeyMsg) tea.Cmd {
	if msg.Paste {
		m.searchInput += string(msg.Runes)
		return nil
	}
	switch msg.String() {
	case "enter":
		query := strings.TrimSpace(m.searchInput)
		if query == "" {
			m.status = "Enter a search query"
			return nil
		}
		return m.beginSearch(query)
	case "esc":
		m.searchMode = false
		m.status = "Search cancelled"
	case "backspace", "ctrl+h":
		if len(m.searchInput) > 0 {
			runes := []rune(m.searchInput)
			m.searchInput = string(runes[:len(runes)-1])
		}
	case "ctrl+u":
		m.searchInput = ""
	case "ctrl+w":
		m.searchInput = strings.TrimRightFunc(m.searchInput, unicode.IsSpace)
		if i := strings.LastIndexFunc(m.searchInput, unicode.IsSpace); i >= 0 {
			m.searchInput = m.searchInput[:i+1]
		} else {
			m.searchInput = ""
		}
	case " ":
		m.searchInput += " "
	default:
		if msg.Type == tea.KeyRunes {
			m.searchInput += string(msg.Runes)
		}
	}
	return nil
}

func (m *Model) handleKey(msg tea.KeyMsg) tea.Cmd {
	key := msg.String()
	if msg.Paste {
		return nil
	}

	if m.pendingG {
		m.pendingG = false
		if key == "g" {
			if m.focus == previewPane {
				m.previewScroll = 0
			} else {
				m.jumpTo(0)
				return m.scheduleThumbnail()
			}
			return nil
		}
	}

	if len(key) == 1 && key[0] >= '0' && key[0] <= '9' {
		if key == "0" && m.countPrefix == "" {
			if m.focus == previewPane {
				m.previewScroll = 0
			} else {
				m.jumpTo(0)
				return m.scheduleThumbnail()
			}
		} else if len(m.countPrefix) < 4 {
			m.countPrefix += key
		}
		return nil
	}
	count := 1
	if m.countPrefix != "" {
		if parsed, err := strconv.Atoi(m.countPrefix); err == nil && parsed > 0 {
			count = parsed
		}
	}
	m.countPrefix = ""

	switch key {
	case "q", "ctrl+c":
		m.cancel()
		return tea.Quit
	case "/":
		// Start with a clean query so the next typed characters don't
		// accidentally append to the previous search.
		m.searchMode = true
		m.searchInput = ""
		m.status = "Search | Enter runs | Esc cancels | Ctrl+U clears"
		return nil
	case "?":
		m.showHelp = true
		m.overlay.Remove()
		return nil
	case "esc":
		m.status = "Ready"
		return nil
	case "enter":
		return m.playSelected()
	case "h", "left":
		m.focus = resultsPane
		return nil
	case "l", "right":
		if m.width >= wideWidth {
			m.focus = previewPane
		} else {
			m.status = "Widen the terminal to focus the preview pane"
		}
		return nil
	case "tab":
		if m.width >= wideWidth {
			if m.focus == resultsPane {
				m.focus = previewPane
			} else {
				m.focus = resultsPane
			}
		} else {
			m.focus = resultsPane
		}
		return nil
	case "up", "k", "ctrl+p":
		if m.focus == previewPane {
			m.scrollPreview(-count)
			return nil
		}
		m.moveSelection(-count)
		return m.scheduleThumbnail()
	case "down", "j", "ctrl+n":
		if m.focus == previewPane {
			m.scrollPreview(count)
			return nil
		}
		m.moveSelection(count)
		return m.scheduleThumbnail()
	case "w", "e", "n":
		m.moveSelection(count)
		m.focus = resultsPane
		return m.scheduleThumbnail()
	case "b", "N":
		m.moveSelection(-count)
		m.focus = resultsPane
		return m.scheduleThumbnail()
	case "pgup", "pageup", "ctrl+b":
		if m.focus == previewPane {
			m.scrollPreview(-m.pageSize() * count)
			return nil
		}
		m.moveSelection(-m.pageSize() * count)
		return m.scheduleThumbnail()
	case "ctrl+u":
		if m.focus == previewPane {
			m.scrollPreview(-max(1, m.pageSize()/2) * count)
			return nil
		}
		m.moveSelection(-max(1, m.pageSize()/2) * count)
		return m.scheduleThumbnail()
	case "pgdown", "pagedown", "ctrl+f":
		if m.focus == previewPane {
			m.scrollPreview(m.pageSize() * count)
			return nil
		}
		m.moveSelection(m.pageSize() * count)
		return m.scheduleThumbnail()
	case "ctrl+d":
		if m.focus == previewPane {
			m.scrollPreview(max(1, m.pageSize()/2) * count)
			return nil
		}
		m.moveSelection(max(1, m.pageSize()/2) * count)
		return m.scheduleThumbnail()
	case "g":
		m.pendingG = true
		return nil
	case "G", "shift+g", "end", "$":
		if m.focus == previewPane {
			m.previewScroll = m.previewMaxScroll()
			return nil
		}
		m.jumpTo(len(m.items) - 1)
		return m.scheduleThumbnail()
	case "home":
		m.jumpTo(0)
		return m.scheduleThumbnail()
	case "H", "shift+h":
		if m.focus == previewPane {
			m.previewScroll = 0
			return nil
		}
		m.jumpTo(m.top)
		return m.scheduleThumbnail()
	case "M", "shift+m":
		if m.focus == previewPane {
			m.previewScroll = m.previewMaxScroll() / 2
			return nil
		}
		m.jumpTo(m.top + m.pageSize()/2)
		return m.scheduleThumbnail()
	case "L", "shift+l":
		if m.focus == previewPane {
			m.previewScroll = m.previewMaxScroll()
			return nil
		}
		m.jumpTo(m.top + m.pageSize() - 1)
		return m.scheduleThumbnail()
	case "r":
		return m.beginSearch(m.query)
	case "a":
		m.audioOnly = !m.audioOnly
		if m.audioOnly {
			m.status = "Audio-only mode on | applies to playing and downloading"
		} else {
			m.status = "Audio-only mode off"
		}
		return nil
	case "d":
		return m.downloadSelected()
	case "y":
		v, ok := m.selectedVideo()
		if !ok {
			return nil
		}
		if _, err := exec.LookPath("wl-copy"); err != nil {
			m.status = "wl-copy not found | install wl-clipboard"
			return nil
		}
		cmd := exec.Command("wl-copy")
		cmd.Stdin = strings.NewReader(v.CanonicalURL())
		if err := cmd.Start(); err != nil {
			m.status = "Could not copy URL: " + err.Error()
			return nil
		}
		go func() { _ = cmd.Wait() }()
		m.status = "Copied video URL"
		return nil
	}
	return nil
}

func (m *Model) moveSelection(delta int) {
	if len(m.items) == 0 {
		return
	}
	m.selected += delta
	if m.selected < 0 {
		m.selected = 0
	}
	if m.selected >= len(m.items) {
		m.selected = len(m.items) - 1
	}
	m.previewScroll = 0
	m.keepSelectionVisible()
}

func (m *Model) jumpTo(index int) {
	if len(m.items) == 0 {
		m.selected, m.top = 0, 0
		return
	}
	if index < 0 {
		index = 0
	}
	if index >= len(m.items) {
		index = len(m.items) - 1
	}
	m.selected = index
	m.previewScroll = 0
	m.keepSelectionVisible()
}

func (m *Model) keepSelectionVisible() {
	visible := m.pageSize()
	if visible < 1 {
		visible = 1
	}
	if m.selected < m.top {
		m.top = m.selected
	}
	if m.selected >= m.top+visible {
		m.top = m.selected - visible + 1
	}
	maxTop := len(m.items) - visible
	if maxTop < 0 {
		maxTop = 0
	}
	if m.top > maxTop {
		m.top = maxTop
	}
}

func (m *Model) pageSize() int {
	// The list uses two terminal rows per result and reserves one row for
	// the remaining-results hint, matching renderResultLines exactly.
	return max(1, (m.height-7)/2)
}

func (m *Model) selectedVideo() (media.Video, bool) {
	if m.selected < 0 || m.selected >= len(m.items) {
		return media.Video{}, false
	}
	return m.items[m.selected], true
}

func (m *Model) selectedID() string {
	v, ok := m.selectedVideo()
	if !ok {
		return ""
	}
	return v.ID
}

func (m *Model) scheduleThumbnail() tea.Cmd {
	v, ok := m.selectedVideo()
	if ok && m.thumbLoadingID == v.ID && m.thumbCancel != nil {
		return nil
	}
	m.thumbSequence++
	sequence := m.thumbSequence
	if m.thumbCancel != nil {
		m.thumbCancel()
		m.thumbCancel = nil
	}
	m.thumbLoadingID = ""
	if !ok || m.width < wideWidth || !preview.Available() {
		m.overlay.Remove()
		return nil
	}
	if path := m.thumbnailPaths[v.ID]; path != "" {
		m.showSelectedThumbnail()
		return nil
	}
	if failedAt, failed := m.thumbFailures[v.ID]; failed {
		if time.Since(failedAt) < 2*time.Second {
			return nil
		}
		delete(m.thumbFailures, v.ID)
		m.thumbAttempts[v.ID] = 0
	}
	// Clear an unrelated image while the newly selected thumbnail loads.
	m.overlay.Remove()
	id := v.ID
	return tea.Tick(thumbDebounce, func(time.Time) tea.Msg {
		return thumbnailDelayMsg{id: id, sequence: sequence}
	})
}

func (m *Model) scrollPreview(delta int) {
	m.previewScroll += delta
	if m.previewScroll < 0 {
		m.previewScroll = 0
	}
	if limit := m.previewMaxScroll(); m.previewScroll > limit {
		m.previewScroll = limit
	}
}

func (m *Model) previewMaxScroll() int {
	v, ok := m.selectedVideo()
	if !ok {
		return 0
	}
	description := strings.TrimSpace(v.Description)
	if description == "" {
		return 0
	}
	_, width := m.paneWidths()
	// PREVIEW, thumbnail rows, blank, title, channel, stats, blank, DESCRIPTION.
	baseLines := m.thumbRows() + 7
	if section, _ := m.streamSection(v, width); len(section) > 0 {
		baseLines += 1 + len(section) // blank separator + section lines
	}
	visible := max(1, m.bodyHeight()-baseLines)
	return max(0, len(wrapLines(description, width, 1000))-visible)
}

// bodyHeight is the content height of the panes: the terminal minus the
// 3-line header, the pane borders and the 1-line footer.
func (m *Model) bodyHeight() int {
	return max(4, m.height-6)
}

// paneWidths returns the content width (inside border and padding) of the
// results pane and the preview pane. In narrow terminals the results pane
// uses the full width and the preview width is 0.
func (m *Model) paneWidths() (results, preview int) {
	if m.width < wideWidth {
		return max(12, m.width-4), 0
	}
	// Two panes, each with a 1-column border and 1-column padding per side.
	content := m.width - 8
	results = max(25, content*53/100)
	preview = max(12, content-results)
	return results, preview
}

func (m *Model) thumbRows() int {
	return max(0, min(8, m.bodyHeight()-10))
}

func loadThumbnail(ctx context.Context, v media.Video, sequence int) tea.Cmd {
	return func() tea.Msg {
		path, err := preview.Download(ctx, v)
		return thumbnailLoadedMsg{id: v.ID, sequence: sequence, path: path, err: err}
	}
}

func (m *Model) showSelectedThumbnail() {
	if m.width < wideWidth {
		m.overlay.Remove()
		return
	}
	v, ok := m.selectedVideo()
	if !ok {
		m.overlay.Remove()
		return
	}
	path := m.thumbnailPaths[v.ID]
	if path == "" {
		m.overlay.Remove()
		return
	}
	leftWidth, rightWidth, thumbRows, y := m.previewGeometry()
	if thumbRows < 3 || rightWidth < 8 {
		m.overlay.Remove()
		return
	}
	if !m.overlay.Show(path, leftWidth+2, y, rightWidth, thumbRows) {
		m.status = "Thumbnail overlay unavailable | check ueberzugpp/Wayland support"
	}
}

func (m *Model) previewGeometry() (leftOuterWidth, rightInnerWidth, thumbRows, y int) {
	results, preview := m.paneWidths()
	leftOuterWidth = results + 4 // border + horizontal padding
	rightInnerWidth = preview    // overlay occupies the preview's content area
	thumbRows = m.thumbRows()
	y = 5 // 3-line header, pane border, then PREVIEW title
	return
}

func (m *Model) playSelected() tea.Cmd {
	v, ok := m.selectedVideo()
	if !ok {
		m.status = "No result selected"
		return nil
	}
	if v.CanonicalURL() == "" {
		m.status = "This result has no playable URL"
		return nil
	}
	if _, exists := m.playing[v.ID]; exists {
		m.status = "Already playing: " + v.Title
		return nil
	}
	cmd, err := player.Start(v, m.cfg, m.audioOnly)
	if err != nil {
		m.status = err.Error()
		return nil
	}
	m.playing[v.ID] = v.Title
	m.status = "▶ Playing | " + clean(v.Title)
	if v.IsCurrentlyLive() {
		if err := player.OpenYouTubeChat(v, m.cfg); err == nil {
			m.chatOpened[v.ID] = true
			m.status = "Live stream | opened chat"
		} else {
			m.status = "Live stream | " + err.Error()
		}
	}
	cmds := []tea.Cmd{waitForPlayer(cmd, v.ID)}
	if !m.animating {
		m.animating = true
		cmds = append(cmds, tickAnimation())
	}
	return tea.Batch(cmds...)
}

func (m *Model) downloadSelected() tea.Cmd {
	v, ok := m.selectedVideo()
	if !ok {
		m.status = "No result selected"
		return nil
	}
	if _, exists := m.downloading[v.ID]; exists {
		m.status = "Already downloading: " + clean(v.Title)
		return nil
	}
	if v.IsPlaylist() {
		m.status = "Playlists cannot be downloaded yet"
		return nil
	}
	if v.IsCurrentlyLive() || v.IsUpcoming() {
		m.status = "Live and upcoming streams cannot be downloaded"
		return nil
	}
	m.downloading[v.ID] = v.Title
	m.status = "⬇ Downloading | " + clean(v.Title)
	ctx, cfg, audioOnly := m.ctx, m.cfg, m.audioOnly
	return func() tea.Msg {
		path, err := download.Run(ctx, v, cfg, audioOnly)
		return downloadDoneMsg{id: v.ID, title: v.Title, path: path, err: err}
	}
}

func waitForPlayer(cmd *player.Process, id string) tea.Cmd {
	return func() tea.Msg {
		return playerExitedMsg{id: id, err: cmd.Wait()}
	}
}

func tickAnimation() tea.Cmd {
	return tea.Tick(animationInterval, func(time.Time) tea.Msg { return animationMsg{} })
}

func (m *Model) View() string {
	if m.width < 1 || m.height < 1 {
		return "ytwatch | starting..."
	}
	if m.width < minWidth || m.height < minHeight {
		return fmt.Sprintf("ytwatch needs a terminal at least %dx%d\nResize the terminal or press q to quit.", minWidth, minHeight)
	}

	if m.showHelp {
		return m.renderHelp()
	}
	return lipgloss.JoinVertical(lipgloss.Left, m.renderHeader(), m.renderBody(), m.renderFooter())
}

func (m *Model) renderHeader() string {
	brand := titleStyle.Render("ytwatch") + taglineStyle.Render("  -  search YouTube, play in mpv")
	query := m.query
	if m.searchMode {
		query = tailTruncate(m.searchInput+"_", max(10, m.width-8))
	}
	searchLine := searchStyle.Render("> ")
	if query == "" {
		searchLine += helpStyle.Render("press / to search videos, Shorts and live streams")
	} else {
		searchLine += truncate(query, max(10, m.width-8))
	}
	keys := "j/k move | h/l panes | Enter play | / search | ? help | q quit"
	if m.searchMode {
		keys = "Enter search | Esc cancel | Ctrl+U clear | Ctrl+W delete word"
	}
	center := func(s string) string {
		return lipgloss.PlaceHorizontal(m.width, lipgloss.Center, s)
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		center(brand),
		lipgloss.NewStyle().Width(m.width).Render(searchLine),
		center(helpStyle.Render(truncate(keys, m.width))),
	)
}

func (m *Model) renderBody() string {
	height := m.bodyHeight()
	// lipgloss Width includes padding but not the border, so add the 2
	// padding columns to get the requested content width.
	const padding = 2
	resultsWidth, previewWidth := m.paneWidths()
	if m.width < wideWidth {
		content := m.renderResultLines(resultsWidth, height)
		return activePanelStyle.Width(resultsWidth + padding).Height(height).Render(content)
	}

	leftStyle, rightStyle := panelStyle, panelStyle
	if m.focus == resultsPane {
		leftStyle = activePanelStyle
	} else {
		rightStyle = activePanelStyle
	}
	leftPane := leftStyle.Width(resultsWidth + padding).Height(height).Render(m.renderResultLines(resultsWidth, height))
	rightPane := rightStyle.Width(previewWidth + padding).Height(height).Render(m.renderPreview(previewWidth, height))
	return lipgloss.JoinHorizontal(lipgloss.Top, leftPane, rightPane)
}

func (m *Model) renderResultLines(width, height int) string {
	if width < 8 {
		width = 8
	}
	lines := make([]string, 0, height)
	visible := max(1, (height-1)/2) // Reserve a row for the remaining-results hint.
	top := min(max(0, m.top), len(m.items))
	end := min(len(m.items), top+visible)
	for i := top; i < end; i++ {
		v := m.items[i]
		selected := i == m.selected
		cursor := "  "
		if selected {
			cursor = "> "
		}

		badge := ""
		if label, style := badgeInfo(v); label != "" {
			badge = style.Render(label)
		}
		leftWidth := width
		if badge != "" {
			leftWidth = max(2, width-lipgloss.Width(badge)-1)
		}
		titleStyle := normalTitleStyle
		if selected {
			titleStyle = selectedTitleStyle
		}
		marker := titleStyle.Render(cursor)
		if _, isPlaying := m.playing[v.ID]; isPlaying {
			marker = equalizer(m.spinnerFrame, 1) + " "
		}
		titleWidth := leftWidth - 2
		prefix := ""
		if v.IsPlaylist() {
			prefix = playlistBadgeStyle.Render(playlistMark + " ")
			titleWidth -= 2
		}
		title := padToWidth(truncate(clean(v.Title), max(1, titleWidth)), titleWidth)
		line := marker + prefix + titleStyle.Render(title)
		if badge != "" {
			line += " " + badge
		}
		lines = append(lines, line)

		meta := clean(v.ChannelName()) + " | " + v.DurationText()
		if v.IsPlaylist() {
			meta = strings.Join(append([]string{clean(v.ChannelName()), "playlist"}, playlistFacts(v)...), " | ")
		} else if v.IsCurrentlyLive() {
			meta = clean(v.ChannelName()) + " | streaming now"
			if v.ConcurrentViewers > 0 {
				meta += " | " + v.ViewsText()
			}
		} else if v.ViewCount > 0 {
			meta += " | " + v.ViewsText()
		}
		age := v.AgeText(time.Now())
		if age != "date n/a" && !v.IsPlaylist() {
			meta += " | " + age
		}
		meta = truncate(meta, width-2)
		if selected {
			lines = append(lines, selectedMetaStyle.Render(padToWidth("  "+meta, width)))
		} else {
			lines = append(lines, metaStyle.Render("  "+meta))
		}
	}

	if len(m.items) == 0 {
		message := "Type a query to search YouTube"
		if m.searching {
			message = "Searching videos and live streams..."
		}
		if m.searchError != "" {
			message = truncate("Search failed: "+m.searchError, width)
		}
		lines = append(lines, subtitleStyle.Render(truncate(message, width)))
	} else if end < len(m.items) {
		lines = append(lines, metaStyle.Render(truncate(fmt.Sprintf("  v %d more results", len(m.items)-end), width)))
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	return strings.Join(lines, "\n")
}

// streamSection describes the stream state of a video in the preview. The
// first line uses the returned style, the rest are secondary text. Ordinary
// videos and Shorts have no section.
func (m *Model) streamSection(v media.Video, width int) ([]string, lipgloss.Style) {
	switch v.Kind() {
	case media.KindLive:
		head := "LIVE NOW"
		if v.ConcurrentViewers > 0 {
			head += "  " + v.ViewsText()
		}
		hint := "Enter plays in mpv"
		if m.cfg.Chat.Enabled {
			hint += " and opens the chat window"
		}
		return []string{head, hint}, liveBadgeStyle
	case media.KindUpcoming:
		return []string{"⏳ UPCOMING  " + v.StartText(time.Now()), "Not live yet; playback works once it starts"}, upcomingBadgeStyle
	case media.KindPastLive:
		return []string{"📼 LIVE VOD", "Recording of a past livestream"}, pastLiveBadgeStyle
	case media.KindPlaylist:
		lines := []string{playlistMark + " PLAYLIST"}
		for _, entry := range v.Entries[:min(len(v.Entries), 3)] {
			lines = append(lines, entryLine(entry, width))
		}
		if len(v.Entries) == 0 && !m.metadataDone {
			lines = append(lines, "Loading videos...")
		}
		return append(lines, "Enter plays the full playlist"), playlistBadgeStyle
	}
	return nil, metaStyle
}

// playlistFacts lists what is known about a playlist: its size and when it
// last changed. Both are empty until the detail lookup finishes.
func playlistFacts(v media.Video) []string {
	var facts []string
	if count := v.PlaylistCountText(); count != "" {
		facts = append(facts, count)
	}
	if updated := v.UpdatedText(time.Now()); updated != "" {
		facts = append(facts, updated)
	}
	return facts
}

// entryLine is one video of a playlist as "title · 3:06", cutting the title
// so the duration stays visible.
func entryLine(e media.PlaylistEntry, width int) string {
	title := clean(e.Title)
	duration := e.DurationText()
	if duration == "" {
		return truncate(title, width)
	}
	suffix := " · " + duration
	return truncate(title, max(1, width-lipgloss.Width(suffix))) + suffix
}

func (m *Model) renderPreview(width, height int) string {
	width = max(8, width)
	v, ok := m.selectedVideo()
	lines := []string{panelTitleStyle.Render("PREVIEW")}
	thumbRows := m.thumbRows()
	for i := 0; i < thumbRows; i++ {
		lines = append(lines, "")
	}
	if !preview.Available() && thumbRows > 0 {
		lines[1] = metaStyle.Render(truncate("Install ueberzugpp for thumbnails", width))
	}
	if !ok {
		lines = append(lines, "", subtitleStyle.Render("Select a result to inspect it."))
	} else {
		lines = append(lines, "")
		lines = append(lines, lipgloss.NewStyle().Bold(true).Render(truncate(clean(v.Title), width)))
		lines = append(lines, metaStyle.Render(truncate(clean(v.ChannelName()), width)))

		var stats []string
		if v.IsCurrentlyLive() {
			stats = append(stats, "streaming now")
		} else {
			stats = append(stats, v.DurationText())
		}
		if v.ViewCount > 0 && !v.IsCurrentlyLive() {
			stats = append(stats, v.ViewsText())
		}
		if age := v.AgeText(time.Now()); age != "date n/a" && !v.IsUpcoming() {
			stats = append(stats, age)
		}
		if v.IsShort() {
			stats = append(stats, "⚡ YouTube Short")
		}
		if v.IsPlaylist() {
			stats = playlistFacts(v)
			if len(stats) == 0 {
				stats = []string{"playlist"}
			}
		}
		lines = append(lines, metaStyle.Render(truncate(strings.Join(stats, " | "), width)))
		if section, style := m.streamSection(v, width); len(section) > 0 {
			lines = append(lines, "")
			for i, line := range section {
				if i == 0 {
					lines = append(lines, style.Render(truncate(line, width)))
				} else {
					lines = append(lines, metaStyle.Render(truncate(line, width)))
				}
			}
		}

		description := strings.TrimSpace(v.Description)
		if description == "" {
			if !m.metadataDone {
				description = "Loading description and stream status..."
			} else {
				description = "No description available."
			}
		}
		lines = append(lines, "", panelTitleStyle.Render("DESCRIPTION"))
		descLines := wrapLines(description, width, 1000)
		available := max(0, height-len(lines))
		maxScroll := max(0, len(descLines)-available)
		scroll := min(max(0, m.previewScroll), maxScroll)
		for _, line := range descLines[scroll:min(len(descLines), scroll+available)] {
			lines = append(lines, descriptionStyle.Render(truncate(line, width)))
		}
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}
func (m *Model) renderFooter() string {
	status := m.status
	prefix := ""
	if len(m.playing) > 0 {
		prefix = equalizer(m.spinnerFrame, 3) + " "
		status = "Playing " + strconv.Itoa(len(m.playing)) + " video(s)"
	}
	style := statusStyle
	if m.searchError != "" && len(m.items) == 0 {
		status = m.searchError
		style = errorStyle
		prefix = ""
	}
	tags := ""
	if m.audioOnly {
		tags += "♪ audio  "
	}
	if len(m.downloading) > 0 {
		tags += fmt.Sprintf("⬇ %d  ", len(m.downloading))
	}
	right := footerStyle.Render(tags + fmt.Sprintf("%d/%d", min(m.selected+1, len(m.items)), len(m.items)))
	leftWidth := max(10, m.width-lipgloss.Width(right)-2-lipgloss.Width(prefix))
	left := prefix + style.Render(padToWidth(truncate(status, leftWidth), leftWidth))
	return left + "  " + right
}

func (m *Model) renderHelp() string {
	rows := [][2]string{
		{"j / k, ^ / v", "Move selection (or scroll preview)"},
		{"w / b, n / N", "Next / previous result"},
		{"5j / 3k", "Move several results"},
		{"gg / G, 0 / $", "First / last result"},
		{"H / M / L", "Top / middle / bottom of visible list"},
		{"Ctrl+d/u, Ctrl+f/b", "Half-page / full-page down / up"},
		{"h / l, Tab", "Focus results / preview (Tab toggles)"},
		{"/", "Search (Enter runs, Esc cancels)"},
		{"r", "Repeat current search"},
		{"Enter", "Play selected result in mpv"},
		{"d", "Download selected result"},
		{"a", "Toggle audio-only (play and download)"},
		{"y", "Copy URL with wl-copy"},
		{"? / Esc", "Close this help"},
		{"q", "Quit"},
	}
	notes := []string{
		"Search includes regular videos and a live-oriented query.",
		"Current livestreams open YouTube chat in your browser.",
		"Shorts are marked [SHORT]; ordinary videos have no type label.",
		"Playlists are marked PLAYLIST; Enter plays all their videos.",
	}
	// Try the roomiest layout first and drop notes, padding and the blank
	// line under the title until the box fits the terminal height.
	layouts := []struct {
		notes, titleGap bool
		pad             int
	}{{true, true, 1}, {false, true, 1}, {false, true, 0}, {false, false, 0}}
	var box string
	for _, layout := range layouts {
		var b strings.Builder
		b.WriteString(panelTitleStyle.Render("KEYBOARD") + "\n")
		if layout.titleGap {
			b.WriteString("\n")
		}
		for i, row := range rows {
			b.WriteString("  " + searchStyle.Render(padToWidth(row[0], 20)) + helpStyle.Render(row[1]))
			if i < len(rows)-1 {
				b.WriteString("\n")
			}
		}
		if layout.notes {
			b.WriteString("\n\n")
			for i, note := range notes {
				b.WriteString(helpStyle.Render("  " + note))
				if i < len(notes)-1 {
					b.WriteString("\n")
				}
			}
		}
		box = helpBoxStyle.Padding(layout.pad, 2).Width(min(78, m.width-4)).Render(b.String())
		if lipgloss.Height(box) <= m.height {
			break
		}
	}
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

func badgeInfo(v media.Video) (string, lipgloss.Style) {
	switch v.Kind() {
	case media.KindLive:
		return "LIVE", liveBadgeStyle
	case media.KindUpcoming:
		return "⏳ UPCOMING", upcomingBadgeStyle
	case media.KindPastLive:
		return "📼 LIVE VOD", pastLiveBadgeStyle
	case media.KindShort:
		return "⚡ SHORT", shortBadgeStyle
	case media.KindPlaylist:
		return "PLAYLIST", playlistBadgeStyle
	default:
		return "", metaStyle
	}
}

func wrapLines(s string, width, maxLines int) []string {
	if width < 1 || maxLines < 1 {
		return nil
	}
	words := strings.Fields(sanitize(strings.NewReplacer("\r", " ", "\n", " ").Replace(s)))
	if len(words) == 0 {
		return nil
	}
	lines := make([]string, 0, maxLines)
	current := ""
	for _, word := range words {
		candidate := word
		if current != "" {
			candidate = current + " " + word
		}
		if lipgloss.Width(candidate) <= width {
			current = candidate
			continue
		}
		if current != "" {
			lines = append(lines, current)
		}
		current = truncate(word, width)
		if len(lines) >= maxLines {
			break
		}
	}
	if len(lines) < maxLines && current != "" {
		lines = append(lines, current)
	}
	if len(lines) == maxLines && len(lines) > 0 && len(words) > 0 {
		joined := strings.Join(lines, " ")
		if len(strings.Fields(joined)) < len(words) {
			lines[len(lines)-1] = truncate(lines[len(lines)-1], max(1, width-1)) + "~"
		}
	}
	if len(lines) > maxLines {
		lines = lines[:maxLines]
	}
	return lines
}

// sanitize removes characters whose terminal width is unreliable (emoji,
// joiners, variation selectors) or that would break the layout (control
// characters). Such characters make borders drift while scrolling.
func clean(s string) string { return sanitize(s) }

func sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\t':
			return ' '
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0):
			return -1
		case r == 0x200d || (r >= 0xfe00 && r <= 0xfe0f) || (r >= 0x2190 && r <= 0x2bff) || r >= 0x1f000:
			return -1
		}
		return r
	}, s)
}

func truncate(s string, width int) string {
	if width < 1 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	if width == 1 {
		return "~"
	}
	runes := []rune(s)
	for len(runes) > 0 && lipgloss.Width(string(runes))+1 > width {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + "~"
}

func padToWidth(s string, width int) string {
	if width <= lipgloss.Width(s) {
		return s
	}
	return s + strings.Repeat(" ", width-lipgloss.Width(s))
}

// tailTruncate keeps the end of s, so text being typed stays visible.
func tailTruncate(s string, width int) string {
	if width < 1 {
		return ""
	}
	s = sanitize(s)
	runes := []rune(s)
	if lipgloss.Width(s) <= width {
		return s
	}
	for len(runes) > 0 && lipgloss.Width(string(runes))+1 > width {
		runes = runes[1:]
	}
	return "~" + string(runes)
}

var _ tea.Model = (*Model)(nil)
