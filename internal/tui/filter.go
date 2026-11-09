package tui

import (
	"slices"
	"strconv"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/text/unicode/norm"

	"github.com/geovannewashington/ytwatch/internal/media"
)

// The filter of the results, opened with /, keeps the results that match what
// is typed. It only narrows the list on screen: the whole list is kept aside,
// so results that arrive while it is on are filtered too, and Esc brings the
// list back. It is not the filter menu (f), which changes the search itself.

// narrowBase is the whole list, kept while a filter is on.
type narrowBase struct {
	items []media.Video
	ranks []int
}

// foldText lowercases s and removes accents, so "cafe" matches "Café".
func foldText(s string) string {
	var b strings.Builder
	for _, r := range norm.NFD.String(s) {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		b.WriteRune(unicode.ToLower(r))
	}

	return b.String()
}

// fuzzyMatch reports whether every word of pattern is in text as a
// subsequence: its letters in order, with anything between them. "grm fst"
// matches "German FAST". An empty pattern matches everything.
func fuzzyMatch(text, pattern string) bool {
	pattern = clean(pattern) // the text is shown cleaned, so it is matched cleaned
	hay := []rune(foldText(text))
	for _, term := range strings.Fields(foldText(pattern)) {
		at := 0
		for _, r := range term {
			for at < len(hay) && hay[at] != r {
				at++
			}
			if at == len(hay) {
				return false
			}
			at++
		}
	}

	return true
}

// narrowText is what a filter looks in: the title and the channel.
func matchText(v media.Video) string {
	return clean(v.Title) + " " + clean(v.ChannelName()) + " " + clean(v.UploaderID)
}

// startNarrow opens the filter line. Without results there is nothing to
// filter, so / edits the search, as it does on the first screen.
func (m *Model) startNarrow() tea.Cmd {
	if len(m.items) == 0 && m.narrowBase == nil {
		m.editSearch()
		return nil
	}

	m.endVisual()
	if m.narrowBase == nil {
		m.narrowBase = &narrowBase{items: slices.Clone(m.items), ranks: slices.Clone(m.ranks)}
	}
	m.searchMode, m.narrowTyping = true, true
	m.setInput(m.narrowText)
	m.status = "Filter the results"

	return nil
}

// editSearch opens the search line with the current query. Ctrl+U clears it.
func (m *Model) editSearch() {
	m.searchMode = true
	m.narrowTyping = false
	m.setInput(m.query)
	m.historyPos = -1
	m.status = "Edit the search"
}

// applyNarrow keeps the results that match text, in their order, and the
// selection on the same result when it is still there.
func (m *Model) applyNarrow(text string) tea.Cmd {
	base := m.narrowBase
	if base == nil {
		return nil
	}

	id := m.selectedID()
	m.narrowText = text
	m.endVisual()

	var items []media.Video
	var ranks []int
	for i, v := range base.items {
		if fuzzyMatch(matchText(v), text) {
			items = append(items, v)
			ranks = append(ranks, base.ranks[i])
		}
	}
	m.items, m.ranks = items, ranks

	m.selected, m.top, m.previewScroll = 0, 0, 0
	for i, v := range items {
		if v.ID == id {
			m.selected = i
			break
		}
	}
	m.keepSelectionVisible()
	m.status = m.resultsStatus()

	return m.scheduleThumbnail()
}

// clearNarrow brings the whole list back, with the selection on the same result.
func (m *Model) clearNarrow() {
	base := m.narrowBase
	if base == nil {
		return
	}

	id := m.selectedID()
	m.items, m.ranks = base.items, base.ranks
	m.narrowBase, m.narrowText, m.narrowTyping = nil, "", false
	m.endVisual()

	m.selected = min(m.selected, max(len(m.items)-1, 0))
	for i, v := range m.items {
		if v.ID == id {
			m.selected = i
			break
		}
	}
	m.trimResults() // results came in while the filter hid the cap
	m.previewScroll = 0
	m.keepSelectionVisible()
}

// handleNarrowInput edits the filter line. It is the search line with another
// meaning: the keys that edit are the same, Up and Down move through the
// results, Enter keeps the filter and Esc removes it.
func (m *Model) handleNarrowInput(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "enter":
		m.searchMode, m.narrowTyping = false, false
		if strings.TrimSpace(m.narrowText) == "" {
			m.clearNarrow()
		}
		m.status = m.resultsStatus()

		return m.scheduleThumbnail()

	case "esc":
		m.clearNarrow()
		m.searchMode = false
		m.status = m.resultsStatus()

		return m.scheduleThumbnail()

	case "up", "ctrl+p":
		m.moveSelection(-1)
		return m.scheduleThumbnail()

	case "down", "ctrl+n":
		m.moveSelection(1)
		return m.scheduleThumbnail()
	}

	before := m.searchInput
	m.handleSearchEdit(msg)
	if m.searchInput != before {
		return m.applyNarrow(m.searchInput)
	}

	return nil
}

// insertIntoNarrowBase adds a result that arrived while a filter is on to the
// whole list, in its place by rank.
func (m *Model) insertIntoNarrowBase(v media.Video, rank int) {
	base := m.narrowBase
	at := len(base.items)
	for i, r := range base.ranks {
		if r > rank {
			at = i
			break
		}
	}
	base.items = slices.Insert(base.items, at, v)
	base.ranks = slices.Insert(base.ranks, at, rank)
	// The cap is applied when the filter is cleared: cutting the whole list here
	// could remove a result that is still on screen.
}

// eachCopy calls change on the result with this id, in the list on screen and in
// the whole list kept aside by a filter.
func (m *Model) eachCopy(id string, change func(*media.Video)) {
	for i := range m.items {
		if m.items[i].ID == id {
			change(&m.items[i])
			break
		}
	}

	if m.narrowBase == nil {
		return
	}
	for i := range m.narrowBase.items {
		if m.narrowBase.items[i].ID == id {
			change(&m.narrowBase.items[i])
			break
		}
	}
}

// narrowStatus is the count line of a list with a filter on: "3 of 50 results".
func (m *Model) narrowStatus() string {
	return strconv.Itoa(len(m.items)) + " of " + plural(len(m.narrowBase.items), "result")
}
