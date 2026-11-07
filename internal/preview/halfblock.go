package preview

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// HalfBlocks draws the image at path with half-block characters, two pixels
// per cell, in at most maxCols by maxRows cells. Each line is centred in
// maxCols and ends without a full reset, so the style around it survives.
func HalfBlocks(path string, maxCols, maxRows int) ([]string, error) {
	src, err := loadImage(path)
	if err != nil {
		return nil, err
	}

	size := src.Bounds()
	cols, rows := fitCells(size.Dx(), size.Dy(), 1, 2, maxCols, maxRows)
	if cols == 0 {
		return nil, fmt.Errorf("no room for a thumbnail")
	}

	img := scaleTo(src, cols, rows*2)
	profile := lipgloss.ColorProfile()
	pad := strings.Repeat(" ", (maxCols-cols)/2)

	lines := make([]string, rows)
	for y := range rows {
		var line strings.Builder
		line.WriteString(pad)

		for x := range cols {
			top, bottom := img.RGBAAt(x, 2*y), img.RGBAAt(x, 2*y+1)
			fg := profile.Color(fmt.Sprintf("#%02x%02x%02x", top.R, top.G, top.B)).Sequence(false)
			bg := profile.Color(fmt.Sprintf("#%02x%02x%02x", bottom.R, bottom.G, bottom.B)).Sequence(true)

			if fg != "" || bg != "" {
				line.WriteString("\x1b[" + strings.Trim(fg+";"+bg, ";") + "m")
			}
			line.WriteString("▀")
		}

		line.WriteString("\x1b[39;49m")
		lines[y] = line.String()
	}

	return lines, nil
}
