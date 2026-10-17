package tui

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/geovannewashington/ytwatch/internal/clipboard"
	"github.com/geovannewashington/ytwatch/internal/config"
	"github.com/geovannewashington/ytwatch/internal/download"
	"github.com/geovannewashington/ytwatch/internal/history"
	"github.com/geovannewashington/ytwatch/internal/links"
	"github.com/geovannewashington/ytwatch/internal/locale"
	"github.com/geovannewashington/ytwatch/internal/media"
	"github.com/geovannewashington/ytwatch/internal/player"
	"github.com/geovannewashington/ytwatch/internal/preview"
	"github.com/geovannewashington/ytwatch/internal/quality"
	"github.com/geovannewashington/ytwatch/internal/theme"
	"github.com/geovannewashington/ytwatch/internal/youtube"
)

const (
	thumbDebounce     = 140 * time.Millisecond
	animationInterval = 180 * time.Millisecond

	minWidth  = 80
	minHeight = 18
	wideWidth = 100 // below this the preview pane is hidden
	maxWidth  = 160 // wider terminals get empty margins, so the layout keeps its shape
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
	ids []string // everything that was in this mpv: one video, or a queue
	err error
}

type downloadDoneMsg struct {
	id    string
	title string
	path  string
	err   error
}

type animationMsg struct{}

// detailsDelayMsg fires when a row has stayed selected for a moment.
type detailsDelayMsg struct {
	id         string
	generation int
}

// detailsLoadedMsg carries the details of one result looked up on its own.
type detailsLoadedMsg struct {
	generation int
	video      media.Video
}

// noteMark splits a status message from its note. The footer shows the note
// dimmed after the message. sanitize removes control characters from titles,
// so text from the network cannot contain it.
const noteMark = "\x1f"

// undoNote is the note that follows a play or download status.
const undoNote = noteMark + "u, Esc or Ctrl+C: undo"

// undoWindow is how long u can still cancel the last play or download.
const undoWindow = 10 * time.Second

// tryUndo cancels the last play or download if it is still inside the undo
// window. It reports whether it cancelled anything.
func (m *Model) tryUndo() bool {
	if m.undo == nil || time.Now().After(m.undo.until) {
		return false
	}
	m.undo.cancel()
	m.undo = nil
	return true
}

// undoAction is the last play or download, which u cancels until it expires.
type undoAction struct {
	label  string
	until  time.Time
	cancel func()
}

type paneFocus int

const (
	resultsPane paneFocus = iota
	previewPane
)

type Model struct {
	ctx    context.Context
	cancel context.CancelFunc

	query        string
	searchInput  string
	searchCursor int  // position in searchInput, in runes
	searchMode   bool // the search line at the top is active
	history      *history.Store
	historyPos   int    // index in the history while browsing it with Up/Down
	historyDraft string // what was typed before browsing
	showHelp     bool
	margin       int  // empty columns left of the layout when the terminal is wider than maxWidth
	confirmQuit  bool // waiting for y/n after q or Ctrl+C
	undo         *undoAction

	filters      youtube.Filters // applied to every search
	showFilters  bool
	quality      string // see package quality
	showQuality  bool
	visual       bool           // a line selection is active (V)
	parents      []resultsState // the results to go back to from an open playlist
	listTitle    string         // title of the open playlist, empty for search results
	visualAnchor int            // where the selection started
	qualityDraft int
	filterDraft  youtube.Filters // edited in the menu, applied with Enter
	filterCol    int
	searching    bool
	searchDone   bool
	searchError  string
	status       string

	generation   int
	searchCancel context.CancelFunc
	detailsAsked map[string]bool // results whose details were already requested one by one
	searchEvents <-chan youtube.Event

	items         []media.Video
	ranks         []int // ranks[i] is the sort rank of items[i], see youtube.Event.Rank
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

	marqueeID   string
	marqueeStep int
	ticking     bool // a marquee tick is waiting, see needsTick

	themeName string

	// openURL and runCommand open links; tests replace them.
	openURL    func(string, config.Config) error
	runCommand func([]string) error
}

func New(initialQuery string) *Model {
	ctx, cancel := context.WithCancel(context.Background())
	m := &Model{
		ctx: ctx, cancel: cancel,
		themeName:      theme.Default,
		openURL:        player.OpenURL,
		runCommand:     links.Run,
		query:          strings.TrimSpace(initialQuery),
		status:         "",
		thumbnailPaths: make(map[string]string),
		thumbFailures:  make(map[string]time.Time),
		thumbAttempts:  make(map[string]int),
		playing:        make(map[string]string),
		downloading:    make(map[string]string),
		chatOpened:     make(map[string]bool),
		overlay:        &preview.Overlay{},
		cfg:            config.Default(),
		width:          100, height: 30,
		historyPos: -1,
	}
	if m.query == "" {
		m.searchMode = true
		m.setInput("")
		m.status = "Type a query and press Enter"
	}
	return m
}

// SetConfig applies the user configuration; call it before running the program.
func (m *Model) SetConfig(cfg config.Config) {
	m.cfg = cfg
	m.audioOnly = cfg.General.AudioOnly
	m.quality = quality.Normalize(cfg.General.Quality)
	m.overlay.Configure(cfg.Preview.Output)
	m.history = nil
	if cfg.History.Enabled {
		maxAge := time.Duration(cfg.History.MaxAgeDays) * 24 * time.Hour
		m.history = history.Load(history.Path(), maxAge, cfg.History.MaxEntries, time.Now())
	}
	// The config parser already checked the theme, so this only fails for a
	// config built in code; the built-in theme stays then.
	if t, err := theme.Resolve(cfg.Theme.Name, cfg.Theme.Colors); err == nil {
		ApplyTheme(t)
		m.themeName = t.Name
	}
}

