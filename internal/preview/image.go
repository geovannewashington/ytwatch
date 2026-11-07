package preview

import (
	"errors"
	"image"
	_ "image/jpeg" // thumbnails are JPEG or PNG, see Download
	_ "image/png"
	"io"
	"os"

	"golang.org/x/image/draw"
)

// maxImageSide is the largest width or height of a thumbnail ytwatch decodes.
// YouTube's biggest thumbnail is 1280 wide, so this leaves room.
const maxImageSide = 4096

var errImageTooLarge = errors.New("thumbnail is too large")

// loadImage decodes the image at path. The header is read first: it tells the
// size before any pixel is allocated, so a small file that claims to be huge
// (a decompression bomb) is refused instead of using all the memory.
func loadImage(path string) (image.Image, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	config, _, err := image.DecodeConfig(file)
	if err != nil {
		return nil, err
	}
	if config.Width <= 0 || config.Height <= 0 || config.Width > maxImageSide || config.Height > maxImageSide {
		return nil, errImageTooLarge
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}

	img, _, err := image.Decode(file)
	return img, err
}

// fitCells returns the size in cells of the largest image of the given aspect
// that fits in maxCols by maxRows cells of cellW by cellH pixels.
func fitCells(imgW, imgH, cellW, cellH, maxCols, maxRows int) (cols, rows int) {
	if imgW <= 0 || imgH <= 0 || maxCols < 1 || maxRows < 1 {
		return 0, 0
	}

	scale := min(float64(maxCols*cellW)/float64(imgW), float64(maxRows*cellH)/float64(imgH))
	cols = min(maxCols, max(1, int(float64(imgW)*scale/float64(cellW)+0.5)))
	rows = min(maxRows, max(1, int(float64(imgH)*scale/float64(cellH)+0.5)))

	return cols, rows
}

// scaleTo resizes img to exactly w by h pixels.
func scaleTo(img image.Image, w, h int) *image.RGBA {
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.CatmullRom.Scale(out, out.Bounds(), img, img.Bounds(), draw.Src, nil)

	return out
}
