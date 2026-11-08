package storyboard

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// realSpec is the spec of a real video (the signatures are shortened).
const realSpec = "https://i.ytimg.com/sb/dQw4w9WgXcQ/storyboard3_L$L/$N.jpg?sqp=-oaymwENSDfyq4qpAwVwAcABBqLzl_8DBgjXxPfBBg==" +
	"|48#27#100#10#10#0#default#rs$AOn4CLDgtW" +
	"|80#45#108#10#10#2000#M$M#rs$AOn4CLBf8G" +
	"|160#90#108#5#5#2000#M$M#rs$AOn4CLClA1" +
	"|320#180#108#3#3#2000#M$M#rs$AOn4CLCHZ"

func TestParseReadsTheLevels(t *testing.T) {
	spec, err := Parse(realSpec)
	if err != nil || len(spec.Levels) != 4 {
		t.Fatalf("levels = %d, err = %v", len(spec.Levels), err)
	}

	want := Level{Width: 160, Height: 90, Count: 108, Cols: 5, Rows: 5, Interval: 2000, Name: "M$M", Sigh: "rs$AOn4CLClA1"}
	if spec.Levels[2] != want {
		t.Errorf("level 2 = %+v, want %+v", spec.Levels[2], want)
	}
	if want.Frames() != 25 || want.Sheets() != 5 || want.RawBytes() != 108*160*90*4 {
		t.Errorf("Frames = %d, Sheets = %d, RawBytes = %d", want.Frames(), want.Sheets(), want.RawBytes())
	}
}

func TestParseRefusesWhatItCannotUse(t *testing.T) {
	for name, spec := range map[string]string{
		"empty":           "",
		"no levels":       "https://i.ytimg.com/sb/x/storyboard3_L$L/$N.jpg",
		"no placeholders": "https://i.ytimg.com/sb/x/a.jpg|160#90#10#5#5#1000#M$M#s",
		"too long":        strings.Repeat("a", maxSpec+1),
		"too many levels": "https://x/$L/$N" + strings.Repeat("|160#90#10#5#5#1000#M$M#s", maxLevels+1),
	} {
		if _, err := Parse(spec); err == nil {
			t.Errorf("%s: should be refused", name)
		}
	}
}

func TestParseKeepsAnUnusableLevelInItsPlace(t *testing.T) {
	spec, err := Parse("https://i.ytimg.com/sb/x/s_L$L/$N.jpg" +
		"|banana" + // not a level
		"|160#90#10#5#5#1000#M$M#s" + // fine
		"|99999#90#10#5#5#1000#M$M#s" + // too wide
		"|160#90#10#0#5#1000#M$M#s" + // no columns
		"|160#90#-5#5#5#1000#M$M#s" + // negative
		"|160#90#9999999#5#5#1000#M$M#s" + // too many frames
		"|160#90#10#5#5#1000##s" + // no name
		"|160#90#10#25#25#1000#M$M#s") // a sheet bigger than the limit
	if err != nil {
		t.Fatal(err)
	}

	usable := 0
	for i, level := range spec.Levels {
		if level.Width != 0 {
			usable++
			if i != 1 {
				t.Errorf("level %d should be unusable: %+v", i, level)
			}
		}
	}
	if usable != 1 {
		t.Errorf("only the good level can be used, %d are", usable)
	}
}

func TestChoosePicksTheFramesClosestToTheWantedWidth(t *testing.T) {
	spec, _ := Parse(realSpec)

	index, level, ok := spec.Choose(213 * time.Second)
	if !ok || index != 3 || level.Width != 320 {
		t.Errorf("Choose = %d, %+v, %v; want the 320 wide frames", index, level, ok)
	}

	// A long video has many frames: the pixels of the big ones do not fit.
	long, _ := Parse("https://i.ytimg.com/sb/x/s_L$L/$N.jpg|80#45#1500#10#10#10000#M$M#a|160#90#1500#5#5#10000#M$M#b|320#180#1500#3#3#10000#M$M#c")
	index, level, ok = long.Choose(3 * time.Hour)
	if !ok || level.Width != 80 || index != 0 || level.RawBytes() > MaxRawBytes {
		t.Errorf("a long video gets small frames that fit: %d, %+v", index, level)
	}

	// Nothing fits.
	huge, _ := Parse("https://i.ytimg.com/sb/x/s_L$L/$N.jpg|320#180#20000#3#3#1000#M$M#a")
	if _, _, ok := huge.Choose(time.Hour); ok {
		t.Error("frames that cannot fit are not used")
	}

	// A level spread over the video needs its length.
	spread, _ := Parse("https://i.ytimg.com/sb/x/s_L$L/$N.jpg|160#90#100#10#10#0#default#a")
	if _, _, ok := spread.Choose(0); ok {
		t.Error("without the length of the video, a level without an interval cannot be used")
	}
	if _, _, ok := spread.Choose(time.Minute); !ok {
		t.Error("with the length, it can")
	}
}

