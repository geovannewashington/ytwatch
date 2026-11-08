// Package storyboard prepares the small pictures that the seek bar shows under
// the mouse, like YouTube does.
//
// The watch page of a video describes them in one "spec" string. The pictures
// are in sheets: image files with a grid of frames, one frame for every few
// seconds of the video. mpv can draw a raw picture on the screen from a file
// (overlay-add), but not a JPEG or WebP, so each sheet is downloaded and written as raw
// BGRA pixels. The seek bar then shows a frame by its place in the sheet, and
// needs no program to cut it out.
package storyboard

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // the sheets are JPEG, or WebP with the name of a JPEG
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // YouTube serves the sheets as WebP, whatever their name says
)

const (
	// WantedWidth is the width of a frame that is chosen when there is a choice.
	// Small frames are made this wide, and 320 pixel ones are kept as they are.
	WantedWidth = 320

	// MaxRawBytes is the most raw pixels that are kept for one video. A long
	// video gets smaller frames, because the files are in the temporary folder,
	// which is often in memory.
	MaxRawBytes = 64 << 20

	// Limits for what the page and the sheets may make ytwatch do.
	maxSpec        = 8 << 10
	maxLevels      = 8
	maxSheets      = 120
	maxSheetBytes  = 4 << 20
	maxSheetSide   = 2048
	maxURL         = 1024
	fetchTimeout   = 15 * time.Second
	fetchWorkers   = 4
	maxFrameSide   = 640
	maxFrameCount  = 20000
	maxCellsPerRow = 20
)

// Level is one size of frame the page describes.
type Level struct {
	Width, Height int    // of one frame, in pixels
	Count         int    // the number of frames of the video
	Cols, Rows    int    // the frames in one sheet
	Interval      int    // milliseconds of video between two frames; 0: spread over the video
	Name          string // the name of the sheet file, with $M for its number
	Sigh          string // the signature that goes with the address
}

// Frames is the number of frames in one full sheet.
func (l Level) Frames() int { return l.Cols * l.Rows }

// Sheets is the number of sheets of the level.
func (l Level) Sheets() int { return (l.Count + l.Frames() - 1) / l.Frames() }

// RawBytes is the size of all the frames of the level as raw pixels.
func (l Level) RawBytes() int { return l.Count * l.Width * l.Height * 4 }

// Spec is a parsed storyboard description.
type Spec struct {
	base   string
	Levels []Level
}

// Parse reads the spec string of the watch page: an address with $L and $N in
// it, then one description for each level, separated by "|". A level is
// width#height#count#columns#rows#interval#name#signature.
func Parse(spec string) (Spec, error) {
	if spec == "" || len(spec) > maxSpec {
		return Spec{}, errors.New("no storyboard")
	}

	parts := strings.Split(spec, "|")
	if len(parts) < 2 || len(parts)-1 > maxLevels {
		return Spec{}, errors.New("unexpected storyboard")
	}

	result := Spec{base: parts[0]}
	if !strings.Contains(result.base, "$L") || !strings.Contains(result.base, "$N") {
		return Spec{}, errors.New("unexpected storyboard address")
	}

	for _, part := range parts[1:] {
		fields := strings.Split(part, "#")
		if len(fields) < 8 {
			result.Levels = append(result.Levels, Level{})
			continue
		}

		numbers := make([]int, 6)
		valid := true
		for i := range numbers {
			n, err := strconv.Atoi(fields[i])
			if err != nil || n < 0 {
				valid = false
			}
			numbers[i] = n
		}

		level := Level{
			Width: numbers[0], Height: numbers[1], Count: numbers[2], Cols: numbers[3], Rows: numbers[4], Interval: numbers[5],
			Name: fields[6], Sigh: fields[7],
		}
		if !valid || !sane(level) {
			level = Level{} // an unusable level stays in its place, so the indexes hold
		}
		result.Levels = append(result.Levels, level)
	}

	return result, nil
}

func sane(l Level) bool {
	return l.Width > 0 && l.Width <= maxFrameSide && l.Height > 0 && l.Height <= maxFrameSide &&
		l.Count > 0 && l.Count <= maxFrameCount &&
		l.Cols > 0 && l.Cols <= maxCellsPerRow && l.Rows > 0 && l.Rows <= maxCellsPerRow &&
		l.Cols*l.Width <= maxSheetSide && l.Rows*l.Height <= maxSheetSide &&
		l.Name != "" && len(l.Name) < 64 && l.Sheets() <= maxSheets
}

