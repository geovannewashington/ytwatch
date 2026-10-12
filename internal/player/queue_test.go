package player

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/geovannewashington/ytwatch/internal/config"
	"github.com/geovannewashington/ytwatch/internal/media"
)

// A fake mpv writes its arguments to a file, one per line.
func fakeMPV(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	out := filepath.Join(dir, "args.txt")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + out + "\n"
	if err := os.WriteFile(filepath.Join(dir, "mpv"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	return out
}

func TestStartPlaysSeveralVideosAsAQueue(t *testing.T) {
	out := fakeMPV(t)
	videos := []media.Video{
		{ID: "video0001", Title: "One"},
		{ID: "video0002", Title: "Two"},
		{ID: "video0003", Title: "Three"},
	}
	p, err := Start(videos, config.Default(), Options{Quality: "720p"})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Wait(); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(out)
	args := strings.Split(strings.TrimSpace(string(data)), "\n")
	dash := -1
	for i, a := range args {
		if a == "--" {
			dash = i
		}
	}
	if dash < 0 || len(args)-dash-1 != 3 {
		t.Fatalf("want three URLs after --, args = %q", args)
	}
	for i, id := range []string{"video0001", "video0002", "video0003"} {
		if !strings.Contains(args[dash+1+i], id) {
			t.Errorf("URL %d = %q, want it to have %s", i, args[dash+1+i], id)
		}
	}
	joined := strings.Join(args, "\n")
	if !strings.Contains(joined, "--title=ytwatch | ${media-title}") {
		t.Errorf("a queue should show the title of the video playing:\n%s", joined)
	}
	if !strings.Contains(joined, "--ytdl-format=bestvideo[height<=720]") {
		t.Errorf("quality missing:\n%s", joined)
	}
}

func TestStartSingleVideoKeepsItsTitle(t *testing.T) {
	out := fakeMPV(t)
	p, err := Start([]media.Video{{ID: "video0001", Title: "Only one"}}, config.Default(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	_ = p.Wait()
	data, _ := os.ReadFile(out)
	if !strings.Contains(string(data), "--title=ytwatch | Only one") {
		t.Fatalf("args:\n%s", data)
	}
}

func TestStartNeedsAVideo(t *testing.T) {
	if _, err := Start(nil, config.Default(), Options{}); err == nil {
		t.Fatal("want an error")
	}
}