func (m *Model) Init() tea.Cmd {
	var cmds []tea.Cmd
	if m.query != "" {
		cmds = append(cmds, m.beginSearch(m.query))
	}
	return tea.Batch(cmds...)
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
	// A leading / is the key that opens the search, typed again by habit. It
	// would change what YouTube finds, so it is not part of the query.
	query = strings.TrimSpace(strings.TrimLeft(query, "/"))
	if query == "" {
		m.searchMode = true
		m.setInput(m.query)
		m.status = "Enter a search query"
		return nil
	}
	filters := m.filters
	loc := m.locale()
	m.query = query
	m.setInput(query)
	m.searchMode = false
	m.parents, m.listTitle = nil, ""
	if m.history != nil {
		m.history.Add(query, time.Now())
	}
	return m.startStream("Searching YouTube...", func(ctx context.Context, emit func(youtube.Event)) {
		youtube.Search(ctx, query, filters, loc, emit)
	})
}

// locale is the search language and region from the config.
func (m *Model) locale() locale.Locale {
	return locale.Locale{Language: m.cfg.Search.Language, Region: m.cfg.Search.Region}
}

// startStream clears the list and starts run, which sends its results to the
// list as events. It stops the stream that was running before.
func (m *Model) startStream(status string, run func(ctx context.Context, emit func(youtube.Event))) tea.Cmd {
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
	m.detailsAsked = map[string]bool{}
	m.generation++
	generation := m.generation
	m.endVisual()
	m.searching = true
	m.searchDone = false
	m.metadataDone = false
	m.searchError = ""
	m.items, m.ranks = nil, nil
	m.selected = 0
	m.top = 0
	m.countPrefix = ""
	m.pendingG = false
	m.thumbSequence++
	m.status = status

	return func() tea.Msg {
		events := make(chan youtube.Event, 256)
		go func() {
			defer close(events)
			run(ctx, func(event youtube.Event) {
				select {
				case events <- event:
				case <-ctx.Done():
				}
			})
		}()
		return searchStartedMsg{generation: generation, events: events}
	}
}

// resultsStatus counts what the list shows.
func (m *Model) resultsStatus() string {
	if m.listTitle != "" {
		return fmt.Sprintf("%d videos"+noteMark+"Esc goes back", len(m.items))
	}
	return fmt.Sprintf("%d results", len(m.items))
}

// resultsState is a list of results that can be returned to.
type resultsState struct {
	items         []media.Video
	ranks         []int
	selected, top int
	title         string
	// partial is set when the results were still arriving when the playlist
	// was opened, which stops them.
	partial bool
}

// openPlaylist lists the videos of playlist v. Esc returns to the results.
func (m *Model) openPlaylist(v media.Video) tea.Cmd {
	url := v.CanonicalURL()
	if url == "" {
		m.status = "This playlist has no link"
		return nil
	}
	m.parents = append(m.parents, resultsState{m.items, m.ranks, m.selected, m.top, m.listTitle, m.searching})
	m.listTitle = clean(v.Title)
	loc := m.locale()
	cmd := m.startStream("Opening playlist..."+noteMark+"Esc goes back", func(ctx context.Context, emit func(youtube.Event)) {
		youtube.Browse(ctx, url, loc, emit)
	})
	return cmd
}

// closePlaylist goes back to the results the playlist was opened from.
func (m *Model) closePlaylist() tea.Cmd {
	if len(m.parents) == 0 {
		return nil
	}
	if m.searchCancel != nil {
		m.searchCancel()
	}
	m.generation++ // late events of the playlist are ignored
	last := m.parents[len(m.parents)-1]
	m.parents = m.parents[:len(m.parents)-1]
	m.items, m.ranks, m.selected, m.top, m.listTitle = last.items, last.ranks, last.selected, last.top, last.title
	m.searching, m.searchDone, m.metadataDone, m.searchError = false, true, true, ""
	m.endVisual()
	m.status = m.resultsStatus()
	if last.partial {
		m.status += noteMark + "the search was stopped early, press r to run it again"
	}
	return m.scheduleThumbnail()
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
	model, cmd := m.update(msg)
	m.syncMarquee()
	if !m.ticking && m.needsTick() {
		m.ticking = true
		cmd = tea.Batch(cmd, tickMarquee())
	}
	return model, cmd
}