func TestSheetURL(t *testing.T) {
	spec, _ := Parse(realSpec)

	got, err := spec.SheetURL(2, 3)
	want := "https://i.ytimg.com/sb/dQw4w9WgXcQ/storyboard3_L2/M3.jpg?sqp=-oaymwENSDfyq4qpAwVwAcABBqLzl_8DBgjXxPfBBg==&sigh=rs%24AOn4CLClA1"
	if err != nil || got != want {
		t.Errorf("SheetURL = %q, %v\nwant %q", got, err, want)
	}
	if u, _ := url.Parse(got); u.Query().Get("sigh") != "rs$AOn4CLClA1" {
		t.Errorf("the signature must survive the escaping: %v", u.Query())
	}

	for _, tc := range [][2]int{{-1, 0}, {9, 0}, {2, -1}, {2, 5}, {0, 1}} {
		if _, err := spec.SheetURL(tc[0], tc[1]); err == nil && !(tc == [2]int{0, 1}) {
			t.Errorf("SheetURL(%d, %d) should be refused", tc[0], tc[1])
		}
	}
}

func TestAddressAllowedOnlyForYouTubesImageServers(t *testing.T) {
	for _, good := range []string{
		"https://i.ytimg.com/sb/x/M0.jpg?sqp=a&sigh=b",
		"https://i9.ytimg.com/sb/x/M0.jpg",
		"https://I.YTIMG.COM/sb/x",
		"https://i.ytimg.com:443/sb",
	} {
		if !AddressAllowed(good) {
			t.Errorf("%q should be allowed", good)
		}
	}

	for _, bad := range []string{
		"", "http://i.ytimg.com/sb/x", "https://ytimg.com/x", "https://evil.example/x",
		"https://i.ytimg.com.evil.example/x", "https://evilytimg.com/x", "https://i.ytimg.com@evil.example/x",
		"https://user:pw@i.ytimg.com/x", "https://i.ytimg.com:8443/x", "file:///etc/passwd", "javascript:alert(1)",
		"https://i.ytimg.com/ x", "https://i.ytimg.com/\"x", "//i.ytimg.com/x", "https://i.ytimg.com/" + strings.Repeat("a", maxURL),
		"https://169.254.169.254/latest/meta-data", "https://localhost/x",
	} {
		if AddressAllowed(bad) {
			t.Errorf("%q must not be allowed", bad)
		}
	}
}

func TestARedirectMustStayOnYouTubesImageServers(t *testing.T) {
	next := func(raw string) *http.Request {
		u, _ := url.Parse(raw)
		return &http.Request{URL: u}
	}

	if err := checkRedirect(next("https://i.ytimg.com/sb/y"), nil); err != nil {
		t.Errorf("a redirect inside is fine: %v", err)
	}
	if checkRedirect(next("https://evil.example/y"), nil) == nil || checkRedirect(next("http://i.ytimg.com/y"), nil) == nil {
		t.Error("a redirect to another place must stop")
	}
	if checkRedirect(next("https://i.ytimg.com/y"), make([]*http.Request, 3)) == nil {
		t.Error("a long chain of redirects must stop")
	}
}

// sheetImage makes a sheet with a solid color in each frame.
func sheetImage(t *testing.T, level Level, rows int, colors []color.RGBA) []byte {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, level.Cols*level.Width, rows*level.Height))
	for i, c := range colors {
		x0, y0 := (i%level.Cols)*level.Width, (i/level.Cols)*level.Height
		for y := y0; y < y0+level.Height; y++ {
			for x := x0; x < x0+level.Width; x++ {
				img.SetRGBA(x, y, c)
			}
		}
	}

	var out bytes.Buffer
	if err := jpeg.Encode(&out, img, &jpeg.Options{Quality: 100}); err != nil {
		t.Fatal(err)
	}

	return out.Bytes()
}

func near(got, want byte) bool { return int(got)-int(want) < 8 && int(want)-int(got) < 8 }

