package preview

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"
)

var _ term.File = (*SyncWriter)(nil)

func writeTestImage(t *testing.T, w, h int, c color.Color) string {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, c)
		}
	}

	path := filepath.Join(t.TempDir(), "thumb.png")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	if err := png.Encode(file, img); err != nil {
		t.Fatal(err)
	}

	return path
}

func testKitty(w *bytes.Buffer, tmux bool) *Kitty {
	k := NewKitty(w, tmux)
	k.cell = func() (int, int) { return 10, 20 }

	return k
}

func TestKittyTransmissionIsChunked(t *testing.T) {
	k := testKitty(nil, false)
	data := make([]byte, 7000) // 9336 base64 bytes: 3 chunks
	out := k.transmission(data, 20, 5)

	parts := strings.Split(strings.TrimSuffix(out, "\x1b\\"), "\x1b\\")
	if len(parts) != 3 {
		t.Fatalf("got %d chunks, want 3", len(parts))
	}

	var payload strings.Builder
	for i, part := range parts {
		keys, chunk, _ := strings.Cut(strings.TrimPrefix(part, "\x1b_G"), ";")
		if len(chunk) > chunkSize {
			t.Errorf("chunk %d has %d bytes", i, len(chunk))
		}

		switch i {
		case 0:
			if keys != "a=T,U=1,f=100,t=d,i=42,c=20,r=5,q=2,m=1" {
				t.Errorf("first chunk keys %q", keys)
			}
		case 1:
			if keys != "q=2,m=1" {
				t.Errorf("middle chunk keys %q", keys)
			}
		default:
			if keys != "q=2,m=0" {
				t.Errorf("last chunk keys %q", keys)
			}
		}
		payload.WriteString(chunk)
	}

	if got, err := base64.StdEncoding.DecodeString(payload.String()); err != nil || len(got) != len(data) {
		t.Errorf("payload does not decode to the data: %v", err)
	}
}

func TestKittySingleChunkEndsWithM0(t *testing.T) {
	out := testKitty(nil, false).transmission([]byte("tiny"), 1, 1)
	if !strings.Contains(out, ",m=0;") || strings.Count(out, "\x1b_G") != 1 {
		t.Errorf("one chunk with m=0 expected: %q", out)
	}
}

func TestKittyTmuxWrapsEverySequenceWithDoubledEscapes(t *testing.T) {
	k := testKitty(nil, true)
	out := k.transmission(make([]byte, 7000), 20, 5)

	for _, seq := range strings.SplitAfter(out, "\x1b\\\x1b\\")[:1] {
		if !strings.HasPrefix(seq, "\x1bPtmux;\x1b\x1b_Ga=T") {
			t.Errorf("not a passthrough sequence: %.30q", seq)
		}
	}
	if got := strings.Count(out, "\x1bPtmux;"); got != 3 {
		t.Errorf("%d passthrough wrappers, want 3", got)
	}
	if strings.Count(out, "\x1b\x1b_G") != 3 || strings.Count(out, "\x1b\x1b\\") != 3 {
		t.Error("each inner ESC must be doubled")
	}
	if !strings.HasSuffix(out, "\x1b\x1b\\\x1b\\") {
		t.Errorf("the wrapper must end with ESC backslash: %q", out[len(out)-8:])
	}
}

func TestPlaceholderLines(t *testing.T) {
	lines := placeholderLines(12, 3, 4)
	if len(lines) != 3 {
		t.Fatalf("%d lines", len(lines))
	}

	for i, line := range lines {
		if w := lipgloss.Width(line); w != 16 || ansi.StringWidth(line) != 16 {
			t.Errorf("line %d is %d columns, want 4 + 12", i, w)
		}
		if !strings.HasPrefix(line, "    \x1b[38;5;42m\U0010EEEE"+string(rowDiacritics[i])+"̅\U0010EEEE") {
			t.Errorf("line %d starts %q", i, line)
		}
		if !strings.HasSuffix(line, "\x1b[39m") || strings.Contains(line, "\x1b[0m") {
			t.Errorf("line %d must end with a foreground reset only", i)
		}
		if strings.Count(line, "\U0010EEEE") != 12 {
			t.Errorf("line %d has %d placeholders", i, strings.Count(line, "\U0010EEEE"))
		}
	}
}

