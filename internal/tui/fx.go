package tui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Effects are short animations that show what the program is doing. They share
// one clock, fxMsg, which ticks only while an effect is running and stops when
// none is. At rest nothing ticks, so an idle ytwatch uses no CPU.
//
// The [ui] animations setting picks how many run: off, minimal (the ones that
// show progress: spinner, undo bar, download bar) or full (all of them).
const (
	animOff     = 0
	animMinimal = 1
	animFull    = 2

	fxFast = 80 * time.Millisecond  // clock speed while something moves
	fxSlow = 500 * time.Millisecond // clock speed while only a slow pulse runs

	// fadeStep is how long each color of a fade lasts: bright, then medium,
	// then normal.
	fadeStep = 120 * time.Millisecond
	// rowStagger delays each new row a little after the one before it, so a
	// burst of results appears one after the other. Only the first rows count.
	rowStagger     = 30 * time.Millisecond
	maxStaggerRows = 16
	// splitDuration is how long the preview pane takes to open.
	splitDuration = 240 * time.Millisecond
)

type fxMsg struct{}

// fxState is what the effects need to remember.
type fxState struct {
	ticking bool

	arrived  map[string]time.Time // when each new row starts to fade in
	arrivals int                  // rows that arrived in this search

	curID, prevID string // the selected row and the one before it
	prevAt        time.Time

	status   string // the last status, to see when it changes
	statusAt time.Time

	splitAt time.Time // when the preview pane started to open
}

func (m *Model) now() time.Time {
	if m.clock != nil {
		return m.clock()
	}
	return time.Now()
}

// fxObserve notices changes the effects react to. Update calls it after every
// message.
func (m *Model) fxObserve() {
	if m.animations < animFull {
		return
	}
	now := m.now()
	if id := m.selectedID(); id != m.fx.curID {
		if m.fx.curID != "" {
			m.fx.prevID, m.fx.prevAt = m.fx.curID, now
		}
		m.fx.curID = id
	}
	if m.status != m.fx.status {
		m.fx.status, m.fx.statusAt = m.status, now
	}
}

// fxSchedule starts the clock when an effect needs it and it is not running.
func (m *Model) fxSchedule() tea.Cmd {
	if m.fx.ticking {
		return nil
	}
	fast, slow := m.fxWant()
	if !fast && !slow {
		return nil
	}
	interval := fxSlow
	if fast {
		interval = fxFast
	}
	m.fx.ticking = true
	return tea.Tick(interval, func(time.Time) tea.Msg { return fxMsg{} })
}

// fxWant reports whether some effect moves quickly, or only a slow one runs.
func (m *Model) fxWant() (fast, slow bool) {
	if m.animations == animOff {
		return false, false
	}
	now := m.now()
	if m.searching && !m.searchMode || len(m.downloading) > 0 {
		fast = true
	}
	if m.animations < animFull {
		return fast, false
	}
	for _, start := range m.fx.arrived {
		if now.Before(start.Add(2 * fadeStep)) {
			fast = true
			break
		}
	}
	if m.fx.prevID != "" && now.Sub(m.fx.prevAt) < 2*fadeStep {
		fast = true
	}
	if m.status != "" && now.Sub(m.fx.statusAt) < 2*fadeStep {
		fast = true
	}
	if m.splitAnimating() {
		fast = true
	}
	return fast, !fast && m.liveVisible()
}

// fxExpire forgets effects that are over. The clock message calls it.
func (m *Model) fxExpire() {
	now := m.now()
	for id, start := range m.fx.arrived {
		if now.After(start.Add(2 * fadeStep)) {
			delete(m.fx.arrived, id)
		}
	}
	if !m.fx.splitAt.IsZero() && now.Sub(m.fx.splitAt) >= splitDuration {
		m.fx.splitAt = time.Time{}
		m.showSelectedThumbnail() // it waited for the pane to finish opening
	}
}

// fadePhase turns the time since a row appeared into a step: -1 before it
// appears, 0 bright, 1 medium, 2 normal.
func fadePhase(age time.Duration) int {
	switch {
	case age < 0:
		return -1
	case age < fadeStep:
		return 0
	case age < 2*fadeStep:
		return 1
	}
	return 2
}

