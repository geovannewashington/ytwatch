package preview

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image/png"
	"io"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

const (
	kittyID     = 42 // the one image id; a new transmission replaces the image
	placeholder = "\U0010EEEE"
	chunkSize   = 4096 // the most base64 bytes in one escape sequence
)

// rowDiacritics are the first entries of kitty's rowcolumn-diacritics table:
// the combining mark after a placeholder names its row (and column) number.
var rowDiacritics = []rune{
	0x0305, 0x030D, 0x030E, 0x0310, 0x0312, 0x033D, 0x033E, 0x033F, 0x0346, 0x034A,
}

// Kitty draws images with the kitty graphics protocol. The pixels are sent once
// to the terminal, and the text of the screen only holds Unicode placeholders
// that show them, so the image scrolls, clips and redraws like text.
type Kitty struct {
	w    io.Writer // the same locked writer as the program output, see SyncWriter
	tmux bool      // wrap sequences for tmux's allow-passthrough
	cell func() (w, h int)

	// sent is what the terminal shows now. The pixel size of a cell is part of
	// it: a font or DPI change keeps the cell count but needs a new bitmap.
	sent struct {
		path         string
		cols, rows   int
		cellW, cellH int
	}
	transmitted bool // an image is stored in the terminal, whatever sent says
}

// NewKitty returns a Kitty that writes its sequences to w.
func NewKitty(w io.Writer, tmux bool) *Kitty {
	return &Kitty{w: w, tmux: tmux, cell: terminalCellSize}
}

// terminalCellSize is the pixel size of a cell of the terminal on stdout. A
// terminal that does not report it gets cells twice as tall as wide.
func terminalCellSize() (w, h int) {
	ws, err := unix.IoctlGetWinsize(int(os.Stdout.Fd()), unix.TIOCGWINSZ)
	if err != nil || ws.Xpixel == 0 || ws.Ypixel == 0 || ws.Col == 0 || ws.Row == 0 {
		return 10, 20
	}

	return int(ws.Xpixel / ws.Col), int(ws.Ypixel / ws.Row)
}

// Render sends the image at path to the terminal if it is not there yet, and
// returns the lines of placeholders that show it, centred in maxCols.
func (k *Kitty) Render(path string, maxCols, maxRows int) ([]string, error) {
	src, err := loadImage(path)
	if err != nil {
		return nil, err
	}

	cellW, cellH := k.cell()
	size := src.Bounds()
	cols, rows := fitCells(size.Dx(), size.Dy(), cellW, cellH, maxCols, min(maxRows, len(rowDiacritics)))
	if cols == 0 {
		return nil, fmt.Errorf("no room for a thumbnail")
	}

	if k.sent.path != path || k.sent.cols != cols || k.sent.rows != rows ||
		k.sent.cellW != cellW || k.sent.cellH != cellH {
		var encoded bytes.Buffer
		if err := png.Encode(&encoded, scaleTo(src, cols*cellW, rows*cellH)); err != nil {
			return nil, err
		}
		if _, err := k.w.Write([]byte(k.transmission(encoded.Bytes(), cols, rows))); err != nil {
			return nil, err
		}
		k.sent.path, k.sent.cols, k.sent.rows = path, cols, rows
		k.sent.cellW, k.sent.cellH = cellW, cellH
		k.transmitted = true
	}

	return placeholderLines(cols, rows, (maxCols-cols)/2), nil
}

// Reset makes the next Render send the image again, for a file that changed.
// The old image stays in the terminal until it is replaced or deleted.
func (k *Kitty) Reset() { k.sent.path = "" }

// Delete removes the image from the terminal.
func (k *Kitty) Delete() {
	if !k.transmitted {
		return
	}

	k.sent.path, k.transmitted = "", false
	_, _ = k.w.Write([]byte(k.escape(fmt.Sprintf("a=d,d=I,i=%d,q=2", kittyID))))
}

// transmission is the escape sequences that send a PNG and create its virtual
// placement. Only the first chunk carries the keys besides m and q; q=2 is in
// every chunk, because the replies would otherwise arrive as key presses.
func (k *Kitty) transmission(data []byte, cols, rows int) string {
	payload := base64.StdEncoding.EncodeToString(data)

	var out strings.Builder
	for first := true; len(payload) > 0; first = false {
		chunk := payload[:min(chunkSize, len(payload))]
		payload = payload[len(chunk):]

		more := 0
		if len(payload) > 0 {
			more = 1
		}

		keys := fmt.Sprintf("q=2,m=%d", more)
		if first {
			keys = fmt.Sprintf("a=T,U=1,f=100,t=d,i=%d,c=%d,r=%d,", kittyID, cols, rows) + keys
		}

		out.WriteString(k.escape(keys + ";" + chunk))
	}

	return out.String()
}

// escape wraps graphics control data in an APC sequence, and for tmux in a
// passthrough sequence with every ESC doubled.
func (k *Kitty) escape(body string) string {
	seq := "\x1b_G" + body + "\x1b\\"
	if k.tmux {
		return "\x1bPtmux;" + strings.ReplaceAll(seq, "\x1b", "\x1b\x1b") + "\x1b\\"
	}

	return seq
}

// placeholderLines are the rows of the image: the placeholder cells coloured
// with the image id. Only the first cell of a row names its row and column;
// the others follow from it.
func placeholderLines(cols, rows, pad int) []string {
	lines := make([]string, rows)
	for y := range rows {
		lines[y] = strings.Repeat(" ", pad) +
			fmt.Sprintf("\x1b[38;5;%dm", kittyID) +
			placeholder + string(rowDiacritics[y]) + string(rowDiacritics[0]) +
			strings.Repeat(placeholder, cols-1) +
			"\x1b[39m"
	}

	return lines
}