func TestKittyRenderSizesToTheImageAndSendsOnce(t *testing.T) {
	path := writeTestImage(t, 160, 90, color.RGBA{200, 10, 10, 255})

	var out bytes.Buffer
	k := testKitty(&out, false)

	lines, err := k.Render(path, 40, 8)
	if err != nil {
		t.Fatal(err)
	}

	// 16:9 in 10x20 cells: 40 columns are 400 px wide and 225 px tall, 11 rows
	// do not fit, so the 8 rows (160 px) limit it: 284 px, 28 columns.
	if len(lines) != 8 {
		t.Errorf("%d rows, want 8", len(lines))
	}
	for i, line := range lines {
		if w := lipgloss.Width(line); w != 6+28 {
			t.Errorf("line %d is %d columns wide, want 6 padding + 28", i, w)
		}
	}
	if !strings.Contains(out.String(), "c=28,r=8") {
		t.Errorf("placement size missing: %.80q", out.String())
	}

	size := out.Len()
	if _, err := k.Render(path, 44, 8); err != nil {
		t.Fatal(err)
	}
	if out.Len() != size {
		t.Error("the same image at the same size must not be sent again")
	}

	if _, err := k.Render(path, 40, 4); err != nil {
		t.Fatal(err)
	}
	if out.Len() == size {
		t.Error("a new size must send the image again")
	}

	size = out.Len()
	k.Reset()
	if _, err := k.Render(path, 40, 4); err != nil || out.Len() == size {
		t.Error("Reset must send the image again")
	}
}

func TestKittyDelete(t *testing.T) {
	path := writeTestImage(t, 20, 20, color.White)
	var out bytes.Buffer
	k := testKitty(&out, false)

	k.Delete()
	if out.Len() != 0 {
		t.Error("nothing was sent, so there is nothing to delete")
	}

	if _, err := k.Render(path, 10, 4); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	k.Delete()
	if out.String() != "\x1b_Ga=d,d=I,i=42,q=2\x1b\\" {
		t.Errorf("delete sequence %q", out.String())
	}
}

func TestFitCells(t *testing.T) {
	for name, tc := range map[string]struct {
		w, h, cw, ch, maxC, maxR, cols, rows int
	}{
		"wide image is limited by columns": {160, 90, 1, 2, 40, 20, 40, 11},
		"tall image is limited by rows":    {90, 160, 1, 2, 40, 8, 9, 8},
		"small image is scaled up":         {1, 1, 10, 20, 40, 8, 16, 8},
		"no room":                          {10, 10, 10, 20, 0, 8, 0, 0},
	} {
		cols, rows := fitCells(tc.w, tc.h, tc.cw, tc.ch, tc.maxC, tc.maxR)
		if cols != tc.cols || rows != tc.rows {
			t.Errorf("%s: got %dx%d, want %dx%d", name, cols, rows, tc.cols, tc.rows)
		}
	}
}

func TestKittySendsAgainWhenTheCellSizeChanges(t *testing.T) {
	var out bytes.Buffer
	k := NewKitty(&out, false)
	cellW := 10
	k.cell = func() (int, int) { return cellW, 20 }
	path := writePNG(t, 160, 90)

	if _, err := k.Render(path, 20, 10); err != nil {
		t.Fatal(err)
	}
	out.Reset()

	cellW = 12 // a bigger font: the same cells, more pixels
	if _, err := k.Render(path, 20, 10); err != nil {
		t.Fatal(err)
	}
	if out.Len() == 0 {
		t.Error("a new cell size needs a new bitmap")
	}
}

func TestKittyDeletesTheImageAfterAReset(t *testing.T) {
	var out bytes.Buffer
	k := NewKitty(&out, false)
	k.cell = func() (int, int) { return 10, 20 }

	if _, err := k.Render(writePNG(t, 160, 90), 20, 10); err != nil {
		t.Fatal(err)
	}
	k.Reset() // a refreshed thumbnail, then the user quits before the next Render
	out.Reset()
	k.Delete()
	if !strings.Contains(out.String(), "a=d") {
		t.Error("the image is still in the terminal, so Delete must remove it")
	}
}
