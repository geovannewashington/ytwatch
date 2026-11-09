package tui

import (
	"fmt"
	"strings"

	"github.com/geovannewashington/ytwatch/internal/media"
)

// Visual mode works like Neovim's line mode: V starts a selection at the
// current result, moving extends it, and Enter or d acts on all of it.

// startVisual begins a selection at the current result.
func (m *Model) startVisual() {
	if len(m.items) == 0 {
		m.status = "Nothing to select"
		return
	}
	m.visual = true
	m.visualAnchor = m.selected
}

func (m *Model) endVisual() { m.visual = false }

// selection returns the first and last selected index. Without visual mode it
// is just the current result.
func (m *Model) selection() (int, int) {
	if !m.visual {
		return m.selected, m.selected
	}
	return min(m.visualAnchor, m.selected), max(m.visualAnchor, m.selected)
}

// inSelection reports whether result i is part of a visual selection.
func (m *Model) inSelection(i int) bool {
	if !m.visual {
		return false
	}
	lo, hi := m.selection()
	return i >= lo && i <= hi
}

// skipped counts the results left out of a selection, by reason.
type skipped struct {
	playlists, channels, live, unplayable int
}

func (s skipped) total() int { return s.playlists + s.channels + s.live + s.unplayable }

// text says what was skipped, such as "skipped 2 playlists, 1 live stream".
func (s skipped) text() string {
	var parts []string
	if s.playlists > 0 {
		parts = append(parts, plural(s.playlists, "playlist"))
	}
	if s.channels > 0 {
		parts = append(parts, plural(s.channels, "channel"))
	}
	if s.live > 0 {
		parts = append(parts, plural(s.live, "live stream"))
	}
	if s.unplayable > 0 {
		parts = append(parts, plural(s.unplayable, "result")+" without a link")
	}

	if len(parts) == 0 {
		return ""
	}

	return "skipped " + strings.Join(parts, ", ")
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// selectedVideos returns the selected results that can be acted on. For
// playing, a queue cannot hold playlists or streams that have not started. For
// downloading, live streams are left out as well. A single result is never
// filtered here, so it keeps its own messages.
func (m *Model) selectedVideos(forDownload bool) ([]media.Video, skipped) {
	lo, hi := m.selection()

	var out []media.Video
	var skip skipped
	for i := lo; i <= hi && i < len(m.items); i++ {
		v := m.items[i]
		switch {
		case v.IsPlaylist():
			skip.playlists++
		case v.IsChannel():
			skip.channels++
		case v.CanonicalURL() == "":
			skip.unplayable++
		case v.IsUpcoming() || (forDownload && v.IsCurrentlyLive()):
			skip.live++
		default:
			out = append(out, v)
		}
	}

	return out, skip
}

// visualStatus is the footer text while a selection is active.
func (m *Model) visualStatus() string {
	lo, hi := m.selection()
	text := "-- VISUAL -- " + fmt.Sprintf("%d selected", hi-lo+1)
	if _, skip := m.selectedVideos(false); skip.playlists+skip.channels > 0 {
		text += " (" + skip.text() + ")"
	}

	return text
}