func (m *Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case marqueeMsg:
		m.marqueeStep++
		m.ticking = false // Update starts the next tick if it is still needed
		return m, nil
	case tea.WindowSizeMsg:
		m.width, m.height = min(msg.Width, maxWidth), msg.Height
		m.margin = (msg.Width - m.width) / 2
		if m.width < wideWidth {
			m.focus = resultsPane
		}
		m.keepSelectionVisible()
		m.scrollPreview(0)
		if m.width < wideWidth || m.height < minHeight || m.showHelp || m.showFilters || m.showQuality {
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

	case detailsDelayMsg:
		if msg.generation != m.generation || m.selectedID() != msg.id {
			return m, nil
		}
		return m, m.lookUpSelectedDetails()

	case detailsLoadedMsg:
		if msg.generation != m.generation {
			return m, nil
		}
		return m, tea.Batch(m.handleSearchEvent(youtube.Event{Kind: youtube.EventDetails, Video: msg.video})...)

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
		for _, id := range msg.ids {
			delete(m.playing, id)
		}
		if len(m.playing) == 0 {
			m.status = "Playback ended"
		} else {
			m.status = fmt.Sprintf("▶ %d player(s) running", len(m.playing))
		}
		if errors.Is(msg.err, player.ErrCancelled) {
			m.status = "Playback cancelled"
		} else if msg.err != nil {
			m.status = msg.err.Error()
		}
		// The running animation tick keeps itself alive; do not start another.
		return m, nil

	case downloadDoneMsg:
		delete(m.downloading, msg.id)
		switch {
		case errors.Is(msg.err, context.Canceled):
			// Either ytwatch is quitting or u cancelled the download. yt-dlp
			// resumes a partial file next time.
			if m.ctx.Err() == nil {
				m.status = "Download cancelled"
			}
		case msg.err != nil:
			m.status = "Download failed" + noteMark + clean(msg.err.Error())
		default:
			m.status = "✔ Saved" + noteMark + msg.path
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
		if m.confirmQuit {
			if key := msg.String(); key == "y" || key == "Y" {
				m.cancel()
				return m, tea.Quit
			}
			m.confirmQuit = false
			return m, nil
		}
		if msg.String() == "ctrl+c" {
			// Inside the undo window, Ctrl+C cancels the last action first.
			if !m.tryUndo() {
				m.confirmQuit = true
			}
			return m, nil
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
		if m.showFilters {
			return m, m.handleFilterKey(msg)
		}
		if m.showQuality {
			m.handleQualityKey(msg)
			return m, nil
		}
		return m, m.handleKey(msg)
	}
	return m, nil
}

// insertResult adds a result at the place its rank gives, after results of the
// same rank. When it lands above the selection, the selection moves down with
// the result it was on, so the list does not jump under the user.
func (m *Model) insertResult(v media.Video, rank int) []tea.Cmd {
	for len(m.ranks) < len(m.items) {
		m.ranks = append(m.ranks, 1<<30) // items set without a rank go last
	}
	at := len(m.items)
	for i, r := range m.ranks {
		if r > rank {
			at = i
			break
		}
	}
	m.items = slices.Insert(m.items, at, v)
	m.ranks = slices.Insert(m.ranks, at, rank)
	if len(m.items) == 1 {
		m.selected, m.top = 0, 0
		return []tea.Cmd{m.scheduleThumbnail()}
	}
	// The result came in above the anchor of a selection: the rows the
	// selection covers moved down with it.
	if m.visual && at <= m.visualAnchor {
		m.visualAnchor++
	}
	if at <= m.selected && m.selected > 0 {
		m.selected++
		if at < m.top {
			m.top++ // the rows in view moved down, keep showing the same ones
		}
		m.keepSelectionVisible()
	} else if at == 0 {
		// A better first result arrived while the cursor was on the first row.
		return []tea.Cmd{m.scheduleThumbnail()}
	}
	return nil
}

func (m *Model) handleSearchEvent(event youtube.Event) []tea.Cmd {
	switch event.Kind {
	case youtube.EventResult:
		for _, existing := range m.items {
			if existing.ID == event.Video.ID {
				return nil
			}
		}
		return m.insertResult(event.Video, event.Rank)
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
				m.status = "Live stream" + noteMark + "opened YouTube chat"
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
			m.status = "Search failed" + noteMark + "/ to retry"
		} else if len(m.items) == 0 && m.filters.Active() {
			m.status = "No results with these filters" + noteMark + "press f to change them"
		} else if len(m.items) == 0 {
			m.status = "No results" + noteMark + "press / to search"
		} else {
			m.status = m.resultsStatus() + noteMark + "resolving details..."
		}
	case youtube.EventDetailsEnd:
		m.metadataDone = true
		if len(m.items) > 0 && len(m.playing) == 0 {
			m.status = m.resultsStatus()
		}
		return []tea.Cmd{m.lookUpSelectedDetails()}
	}
	return nil
}

// browseHistory moves through the past searches in the search line: -1 goes to
// an older one, +1 to a newer one, and past the newest it brings back what was
// being typed. Editing the text ends the browsing.
func (m *Model) browseHistory(step int) {
	if m.history == nil {
		return
	}
	queries := m.history.Queries()
	if m.historyPos < 0 || m.historyPos > len(queries) {
		m.historyPos = len(queries)
	}
	if m.historyPos == len(queries) {
		m.historyDraft = m.searchInput
	}
	pos := min(max(m.historyPos+step, 0), len(queries))
	if pos == m.historyPos {
		return
	}
	m.historyPos = pos
	if pos == len(queries) {
		m.setInput(m.historyDraft)
	} else {
		m.setInput(queries[pos])
	}
}

// setInput replaces the text of the search line and puts the cursor at its end.
func (m *Model) setInput(text string) {
	m.searchInput = text
	m.searchCursor = len([]rune(text))
}

// handleSearchInput edits the search line. The cursor moves with the arrows,
// Home and End, like in a shell.
func (m *Model) handleSearchInput(msg tea.KeyMsg) tea.Cmd {
	runes := []rune(m.searchInput)
	cursor := min(max(m.searchCursor, 0), len(runes))
	edit := func(from, to int, insert string) {
		out := append(append(append([]rune{}, runes[:from]...), []rune(insert)...), runes[to:]...)
		m.searchInput = string(out)
		m.searchCursor = from + len([]rune(insert))
	}
	if key := msg.String(); key != "up" && key != "down" && key != "ctrl+p" && key != "ctrl+n" {
		m.historyPos = -1 // any other key ends the browsing
	}
	if msg.Paste {
		edit(cursor, cursor, strings.Join(strings.Fields(string(msg.Runes)), " "))
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
	case "up", "ctrl+p":
		m.browseHistory(-1)
		return nil
	case "down", "ctrl+n":
		m.browseHistory(+1)
		return nil
	case "left", "ctrl+b":
		m.searchCursor = max(cursor-1, 0)
	case "right", "ctrl+f":
		m.searchCursor = min(cursor+1, len(runes))
	case "home", "ctrl+a":
		m.searchCursor = 0
	case "end", "ctrl+e":
		m.searchCursor = len(runes)
	case "backspace", "ctrl+h":
		if cursor > 0 {
			edit(cursor-1, cursor, "")
		}
	case "delete", "ctrl+d":
		if cursor < len(runes) {
			edit(cursor, cursor+1, "")
		}
	case "ctrl+u":
		edit(0, cursor, "")
	case "ctrl+k":
		edit(cursor, len(runes), "")
	case "ctrl+w":
		start := cursor
		for start > 0 && unicode.IsSpace(runes[start-1]) {
			start--
		}
		for start > 0 && !unicode.IsSpace(runes[start-1]) {
			start--
		}
		edit(start, cursor, "")
	case " ":
		edit(cursor, cursor, " ")
	default:
		if msg.Type == tea.KeyRunes {
			if len(runes) == 0 && string(msg.Runes) == "/" {
				break // the search is already open, so this / is a habit
			}
			edit(cursor, cursor, string(msg.Runes))
		}
	}
	return nil
}

// openFilters shows the filter menu.
func (m *Model) openFilters() {
	m.showFilters = true
	m.filterDraft = m.filters
	m.filterCol = 0
	m.overlay.Remove()
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
	hasCount := m.countPrefix != ""
	if hasCount {
		if parsed, err := strconv.Atoi(m.countPrefix); err == nil && parsed > 0 {
			count = parsed
		}
	}
	m.countPrefix = ""

	switch key {
	case "q":
		// q first closes the layer the user is in, like Esc does.
		if m.visual {
			m.endVisual()
			m.status = ""
			return nil
		}
		if len(m.parents) > 0 {
			return m.closePlaylist()
		}
		m.confirmQuit = true
		return nil
	case "ctrl+c":
		m.confirmQuit = true
		return nil
	case "/":
		// The current query is there to edit; Ctrl+U clears it.
		m.searchMode = true
		m.setInput(m.query)
		m.historyPos = -1
		m.status = "Edit the search" + noteMark + "Up/Down past searches · Enter runs · Esc cancels"
		return nil
	case "?":
		m.showHelp = true
		m.overlay.Remove()
		return nil
	case "f":
		m.openFilters()
		return nil
	case "Q", "shift+q":
		m.showQuality = true
		m.qualityDraft = max(slices.Index(quality.Levels, m.quality), 0)
		m.overlay.Remove()
		return nil
	case "V", "shift+v":
		if m.visual {
			m.endVisual()
			m.status = ""
		} else {
			m.startVisual()
		}
		return nil
	case "esc":
		if m.tryUndo() {
			return nil
		}
		if m.visual {
			m.endVisual()
			m.status = ""
			return nil
		}
		if len(m.parents) > 0 {
			return m.closePlaylist()
		}
		m.confirmQuit = true
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
	case "t":
		next := theme.Next(m.themeName)
		ApplyTheme(next)
		m.themeName = next.Name
		m.status = "Theme: " + next.Name + noteMark + "this session only, set [theme] name in config.toml to keep it"
		return nil
	case "a":
		m.audioOnly = !m.audioOnly
		if m.audioOnly {
			m.status = "Audio-only mode on" + noteMark + "applies to playing and downloading"
		} else {
			m.status = "Audio-only mode off"
		}
		return nil
	case "B":
		v, ok := m.selectedVideo()
		if !ok || !v.IsPlaylist() {
			m.status = "B opens a playlist to browse its videos"
			return nil
		}
		return m.openPlaylist(v)
	case "d":
		return m.downloadSelected()
	case "u":
		if !m.tryUndo() {
			m.status = "Nothing to undo"
		}
		return nil
	case "o":
		return m.openLink(hasCount, count)
	case "y":
		return m.copyLink(hasCount, count)
	}
	return nil
}

// linkTarget is what o and y act on: the selected video's URL, or link n of
// its description when a count was typed (3o). ok is false when there is
// nothing to act on, with the reason in the status line.
func (m *Model) linkTarget(hasCount bool, n int) (url string, ok bool) {
	v, found := m.selectedVideo()
	if !found {
		return "", false
	}
	if !hasCount {
		if url = v.CanonicalURL(); url == "" {
			m.status = "This result has no URL"
			return "", false
		}
		return url, true
	}
	_, urls := links.Annotate(strings.TrimSpace(v.Description))
	switch {
	case n <= len(urls):
		return urls[n-1], true
	case len(urls) == 0:
		m.status = fmt.Sprintf("No link %d"+noteMark+"this description has no links", n)
	default:
		m.status = fmt.Sprintf("No link %d"+noteMark+"this description has %d links", n, len(urls))
	}
	return "", false
}

// openLink opens the video in the browser, or description link n with the
// program configured for its kind (3o), falling back to the browser.
func (m *Model) openLink(hasCount bool, n int) tea.Cmd {
	url, ok := m.linkTarget(hasCount, n)
	if !ok {
		return nil
	}
	if hasCount {
		if argv, name, matched := links.Command(url, m.cfg.Links); matched {
			if err := m.runCommand(argv); err != nil {
				m.status = fmt.Sprintf("Link %d"+noteMark+"%v", n, err)
				return nil
			}
			m.status = fmt.Sprintf("Opened link %d"+noteMark+"%s", n, name)
			return nil
		}
	}
	if err := m.openURL(url, m.cfg); err != nil {
		m.status = err.Error()
		return nil
	}
	if hasCount {
		m.status = fmt.Sprintf("Opened link %d in the browser", n)
	} else {
		m.status = "Opened the video in the browser"
	}
	return nil
}

// copyLink copies the video URL, or description link n (3y).
func (m *Model) copyLink(hasCount bool, n int) tea.Cmd {
	url, ok := m.linkTarget(hasCount, n)
	if !ok {
		return nil
	}
	if err := clipboard.Copy(url); err != nil {
		m.status = clean(err.Error())
		return nil
	}
	if hasCount {
		m.status = fmt.Sprintf("Copied link %d", n)
	} else {
		m.status = "Copied video URL"
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

// detailsDelay is how long a row stays selected before its details are looked
// up, so scrolling past rows does not start a yt-dlp run for each.
const detailsDelay = 400 * time.Millisecond

// scheduleThumbnail is called whenever the selection may have changed. It
// schedules the thumbnail and, for a row without details, its detail lookup.
func (m *Model) scheduleThumbnail() tea.Cmd {
	cmds := []tea.Cmd{m.scheduleThumbnailLoad()}
	if v, ok := m.selectedVideo(); ok && !v.Detailed {
		id, generation := v.ID, m.generation
		cmds = append(cmds, tea.Tick(detailsDelay, func(time.Time) tea.Msg {
			return detailsDelayMsg{id: id, generation: generation}
		}))
	}
	return tea.Batch(cmds...)
}

// lookUpSelectedDetails fetches the details of the selected row when the
// search did not. The search itself only looks up the first rows, to keep the
// CPU use of a search low. It waits until the search has finished its own
// lookups, so the same result is not fetched twice.
func (m *Model) lookUpSelectedDetails() tea.Cmd {
	v, ok := m.selectedVideo()
	if !ok || v.Detailed || !m.metadataDone || m.detailsAsked[v.ID] {
		return nil
	}
	if m.detailsAsked == nil {
		m.detailsAsked = map[string]bool{}
	}
	m.detailsAsked[v.ID] = true
	ctx, generation, loc := m.ctx, m.generation, m.locale()
	return func() tea.Msg {
		var got media.Video
		_ = youtube.FetchDetailsBatch(ctx, []media.Video{v}, loc, func(d media.Video) { got = d })
		if got.ID == "" {
			return nil
		}
		return detailsLoadedMsg{generation: generation, video: got}
	}
}

func (m *Model) scheduleThumbnailLoad() tea.Cmd {
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
	if !ok || m.width < wideWidth || !m.thumbnailsOn() {
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
	description, _ = links.Annotate(description)
	_, width := m.paneWidths()
	// PREVIEW, thumbnail rows, blank, title, channel, stats, blank, DESCRIPTION.
	baseLines := m.thumbRows() + 7
	if section, _ := m.streamSection(v, width); len(section) > 0 {
		baseLines += 1 + len(section) // blank separator + section lines
	}
	visible := max(1, m.bodyHeight()-baseLines)
	return max(0, len(wrapDescription(description, width))-visible)
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

// thumbnailsOn reports whether thumbnails can be drawn: they are not switched
// off in the config and ueberzugpp is installed.
func (m *Model) thumbnailsOn() bool {
	return !m.overlay.Disabled() && preview.Available()
}

// thumbRows is the height reserved for the thumbnail. It is zero when
// thumbnails are switched off, so the preview text uses the space.
func (m *Model) thumbRows() int {
	if m.overlay.Disabled() {
		return 0
	}
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
	if !m.overlay.Show(path, m.margin+leftWidth+2, y, rightWidth, thumbRows) {
		m.status = "Thumbnail overlay unavailable" + noteMark + "check ueberzugpp or set [preview] output in config.toml"
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

// playSelected plays the current result in mpv, or all of a visual selection
// as one queue.
func (m *Model) playSelected() tea.Cmd {
	if m.visual {
		return m.playQueue()
	}
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
	return m.startPlayer([]media.Video{v})
}

// playQueue plays the visual selection in one mpv, which steps through it
// like a playlist (< and > in mpv).
func (m *Model) playQueue() tea.Cmd {
	videos, skip := m.selectedVideos(false)
	m.endVisual()
	var fresh []media.Video
	for _, v := range videos {
		if _, exists := m.playing[v.ID]; !exists {
			fresh = append(fresh, v)
		}
	}
	if len(fresh) == 0 {
		m.status = "Nothing to play in the selection"
		if text := skip.text(); text != "" {
			m.status += noteMark + text
		}
		return nil
	}
	cmd := m.startPlayer(fresh)
	if text := skip.text(); text != "" && cmd != nil {
		m.status += noteMark + text
	}
	return cmd
}

// startPlayer opens videos in one mpv. A single live stream also opens its chat.
func (m *Model) startPlayer(videos []media.Video) tea.Cmd {
	cmd, err := player.Start(videos, m.cfg, player.Options{AudioOnly: m.audioOnly, Quality: m.quality})
	if err != nil {
		m.status = err.Error()
		return nil
	}
	ids := make([]string, len(videos))
	for i, v := range videos {
		ids[i] = v.ID
		m.playing[v.ID] = v.Title
	}
	if len(videos) > 1 {
		m.status = fmt.Sprintf("▶ Playing a queue of %d"+noteMark+"< and > in mpv skip", len(videos))
	} else {
		v := videos[0]
		m.status = "▶ Playing" + noteMark + clean(v.Title)
		if v.IsCurrentlyLive() {
			if err := player.OpenYouTubeChat(v, m.cfg); err == nil {
				m.chatOpened[v.ID] = true
				m.status = "Live stream" + noteMark + "opened chat"
			} else {
				m.status = "Live stream" + noteMark + err.Error()
			}
		}
	}
	m.undo = &undoAction{label: "playback", until: time.Now().Add(undoWindow), cancel: cmd.Cancel}
	if !videos[0].IsCurrentlyLive() || len(videos) > 1 {
		m.status += undoNote
	}
	cmds := []tea.Cmd{waitForPlayer(cmd, ids)}
	if !m.animating {
		m.animating = true
		cmds = append(cmds, tickAnimation())
	}
	return tea.Batch(cmds...)
}

// downloadSelected downloads the current result, or all of a visual selection
// one after the other.
func (m *Model) downloadSelected() tea.Cmd {
	if !m.visual {
		v, ok := m.selectedVideo()
		if !ok {
			m.status = "No result selected"
			return nil
		}
		if v.IsPlaylist() {
			m.status = "Open the playlist (B), then select videos with V to download them"
			return nil
		}
		if v.IsCurrentlyLive() || v.IsUpcoming() {
			m.status = "Live and upcoming streams cannot be downloaded"
			return nil
		}
		return m.startDownloads([]media.Video{v})
	}
	videos, skip := m.selectedVideos(true)
	m.endVisual()
	cmd := m.startDownloads(videos)
	if cmd == nil {
		m.status = "Nothing to download in the selection"
	}
	if text := skip.text(); text != "" {
		m.status += noteMark + text
	}
	return cmd
}

// startDownloads downloads videos one after the other, so a long selection
// does not open many connections at once.
func (m *Model) startDownloads(videos []media.Video) tea.Cmd {
	var cmds []tea.Cmd
	var last media.Video
	// One context for the batch, so u cancels all of it.
	ctx, stop := context.WithCancel(m.ctx)
	cfg := m.cfg
	opts := download.Options{AudioOnly: m.audioOnly, Quality: m.quality}
	for _, v := range videos {
		if _, exists := m.downloading[v.ID]; exists {
			m.status = "Already downloading: " + clean(v.Title)
			continue
		}
		m.downloading[v.ID] = v.Title
		last = v
		cmds = append(cmds, func() tea.Msg {
			path, err := download.Run(ctx, v, cfg, opts)
			return downloadDoneMsg{id: v.ID, title: v.Title, path: path, err: err}
		})
	}
	if len(cmds) == 0 {
		stop()
		return nil
	}
	m.undo = &undoAction{label: "download", until: time.Now().Add(undoWindow), cancel: stop}
	if len(cmds) == 1 {
		m.status = "⬇ Downloading" + noteMark + clean(last.Title)
	} else {
		m.status = fmt.Sprintf("⬇ Downloading %d videos, one after the other", len(cmds))
	}
	m.status += undoNote
	return tea.Sequence(cmds...)
}

func waitForPlayer(cmd *player.Process, ids []string) tea.Cmd {
	return func() tea.Msg {
		return playerExitedMsg{ids: ids, err: cmd.Wait()}
	}
}

func tickAnimation() tea.Cmd {
	return tea.Tick(animationInterval, func(time.Time) tea.Msg { return animationMsg{} })
}

// View draws the interface, centered when the terminal is wider than maxWidth.
func (m *Model) View() string {
	view := m.layout()
	if m.margin > 0 {
		view = lipgloss.NewStyle().PaddingLeft(m.margin).Render(view)
	}
	return view
}

func (m *Model) layout() string {
	if m.width < 1 || m.height < 1 {
		return "ytwatch | starting..."
	}
	if m.width < minWidth || m.height < minHeight {
		return fmt.Sprintf("ytwatch needs a terminal at least %dx%d\nResize the terminal or press q to quit.", minWidth, minHeight)
	}

	if m.showHelp {
		return m.renderHelp()
	}
	if m.showFilters {
		return m.renderFilters()
	}
	if m.showQuality {
		return m.renderQuality()
	}
	return lipgloss.JoinVertical(lipgloss.Left, m.renderHeader(), m.renderBody(), m.renderFooter())
}

func (m *Model) renderHeader() string {
	brand := titleStyle.Render("YTWATCH") + searchStyle.Render(" ✦ ") + taglineStyle.Render("Search YouTube from your terminal and play in mpv")
	searchLine := searchStyle.Render("> ")
	switch {
	case m.searchMode:
		searchLine += renderInput(m.searchInput, m.searchCursor, max(10, m.width-8))
	case m.query == "":
		searchLine += helpStyle.Render("press / to search videos, Shorts and live streams")
	case m.listTitle != "":
		room := max(10, m.width-8)
		searchLine += truncate(m.query, room/3) + metaStyle.Render(" › ") +
			playlistBadgeStyle.Render(playlistMark+" ") + truncate(m.listTitle, room*2/3-6)
	default:
		searchLine += truncate(m.query, max(10, m.width-8))
	}
	keys := "j/k move · h/l panes · Enter play · / search · f filters · ? help · q quit"
	if len(m.parents) > 0 && !m.searchMode {
		keys = "j/k move · Enter play · V select · Esc back to results · ? help · q quit"
	}
	if m.searchMode {
		keys = "Enter search · Esc cancel · Up/Down past searches · arrows move · Ctrl+U clear"
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
		if m.inSelection(i) {
			titleStyle = selectedTitleStyle
			if !selected {
				cursor = "┃ "
			}
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
		rowTitle := clean(v.Title)
		if selected {
			rowTitle = m.marqueeText(rowTitle, max(1, titleWidth))
		} else {
			rowTitle = truncate(rowTitle, max(1, titleWidth))
		}
		title := padToWidth(rowTitle, titleWidth)
		line := marker + prefix + titleStyle.Render(title)
		if badge != "" {
			line += " " + badge
		}
		lines = append(lines, line)

		meta := clean(v.ChannelName()) + " · " + v.DurationText()
		if v.IsPlaylist() {
			meta = strings.Join(append([]string{clean(v.ChannelName()), "playlist"}, playlistFacts(v)...), " · ")
		} else if v.IsCurrentlyLive() {
			meta = clean(v.ChannelName()) + " · streaming now"
			if v.ConcurrentViewers > 0 {
				meta += " · " + v.ViewsText()
			}
		} else if v.ViewCount > 0 {
			meta += " · " + v.ViewsText()
		}
		age := v.AgeText(time.Now())
		if age != "date n/a" && !v.IsPlaylist() {
			meta += " · " + age
		}
		meta = truncate(meta, width-2)
		if selected {
			lines = append(lines, selectedMetaStyle.Render(padToWidth("  "+meta, width)))
		} else {
			lines = append(lines, metaStyle.Render("  "+meta))
		}
	}

	if len(m.items) == 0 {
		lines = append(lines, m.renderEmptyList(width, height)...)
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

// renderEmptyList is what the results pane shows without results: Watchy and a
// line that says what is going on. In a pane too small for him it is just the
// line.
func (m *Model) renderEmptyList(width, height int) []string {
	message, mood := "Press / to search YouTube", moodIdle
	switch {
	case m.searchError != "":
		message, mood = "Search failed: "+m.searchError, moodSad
	case m.searching && len(m.parents) > 0:
		message, mood = "Opening the playlist...", moodSearching
	case m.searching:
		message, mood = "Searching videos, Shorts and live streams...", moodSearching
	case m.searchDone:
		message, mood = "No results. Press / to search again or f to change the filters", moodSad
	}
	caption := subtitleStyle.Render(truncate(message, width))
	if height < mascotHeight+4 || width < mascotWidth+2 {
		return []string{caption}
	}
	var lines []string
	for i := 0; i < (height-mascotHeight-3)/3; i++ {
		lines = append(lines, "")
	}
	lines = append(lines, renderMascot(width, mood, m.marqueeStep)...)
	lines = append(lines, "", lipgloss.PlaceHorizontal(width, lipgloss.Center, caption))
	return lines
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
		return append(lines, "Enter plays the full playlist · B browses it"), playlistBadgeStyle
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
		lines = append(lines, m.marqueeText(clean(v.Title), width))
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
		lines = append(lines, metaStyle.Render(truncate(strings.Join(stats, " · "), width)))
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
		} else {
			description, _ = links.Annotate(description)
		}
		lines = append(lines, "", panelTitleStyle.Render("DESCRIPTION"))
		descLines := wrapDescription(description, width)
		available := max(0, height-len(lines))
		maxScroll := max(0, len(descLines)-available)
		scroll := min(max(0, m.previewScroll), maxScroll)
		for _, line := range descLines[scroll:min(len(descLines), scroll+available)] {
			lines = append(lines, descriptionStyle.Render(highlightLinkMarkers(truncate(line, width))))
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
	if m.visual {
		status = m.visualStatus()
	}
	style := statusStyle
	if m.searchError != "" && len(m.items) == 0 {
		status = m.searchError
		style = errorStyle
		prefix = ""
	}
	if m.confirmQuit {
		status = "Quit? y/n"
		style = shortBadgeStyle // yellow in every theme
		prefix = ""
	}
	tags := ""
	if summary := m.filters.Summary(); summary != "" {
		tags += truncate(strings.ReplaceAll(summary, ", ", " · "), max(10, m.width/3)) + "  "
	}
	if m.quality != "" && m.quality != quality.Best {
		tags += m.quality + "  "
	}
	if m.audioOnly {
		tags += "♪ audio  "
	}
	if len(m.downloading) > 0 {
		tags += fmt.Sprintf("⬇ %d  ", len(m.downloading))
	}
	right := footerStyle.Render(tags + fmt.Sprintf("%d/%d", min(m.selected+1, len(m.items)), len(m.items)))
	leftWidth := max(10, m.width-lipgloss.Width(right)-2-lipgloss.Width(prefix))
	left := prefix + renderStatus(status, style, leftWidth)
	return left + "  " + right
}

// handleFilterKey edits the filter draft. The menu has one column per filter:
// h/l change the column and j/k pick its option. Enter applies the draft and
// searches again.
func (m *Model) handleFilterKey(msg tea.KeyMsg) tea.Cmd {
	columns := len(youtube.Rows)
	switch msg.String() {
	case "l", "right", "tab":
		m.filterCol = (m.filterCol + 1) % columns
	case "h", "left", "shift+tab":
		m.filterCol = (m.filterCol + columns - 1) % columns
	case "j", "down", "ctrl+n", " ":
		m.filterDraft.Set(m.filterCol, m.filterDraft.Get(m.filterCol)+1)
	case "k", "up", "ctrl+p":
		m.filterDraft.Set(m.filterCol, m.filterDraft.Get(m.filterCol)-1)
	case "x":
		m.filterDraft = youtube.Filters{}
	case "enter":
		m.showFilters = false
		m.filters = m.filterDraft
		if m.query == "" {
			m.status = "Filters set" + noteMark + "press / to search"
			return nil
		}
		return m.beginSearch(m.query)
	case "esc", "q", "f":
		m.showFilters = false
		m.showSelectedThumbnail()
	}
	return nil
}

// handleQualityKey moves in the quality menu. Enter picks the level for playing
// and downloading until ytwatch is closed.
func (m *Model) handleQualityKey(msg tea.KeyMsg) {
	n := len(quality.Levels)
	switch msg.String() {
	case "j", "down", "tab", "ctrl+n":
		m.qualityDraft = (m.qualityDraft + 1) % n
	case "k", "up", "shift+tab", "ctrl+p":
		m.qualityDraft = (m.qualityDraft + n - 1) % n
	case "enter":
		m.showQuality = false
		m.quality = quality.Levels[m.qualityDraft]
		m.status = "Quality: " + m.quality + noteMark + "applies to playing and downloading, set [general] quality to keep it"
		m.showSelectedThumbnail()
	case "esc", "q", "Q":
		m.showQuality = false
		m.showSelectedThumbnail()
	}
}

// renderQuality draws the quality menu.
func (m *Model) renderQuality() string {
	var b strings.Builder
	b.WriteString(panelTitleStyle.Render("VIDEO QUALITY") + "\n\n")
	for i, level := range quality.Levels {
		label := "Up to " + level
		if level == quality.Best {
			label = "Best available"
		}
		switch {
		case i == m.qualityDraft:
			b.WriteString(selectedTitleStyle.Render("● "+label) + "\n")
		default:
			b.WriteString(helpStyle.Render("○ "+label) + "\n")
		}
	}
	b.WriteString("\n" + metaStyle.Render("Applies to playing and downloading.") + "\n")
	b.WriteString(helpStyle.Render("j/k option · Enter choose · Esc cancel"))
	box := helpBoxStyle.Render(b.String())
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

// filterColumnWidth is the width of one column of the filter menu.
const filterColumnWidth = 19

// renderFilters draws the filter menu as one column per filter, like the
// filter menu of youtube.com. The chosen option of each column is marked.
func (m *Model) renderFilters() string {
	height := 0
	for _, def := range youtube.Rows {
		height = max(height, len(def.Options))
	}
	columns := make([]string, len(youtube.Rows))
	for col, def := range youtube.Rows {
		focused := col == m.filterCol
		head, rule := helpStyle, helpStyle
		if focused {
			head, rule = panelTitleStyle, searchStyle
		}
		lines := []string{
			head.Render(strings.ToUpper(def.Name)),
			rule.Render(strings.Repeat("─", filterColumnWidth-3)),
		}
		for i := 0; i < height; i++ {
			switch {
			case i >= len(def.Options):
				lines = append(lines, "")
			case i == m.filterDraft.Get(col) && focused:
				lines = append(lines, selectedTitleStyle.Render("● "+def.Options[i]))
			case i == m.filterDraft.Get(col):
				lines = append(lines, "● "+def.Options[i])
			default:
				lines = append(lines, helpStyle.Render("○ "+def.Options[i]))
			}
		}
		columns[col] = lipgloss.NewStyle().Width(filterColumnWidth).Render(strings.Join(lines, "\n"))
	}
	var b strings.Builder
	b.WriteString(panelTitleStyle.Render("SEARCH FILTERS") + "\n\n")
	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, columns...) + "\n\n")
	// The help is always one line, so the box keeps its size between columns.
	help := youtube.Rows[m.filterCol].Help
	if m.filterCol == youtube.RowDate && m.filterDraft.DateIgnored() {
		help = "YouTube has no date filter for Shorts, so it is ignored."
	}
	b.WriteString(metaStyle.Render(truncate(help, filterColumnWidth*len(columns))) + "\n")
	b.WriteString(helpStyle.Render("j/k option · h/l column · Enter search · x reset · Esc cancel"))
	box := helpBoxStyle.Render(b.String())
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

func (m *Model) renderHelp() string {
	// Only the main key for each action: the other ways that exist (w/b,
	// Tab, PageUp and so on) are left out on purpose.
	rows := [][2]string{
		{"j k", "Move through the results"},
		{"h l", "Switch between the results and the preview"},
		{"Enter  B", "Play in mpv (a whole playlist too) / browse a playlist"},
		{"Esc", "Go back, or quit from the main page"},
		{"/  r", "Edit the search (Up/Down: past ones) / repeat it"},
		{"f", "Filters: type, duration, upload date"},
		{"V", "Select lines: Enter plays them, d downloads them"},
		{"d  a  Q", "Download / audio only / video quality"},
		{"u", "Undo a play or download within 10s (Esc, Ctrl+C too)"},
		{"o  y", "Open / copy a link (3o: description link 3)"},
		{"t", "Next color theme"},
		{"?  q", "Close this help / quit"},
	}
	notes := []string{"Vim motions work too: gg, G, 5j, Ctrl+d and so on."}
	// Try the roomiest layout first and drop notes, padding and the blank
	// line under the title until the box fits the terminal height.
	layouts := []struct {
		notes, titleGap bool
		pad             int
	}{{true, true, 1}, {false, true, 1}, {false, true, 0}, {false, false, 0}}
	var box string
	for _, layout := range layouts {
		var b strings.Builder
		b.WriteString(panelTitleStyle.Render("KEYS") + "\n")
		if layout.titleGap {
			b.WriteString("\n")
		}
		for i, row := range rows {
			b.WriteString("  " + searchStyle.Render(padToWidth(row[0], 10)) + helpStyle.Render(row[1]))
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

var linkMarker = regexp.MustCompile(`\[\d+\] https?://`)

// highlightLinkMarkers colours the "[n]" in front of each numbered link.
func highlightLinkMarkers(line string) string {
	return linkMarker.ReplaceAllStringFunc(line, func(found string) string {
		end := strings.IndexByte(found, ']') + 1
		return keyStyle.Render(found[:end]) + found[end:]
	})
}

var bareMarker = regexp.MustCompile(`^\[\d+\]$`)

// joinLinkMarkers keeps a "[n]" marker and the link after it as one word, so
// a line never breaks between them.
func joinLinkMarkers(words []string) []string {
	out := make([]string, 0, len(words))
	for i := 0; i < len(words); i++ {
		if bareMarker.MatchString(words[i]) && i+1 < len(words) && strings.HasPrefix(words[i+1], "http") {
			out = append(out, words[i]+" "+words[i+1])
			i++
			continue
		}
		out = append(out, words[i])
	}
	return out
}

// wrapDescription wraps a description to width, keeping the line breaks and
// blank lines the author wrote, like YouTube does. Several blank lines in a
// row show as one, and blank lines at the start and end are dropped.
func wrapDescription(text string, width int) []string {
	text = strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(text)
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		wrapped := wrapLines(line, width, 1000)
		if len(wrapped) == 0 {
			if len(lines) > 0 && lines[len(lines)-1] != "" {
				lines = append(lines, "")
			}
			continue
		}
		lines = append(lines, wrapped...)
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func wrapLines(s string, width, maxLines int) []string {
	if width < 1 || maxLines < 1 {
		return nil
	}
	words := joinLinkMarkers(strings.Fields(sanitize(strings.NewReplacer("\r", " ", "\n", " ").Replace(s))))
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

// renderInput draws the input line with a block cursor, scrolled sideways so
// that the cursor stays inside width columns. The text is made safe to print
// in parts, so characters that are removed do not move the cursor.
func renderInput(text string, cursor, width int) string {
	before, under, after := inputParts(text, cursor)
	for len(before) > 0 && lipgloss.Width(string(before))+1 > width {
		before = before[1:]
	}
	room := width - lipgloss.Width(string(before)) - 1
	for len(after) > 0 && lipgloss.Width(string(after)) > room {
		after = after[:len(after)-1]
	}
	cell := " "
	if len(under) > 0 {
		cell = string(under)
	}
	return string(before) + lipgloss.NewStyle().Reverse(true).Render(cell) + string(after)
}

// inputParts splits the input at the cursor into the text before it, the
// character under it and the text after it. Each part is made safe to print
// on its own, so characters that are removed do not move the cursor.
func inputParts(text string, cursor int) (before, under, after []rune) {
	all := []rune(text)
	cursor = min(max(cursor, 0), len(all))
	before = []rune(sanitize(string(all[:cursor])))
	if cursor < len(all) {
		under = []rune(sanitize(string(all[cursor])))
		after = []rune(sanitize(string(all[cursor+1:])))
	}
	return before, under, after
}

// renderStatus draws a status message in style, followed by its note (the text
// after noteMark) dimmed, and pads the result to width.
func renderStatus(status string, style lipgloss.Style, width int) string {
	message, note, hasNote := strings.Cut(status, noteMark)
	message = truncate(message, width)
	used := lipgloss.Width(message)
	out := style.Render(message)
	if hasNote && note != "" {
		if room := width - used - 2; room > 0 {
			note = "  " + truncate(note, room)
			out += metaStyle.Render(note)
			used += lipgloss.Width(note)
		}
	}
	return out + strings.Repeat(" ", max(0, width-used))
}