func TestRawKeepsEachFrameWhereItIs(t *testing.T) {
	level := Level{Width: 320, Height: 8, Count: 4, Cols: 2, Rows: 2, Interval: 1000, Name: "M$M"}
	red, green, blue, white := color.RGBA{255, 0, 0, 255}, color.RGBA{0, 255, 0, 255}, color.RGBA{0, 0, 255, 255}, color.RGBA{255, 255, 255, 255}

	raw, err := Raw(level, sheetImage(t, level, 2, []color.RGBA{red, green, blue, white}), 0)
	if err != nil {
		t.Fatal(err)
	}
	stride := level.Cols * level.Width * 4
	if len(raw) != stride*2*level.Height {
		t.Fatalf("size = %d, want %d", len(raw), stride*2*level.Height)
	}

	// The pixel in the middle of each frame, as BGRA.
	for i, want := range [][3]byte{{0, 0, 255}, {0, 255, 0}, {255, 0, 0}, {255, 255, 255}} { // B, G, R of red, green, blue, white
		x, y := (i%2)*320+160, (i/2)*8+4
		p := raw[y*stride+x*4 : y*stride+x*4+4]
		if !near(p[0], want[0]) || !near(p[1], want[1]) || !near(p[2], want[2]) || p[3] != 0xff {
			t.Errorf("frame %d: BGRA = %v, want %v and opaque", i, p, want)
		}
	}
}

func TestRawOfALastSheetWithFewerRows(t *testing.T) {
	level := Level{Width: 320, Height: 8, Count: 7, Cols: 2, Rows: 2, Interval: 1000, Name: "M$M"} // sheet 1 has 3 frames: 2 rows
	raw, err := Raw(level, sheetImage(t, level, 2, []color.RGBA{{255, 0, 0, 255}, {0, 255, 0, 255}, {0, 0, 255, 255}}), 1)
	if err != nil || len(raw) != 2*320*4*2*8 {
		t.Fatalf("len = %d, err = %v", len(raw), err)
	}

	short := Level{Width: 320, Height: 8, Count: 5, Cols: 2, Rows: 2, Interval: 1000, Name: "M$M"} // sheet 1 has 1 frame: 1 row
	raw, err = Raw(short, sheetImage(t, short, 1, []color.RGBA{{255, 0, 0, 255}}), 1)
	if err != nil || len(raw) != 2*320*4*8 {
		t.Errorf("a sheet with one row: len = %d, err = %v", len(raw), err)
	}

	// A picture smaller than it should be leaves the rest black, not a crash.
	small := image.NewRGBA(image.Rect(0, 0, 10, 10))
	var out bytes.Buffer
	if err := jpeg.Encode(&out, small, nil); err != nil {
		t.Fatal(err)
	}
	if raw, err := Raw(level, out.Bytes(), 0); err != nil || len(raw) != 2*320*4*2*8 {
		t.Errorf("a small picture: len = %d, err = %v", len(raw), err)
	}
}

func TestRawMakesSmallFramesBigger(t *testing.T) {
	level := Level{Width: 160, Height: 90, Count: 4, Cols: 2, Rows: 2, Interval: 1000, Name: "M$M"}
	if w, h := FrameSize(level); w != WantedWidth || h != 180 {
		t.Fatalf("FrameSize = %d x %d, want %d x 180", w, h, WantedWidth)
	}

	red, blue := color.RGBA{255, 0, 0, 255}, color.RGBA{0, 0, 255, 255}
	raw, err := Raw(level, sheetImage(t, level, 2, []color.RGBA{red, blue, blue, red}), 0)
	if err != nil {
		t.Fatal(err)
	}

	stride := 2 * WantedWidth * 4
	if len(raw) != stride*2*180 {
		t.Fatalf("size = %d, want %d", len(raw), stride*2*180)
	}
	for i, want := range [][3]byte{{0, 0, 255}, {255, 0, 0}} { // BGR of red, then blue
		x, y := (i%2)*WantedWidth+WantedWidth/2, 90
		p := raw[y*stride+x*4 : y*stride+x*4+4]
		if !near(p[0], want[0]) || !near(p[1], want[1]) || !near(p[2], want[2]) || p[3] != 0xff {
			t.Errorf("frame %d: BGRA = %v, want %v and opaque", i, p, want)
		}
	}

	// Frames that are big enough stay as they are, and so do frames whose
	// bigger pixels would not fit.
	if w, h := FrameSize(Level{Width: 320, Height: 180, Count: 10}); w != 320 || h != 180 {
		t.Errorf("a big frame is not changed: %d x %d", w, h)
	}
	if w, h := FrameSize(Level{Width: 160, Height: 90, Count: 300}); w != 213 || h != 120 {
		t.Errorf("a long video gets a smaller step: %d x %d", w, h)
	}
	if w, h := FrameSize(Level{Width: 160, Height: 90, Count: 2000}); w != 160 || h != 90 {
		t.Errorf("frames that would not fit are not made bigger: %d x %d", w, h)
	}
}