// Choose picks the level to use: the frames closest to WantedWidth whose
// pixels fit in MaxRawBytes. It returns false when no level can be used. A
// level without an interval is used when the length of the video is known.
func (s Spec) Choose(duration time.Duration) (index int, level Level, ok bool) {
	best := -1

	for i, l := range s.Levels {
		if l.Width == 0 || l.RawBytes() > MaxRawBytes || (l.Interval == 0 && duration <= 0) {
			continue
		}
		if l.Width < 80 && best >= 0 {
			continue // a frame this small is only the last resort
		}

		if best < 0 || abs(l.Width-WantedWidth) < abs(s.Levels[best].Width-WantedWidth) {
			best = i
		}
	}
	if best < 0 {
		return 0, Level{}, false
	}

	return best, s.Levels[best], true
}

func abs(n int) int {
	if n < 0 {
		return -n
	}

	return n
}

// SheetURL is the address of one sheet of a level. It is on YouTube's image
// servers over https, or it is refused: the address comes from a web page.
func (s Spec) SheetURL(index, sheet int) (string, error) {
	if index < 0 || index >= len(s.Levels) || s.Levels[index].Width == 0 {
		return "", errors.New("no such level")
	}

	level := s.Levels[index]
	if sheet < 0 || sheet >= level.Sheets() {
		return "", errors.New("no such sheet")
	}

	name := strings.ReplaceAll(level.Name, "$M", strconv.Itoa(sheet))
	address := strings.ReplaceAll(s.base, "$L", strconv.Itoa(index))
	address = strings.ReplaceAll(address, "$N", name)

	sep := "?"
	if strings.Contains(address, "?") {
		sep = "&"
	}
	address += sep + "sigh=" + url.QueryEscape(level.Sigh)

	if !AddressAllowed(address) {
		return "", errors.New("storyboard address not allowed")
	}

	return address, nil
}

// AddressAllowed reports whether address is an https address on YouTube's image
// servers (ytimg.com), with nothing that hides another place in it.
func AddressAllowed(address string) bool {
	if len(address) == 0 || len(address) > maxURL {
		return false
	}
	for _, r := range address {
		if r <= ' ' || r == 0x7f || strings.ContainsRune("\"'<>\\`", r) {
			return false
		}
	}

	u, err := url.Parse(address)
	if err != nil || u.Scheme != "https" || u.User != nil || (u.Port() != "" && u.Port() != "443") {
		return false
	}

	host := strings.ToLower(u.Hostname())

	return strings.HasSuffix(host, ".ytimg.com") && len(host) > len(".ytimg.com")
}

// SheetName is the name of the file of a sheet in the folder of the video.
func SheetName(sheet int) string { return fmt.Sprintf("sheet-%d.bgra", sheet) }

