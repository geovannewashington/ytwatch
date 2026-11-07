package preview

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func TestHalfBlocksUseTopAsForegroundAndBottomAsBackground(t *testing.T) {
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(previous)

	// 4x4: red on top, blue below, so 4 columns by 2 rows, one row of cells
	// per 2 pixel rows, each of them red over blue.
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := range 4 {
		for x := range 4 {
			c := color.RGBA{255, 0, 0, 255}
			if y%2 == 1 {
				c = color.RGBA{0, 0, 255, 255}
			}
			img.Set(x, y, c)
		}
	}

	path := filepath.Join(t.TempDir(), "t.png")
	file, _ := os.Create(path)
	_ = png.Encode(file, img)
	file.Close()

	lines, err := HalfBlocks(path, 10, 2)
	if err != nil {
		t.Fatal(err)
	}

	// 1:1 image with cells twice as tall as wide: 2 rows are 4 pixels tall,
	// so 4 columns; the line is centred in 10.
	if len(lines) != 2 {
		t.Fatalf("%d lines, want 2", len(lines))
	}
	for i, line := range lines {
		if w := lipgloss.Width(line); w != 3+4 {
			t.Errorf("line %d is %d columns, want 3 padding + 4", i, w)
		}
		if !strings.Contains(line, "\x1b[38;2;255;0;0;48;2;0;0;255m▀") {
			t.Errorf("line %d has no red over blue cell: %q", i, line)
		}
		if !strings.HasSuffix(line, "\x1b[39;49m") || strings.Contains(line, "\x1b[0m") {
			t.Errorf("line %d must end with a foreground and background reset only", i)
		}
	}
}

func TestHalfBlocksMissingFile(t *testing.T) {
	if _, err := HalfBlocks(filepath.Join(t.TempDir(), "none.png"), 10, 4); err == nil {
		t.Error("a missing file must fail")
	}
}