func TestRawRefusesASheetThatIsNotThere(t *testing.T) {
	level := Level{Width: 16, Height: 16, Count: 5, Cols: 2, Rows: 2, Interval: 1000, Name: "M$M"} // sheets 0 and 1
	data := sheetImage(t, level, 2, []color.RGBA{{255, 0, 0, 255}})

	for _, sheet := range []int{-1, 2, 99} {
		if _, err := Raw(level, data, sheet); err == nil {
			t.Errorf("sheet %d does not exist", sheet)
		}
	}
	if _, err := Raw(Level{}, data, 0); err == nil {
		t.Error("an unusable level cannot make a sheet")
	}
}

func TestRawRefusesWhatIsNotAPictureOrIsHuge(t *testing.T) {
	level := Level{Width: 16, Height: 16, Count: 4, Cols: 2, Rows: 2, Interval: 1000, Name: "M$M"}

	for name, data := range map[string][]byte{"empty": nil, "text": []byte("not a picture"), "html": []byte("<html>403</html>")} {
		if _, err := Raw(level, data, 0); err == nil {
			t.Errorf("%s: should be refused", name)
		}
	}

	big := image.NewGray(image.Rect(0, 0, maxSheetSide+1, 8)) // tiny as a file, huge as pixels
	var out bytes.Buffer
	if err := jpeg.Encode(&out, big, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := Raw(level, out.Bytes(), 0); err == nil {
		t.Error("a picture that claims to be huge must be refused before it is decoded")
	}
}

func fakeFetch(t *testing.T, fn func(ctx context.Context, address string) ([]byte, error)) {
	t.Helper()

	old := fetch
	t.Cleanup(func() { fetch = old })
	fetch = fn
}

func TestDownloadWritesWholeSheetsAndSkipsFailures(t *testing.T) {
	level := Level{Width: 16, Height: 16, Count: 10, Cols: 2, Rows: 2, Interval: 1000, Name: "M$M", Sigh: "s"} // 3 sheets
	spec := Spec{base: "https://i.ytimg.com/sb/x/s_L$L/$N.jpg?a=b", Levels: []Level{level}}
	sheet := sheetImage(t, level, 2, []color.RGBA{{255, 0, 0, 255}})

	var calls atomic.Int32
	fakeFetch(t, func(_ context.Context, address string) ([]byte, error) {
		calls.Add(1)
		if strings.Contains(address, "/M1.jpg") {
			return nil, errors.New("offline")
		}
		return sheet, nil
	})

	dir := t.TempDir()
	if done := Download(context.Background(), spec, 0, dir); done != 2 {
		t.Errorf("done = %d, want the two sheets that could be fetched", done)
	}
	if calls.Load() != 3 {
		t.Errorf("calls = %d", calls.Load())
	}

	for sheet, want := range map[int]bool{0: true, 1: false, 2: true} {
		_, err := os.Stat(filepath.Join(dir, SheetName(sheet)))
		if (err == nil) != want {
			t.Errorf("sheet %d: exists = %v, want %v", sheet, err == nil, want)
		}
	}
	if parts, _ := filepath.Glob(filepath.Join(dir, "*.part")); len(parts) != 0 {
		t.Errorf("no half-written file may stay: %v", parts)
	}
}

func TestDownloadStopsWhenTheContextEnds(t *testing.T) {
	level := Level{Width: 16, Height: 16, Count: 100, Cols: 2, Rows: 2, Interval: 1000, Name: "M$M", Sigh: "s"} // 25 sheets
	spec := Spec{base: "https://i.ytimg.com/sb/x/s_L$L/$N.jpg?a=b", Levels: []Level{level}}

	ctx, cancel := context.WithCancel(context.Background())
	var calls atomic.Int32
	fakeFetch(t, func(ctx context.Context, _ string) ([]byte, error) {
		if calls.Add(1) == 3 {
			cancel()
		}
		<-ctx.Done()
		return nil, ctx.Err()
	})

	done := make(chan struct{})
	go func() { Download(ctx, spec, 0, t.TempDir()); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Download must stop when the context ends")
	}
	if calls.Load() >= 25 {
		t.Errorf("it kept on asking after the end: %d calls", calls.Load())
	}
}

func TestDownloadNeverAsksForAnAddressOutsideYouTube(t *testing.T) {
	level := Level{Width: 16, Height: 16, Count: 4, Cols: 2, Rows: 2, Interval: 1000, Name: "M$M", Sigh: "s"}
	spec := Spec{base: "https://evil.example/sb/s_L$L/$N.jpg", Levels: []Level{level}}
	fakeFetch(t, func(context.Context, string) ([]byte, error) {
		t.Error("an address outside YouTube must not be fetched")
		return nil, errors.New("no")
	})

	if done := Download(context.Background(), spec, 0, t.TempDir()); done != 0 {
		t.Errorf("done = %d", done)
	}
}