// markArrived starts the fade-in of a new row.
func (m *Model) markArrived(id string) {
	if m.animations < animFull {
		return
	}
	if m.fx.arrived == nil {
		m.fx.arrived = map[string]time.Time{}
	}
	delay := time.Duration(min(m.fx.arrivals, maxStaggerRows)) * rowStagger
	m.fx.arrivals++
	m.fx.arrived[id] = m.now().Add(delay)
}

// resetArrivals runs when a new search starts.
func (m *Model) resetArrivals() {
	m.fx.arrived = nil
	m.fx.arrivals = 0
}

// rowFade picks the color of the title of a row that is not selected: new rows
// fade in, and the row the cursor just left fades out of the highlight.
func (m *Model) rowFade(id string) (phase int, fading bool) {
	if m.animations < animFull {
		return 2, false
	}
	now := m.now()
	if start, ok := m.fx.arrived[id]; ok {
		if p := fadePhase(now.Sub(start)); p < 2 {
			return p, true
		}
	}
	if id == m.fx.prevID && m.fx.prevID != "" {
		// The highlight fades out: the same steps, bright first.
		if p := fadePhase(now.Sub(m.fx.prevAt)); p < 2 {
			return p, true
		}
	}
	return 2, false
}

// flashPhase is the step of the status line after its text changed: 0 bright,
// 1 medium, 2 normal.
func (m *Model) flashPhase() int {
	if m.animations < animFull || m.status == "" {
		return 2
	}
	return fadePhase(m.now().Sub(m.fx.statusAt))
}

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// spinnerFrame is the spinner at time t. It is computed from the time, so it
// does not need to remember a frame.
func spinnerFrame(t time.Time) string {
	return spinnerFrames[int(t.UnixMilli()/int64(fxFast/time.Millisecond))%len(spinnerFrames)]
}

// dressUndo removes the undo note from a status once the undo window is over,
// so the hint does not stay on screen after u stopped working.
func (m *Model) dressUndo(status string) string {
	if !strings.Contains(status, undoNote) {
		return status
	}
	if m.undo == nil || !m.now().Before(m.undo.until) {
		return strings.Replace(status, undoNote, "", 1)
	}
	return status
}

// undoExpiredMsg wakes the program when the undo window ends, to draw the
// status without the undo note.
type undoExpiredMsg struct{}

func undoExpiry() tea.Cmd {
	return tea.Tick(undoWindow+50*time.Millisecond, func(time.Time) tea.Msg { return undoExpiredMsg{} })
}

// liveDim is true during the dim half of the LIVE badge pulse.
func (m *Model) liveDim() bool {
	return m.animations == animFull && (m.now().UnixMilli()/int64(fxSlow/time.Millisecond))%2 == 1
}

// liveVisible reports whether a LIVE row is on screen, which needs the pulse.
func (m *Model) liveVisible() bool {
	visible := max(1, (m.bodyHeight()-1)/2)
	top := min(max(0, m.top), len(m.items))
	for i := top; i < min(len(m.items), top+visible); i++ {
		if m.items[i].IsCurrentlyLive() {
			return true
		}
	}
	return false
}

// splitProgress is how far the preview pane has opened, from 0 to 1.
func (m *Model) splitProgress() float64 {
	if m.animations < animFull || m.fx.splitAt.IsZero() {
		return 1
	}
	t := float64(m.now().Sub(m.fx.splitAt)) / float64(splitDuration)
	if t >= 1 {
		return 1
	}
	return 1 - (1-t)*(1-t) // starts fast and slows down
}

func (m *Model) splitAnimating() bool { return m.splitProgress() < 1 }

// fadeStyles are the title styles of a fade, indexed by phase+1: muted for a
// row that has not appeared yet, then bright, medium and normal. They are built
// when used because a theme change replaces the styles.
func fadeStyles() [4]lipgloss.Style {
	return [4]lipgloss.Style{metaStyle, selectedTitleStyle, keyStyle, normalTitleStyle}
}

// animationLevel turns the [ui] animations setting into a level.
func animationLevel(setting string) int {
	switch setting {
	case "off":
		return animOff
	case "minimal":
		return animMinimal
	}
	return animFull
}
