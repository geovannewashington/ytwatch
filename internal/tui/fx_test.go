package tui

import (
	"testing"
	"time"

	"github.com/geovannewashington/ytwatch/internal/media"
)

func TestFadePhase(t *testing.T) {
	cases := map[time.Duration]int{
		-time.Millisecond: -1,
		0:                 0,
		fadeStep - 1:      0,
		fadeStep:          1,
		2*fadeStep - 1:    1,
		2 * fadeStep:      2,
	}
	for age, want := range cases {
		if got := fadePhase(age); got != want {
			t.Errorf("fadePhase(%v) = %d, want %d", age, got, want)
		}
	}
}

func fixedClock(t time.Time) func() time.Time { return func() time.Time { return t } }

func TestDressUndoKeepsTheNoteOnlyInsideTheWindow(t *testing.T) {
	m := New("test")
	now := time.Now()
	m.clock = fixedClock(now)
	status := "▶ Playing" + undoNote
	m.undo = &undoAction{until: now.Add(3 * time.Second)}
	if got := m.dressUndo(status); got != status {
		t.Fatalf("inside the window the note stays: %q", got)
	}
	m.clock = fixedClock(now.Add(4 * time.Second))
	if got := m.dressUndo(status); got != "▶ Playing" {
		t.Fatalf("after the window the note should go: %q", got)
	}
}

func TestTheClockRunsOnlyWhileAnEffectDoes(t *testing.T) {
	m := New("test")
	m.width, m.height = 120, 30
	now := time.Now()
	m.clock = fixedClock(now)
	m.searchMode = false
	m.status = ""
	if fast, slow := m.fxWant(); fast || slow {
		t.Fatalf("an idle screen needs no clock: fast=%v slow=%v", fast, slow)
	}
	m.searching = true
	if fast, _ := m.fxWant(); !fast {
		t.Fatal("the spinner needs the clock")
	}
	m.searching = false
	m.downloading["video0001"] = "T"
	if fast, _ := m.fxWant(); !fast {
		t.Fatal("the download spinner needs the clock")
	}
	delete(m.downloading, "video0001")

	m.items = []media.Video{{ID: "live0000001", Title: "Live", IsLive: true}}
	if fast, slow := m.fxWant(); fast || !slow {
		t.Fatalf("a visible LIVE row needs only the slow clock: fast=%v slow=%v", fast, slow)
	}
	m.markArrived("live0000001")
	if fast, _ := m.fxWant(); !fast {
		t.Fatal("a row that is fading in needs the fast clock")
	}
	m.clock = fixedClock(now.Add(time.Second))
	m.fxExpire()
	if len(m.fx.arrived) != 0 {
		t.Fatal("a finished fade should be forgotten")
	}

	m.animations = animOff
	m.searching = true
	if fast, slow := m.fxWant(); fast || slow {
		t.Fatalf("animations off must never ask for the clock: fast=%v slow=%v", fast, slow)
	}
}

func TestMinimalKeepsOnlyTheProgressEffects(t *testing.T) {
	m := New("test")
	m.animations = animMinimal
	m.markArrived("video0001")
	if len(m.fx.arrived) != 0 {
		t.Error("minimal should not fade rows in")
	}
	m.searching = true
	if fast, _ := m.fxWant(); !fast {
		t.Error("minimal keeps the spinner")
	}
	if m.splitProgress() != 1 || m.liveDim() {
		t.Error("minimal has no pane animation or pulse")
	}
}

func TestThePreviewPaneOpensOverTime(t *testing.T) {
	m := New("test")
	m.width, m.height = 120, 30
	now := time.Now()
	m.clock = fixedClock(now)
	m.insertResult(media.Video{ID: "video0001", Title: "One"}, 0)
	if !m.splitAnimating() {
		t.Fatal("the first result should open the preview pane")
	}
	early := m.splitProgress()
	m.clock = fixedClock(now.Add(splitDuration / 2))
	if mid := m.splitProgress(); mid <= early || mid >= 1 {
		t.Fatalf("progress should grow: early=%v mid=%v", early, mid)
	}
	m.clock = fixedClock(now.Add(splitDuration))
	if m.splitAnimating() {
		t.Fatal("the pane should be open after the duration")
	}
	// A second result does not open it again.
	m.insertResult(media.Video{ID: "video0002", Title: "Two"}, 10)
	if m.fx.splitAt.IsZero() {
		t.Fatal("still waiting for the expire step in this test")
	}
	m.fxExpire()
	m.insertResult(media.Video{ID: "video0003", Title: "Three"}, 20)
	if !m.fx.splitAt.IsZero() {
		t.Fatal("only the first result opens the pane")
	}
}

func TestAnimationSettingIsParsed(t *testing.T) {
	for setting, want := range map[string]int{"full": animFull, "minimal": animMinimal, "off": animOff, "": animFull} {
		if got := animationLevel(setting); got != want {
			t.Errorf("animationLevel(%q) = %d, want %d", setting, got, want)
		}
	}
}