// Raw makes the raw pixels of a sheet from its picture (JPEG or WebP): width by height frames of
// the level's size as BGRA, with a row after row of exactly cols*frame width
// pixels, so that a frame is found by its place. A sheet that is smaller than
// it should be (the last one has fewer rows) or bigger is cut or left black.
func Raw(level Level, sheetImage []byte, sheet int) ([]byte, error) {
	if !sane(level) || sheet < 0 || sheet >= level.Sheets() {
		return nil, errors.New("no such sheet")
	}

	config, _, err := image.DecodeConfig(bytes.NewReader(sheetImage))
	if err != nil {
		return nil, err
	}
	// The size is read before the picture is, so a small file that claims to be
	// huge cannot make ytwatch allocate it.
	if config.Width <= 0 || config.Height <= 0 || config.Width > maxSheetSide || config.Height > maxSheetSide {
		return nil, errors.New("storyboard sheet has an unexpected size")
	}

	picture, _, err := image.Decode(bytes.NewReader(sheetImage))
	if err != nil {
		return nil, err
	}

	rows := level.Rows
	if left := level.Count - sheet*level.Frames(); left < level.Frames() {
		rows = (left + level.Cols - 1) / level.Cols
	}

	// Each frame goes from its place in the sheet to its place in the output,
	// bigger when the frames are small.
	frameW, frameH := FrameSize(level)
	width, height := level.Cols*frameW, rows*frameH
	scaled := image.NewRGBA(image.Rect(0, 0, width, height))
	bounds := picture.Bounds()

	for k := 0; k < rows*level.Cols; k++ {
		col, row := k%level.Cols, k/level.Cols
		from := image.Rect(col*level.Width, row*level.Height, (col+1)*level.Width, (row+1)*level.Height).
			Add(bounds.Min).Intersect(bounds)
		to := image.Rect(col*frameW, row*frameH, (col+1)*frameW, (row+1)*frameH)
		if from.Empty() {
			continue
		}

		if frameW == level.Width && frameH == level.Height {
			draw.Draw(scaled, to, picture, from.Min, draw.Src)
		} else {
			draw.BiLinear.Scale(scaled, to, picture, from, draw.Src, nil)
		}
	}

	// The pixels are RGBA in the picture and BGRA for mpv.
	raw := scaled.Pix
	for i := 0; i+3 < len(raw); i += 4 {
		raw[i], raw[i+2], raw[i+3] = raw[i+2], raw[i], 0xff
	}

	return raw, nil
}

// FrameSize is the size of a frame as it is drawn: a small frame is made
// bigger, to WantedWidth or else to a smaller step, as long as the pixels still
// fit in MaxRawBytes. A frame that is big enough is not changed.
func FrameSize(l Level) (width, height int) {
	if l.Width <= 0 || l.Height <= 0 || l.Width >= WantedWidth*4/5 {
		return l.Width, l.Height
	}

	for _, wide := range []int{WantedWidth, WantedWidth * 2 / 3} {
		high := (l.Height*wide + l.Width/2) / l.Width
		if wide > l.Width && high <= maxFrameSide && l.Count*wide*high*4 <= MaxRawBytes {
			return wide, high
		}
	}

	return l.Width, l.Height
}

// checkRedirect lets a redirect go on only to YouTube's image servers, like the
// first address, and not for long.
func checkRedirect(next *http.Request, via []*http.Request) error {
	if len(via) >= 3 || !AddressAllowed(next.URL.String()) {
		return errors.New("redirect not allowed")
	}

	return nil
}

// fetch downloads a sheet. It is a variable so that tests do not use the
// network.
var fetch = func(ctx context.Context, address string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:130.0) Gecko/20100101 Firefox/130.0")

	client := &http.Client{CheckRedirect: checkRedirect}

	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("youtube returned %s", response.Status)
	}

	data, err := io.ReadAll(io.LimitReader(response.Body, maxSheetBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxSheetBytes {
		return nil, errors.New("storyboard sheet is too large")
	}

	return data, nil
}

// Download fetches the sheets of a level into dir, a few at a time, as raw
// files named SheetName. A file appears when it is whole. It returns when all
// are done or ctx ends, and reports how many sheets are there.
func Download(ctx context.Context, spec Spec, index int, dir string) int {
	level := spec.Levels[index]

	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		done int
		next = make(chan int)
	)

	for range fetchWorkers {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for sheet := range next {
				if writeSheet(ctx, spec, index, level, sheet, dir) {
					mu.Lock()
					done++
					mu.Unlock()
				}
			}
		}()
	}

	for sheet := 0; sheet < level.Sheets(); sheet++ {
		select {
		case next <- sheet:
		case <-ctx.Done():
			sheet = level.Sheets()
		}
	}
	close(next)
	wg.Wait()

	return done
}

func writeSheet(ctx context.Context, spec Spec, index int, level Level, sheet int, dir string) bool {
	address, err := spec.SheetURL(index, sheet)
	if err != nil {
		return false
	}

	data, err := fetch(ctx, address)
	if err != nil {
		return false
	}

	raw, err := Raw(level, data, sheet)
	if err != nil {
		return false
	}

	// Written aside and renamed, so a reader sees a whole file or none.
	path := filepath.Join(dir, SheetName(sheet))
	temporary := path + ".part"
	if err := os.WriteFile(temporary, raw, 0o600); err != nil {
		return false
	}

	return os.Rename(temporary, path) == nil
}
