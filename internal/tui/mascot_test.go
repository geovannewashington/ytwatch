package tui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestMascotKeepsItsShapeInEveryPose(t *testing.T) {
	for _, mood := range []mascotMood{moodIdle, moodSearching, moodSad} {
		for step := 0; step < 2; step++ {
			rows := mascotRows(mood, step)
			if len(rows) != mascotHeight {
				t.Fatalf("mood %d: %d rows, want %d", mood, len(rows), mascotHeight)
			}
			for i, row := range rows {
				if w := lipgloss.Width(row); w != mascotWidth {
					t.Errorf("mood %d step %d row %d is %d columns wide, want %d: %q", mood, step, i, w, mascotWidth, row)
				}
			}
		}
	}
	// The antennae move while searching and stay still otherwise.
	if mascotRows(moodSearching, 0)[0] == mascotRows(moodSearching, 1)[0] {
		t.Error("the antennae should move while searching")
	}
	if mascotRows(moodIdle, 0)[0] != mascotRows(moodIdle, 1)[0] {
		t.Error("the antennae should not move when idle")
	}
}

func TestEmptyListFitsItsPaneAndFallsBackToTextWhenSmall(t *testing.T) {
	m := New("")
	for _, size := range [][2]int{{60, 20}, {40, 12}, {30, 8}, {30, 6}, {10, 4}} {
		lines := m.renderEmptyList(size[0], size[1])
		if len(lines) > size[1] {
			t.Errorf("%v: %d lines do not fit", size, len(lines))
		}
		for _, line := range lines {
			if w := lipgloss.Width(line); w > size[0] {
				t.Errorf("%v: a line is %d columns wide", size, w)
			}
		}
	}
	if got := len(m.renderEmptyList(30, 6)); got != 1 {
		t.Errorf("a small pane shows only the text, got %d lines", got)
	}
}
