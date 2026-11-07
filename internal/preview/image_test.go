package preview

import (
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func writePNG(t *testing.T, w, h int) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "thumb.png")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	// A gray image of one color compresses to a few bytes, like a bomb would.
	if err := png.Encode(file, image.NewGray(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}

	return path
}

func TestLoadImageRefusesAnImageThatClaimsToBeHuge(t *testing.T) {
	if img, err := loadImage(writePNG(t, 1280, 720)); err != nil || img.Bounds().Dx() != 1280 {
		t.Fatalf("a normal thumbnail must load: %v", err)
	}

	for _, size := range [][2]int{{maxImageSide + 1, 1}, {1, maxImageSide + 1}} {
		if _, err := loadImage(writePNG(t, size[0], size[1])); !errors.Is(err, errImageTooLarge) {
			t.Errorf("%v: err = %v, want errImageTooLarge", size, err)
		}
	}
}

func TestLoadImageReportsAFileThatIsNotAnImage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.png")
	if err := os.WriteFile(path, []byte("not an image"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadImage(path); err == nil {
		t.Fatal("expected an error")
	}
}
