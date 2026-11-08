package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/geovannewashington/ytwatch/internal/media"
	"github.com/geovannewashington/ytwatch/internal/player"
)

const hourVideo = 3600.0

// positionFile writes a watch later file the way mpv does.
func positionFile(t *testing.T, dir, id string, seconds int) {
	t.Helper()

	text := "# https://www.youtube.com/watch?v=" + id + "\nstart=" + itoa(seconds) + ".000000\n"
	if err := os.WriteFile(filepath.Join(dir, "F"+id), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for ; n > 0; n /= 10 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
	}

	return string(digits)
}

func plainVideo(id string) media.Video {
	return media.Video{ID: id, Title: "Video " + id, Duration: hourVideo, LiveStatus: "not_live"}
}

func newResumeModel(t *testing.T, videos ...media.Video) (*Model, string) {
	t.Helper()

	dir := t.TempDir()
	m := New("test")
	m.width, m.height = 120, 40
	m.items = videos
	m.SetResume(dir)

	return m, dir
}

func TestProgressTextShowsHowMuchWasWatched(t *testing.T) {
	m, dir := newResumeModel(t)
	positionFile(t, dir, "midvideo001", 2160) // 36 minutes of an hour
	positionFile(t, dir, "almostdone01", 3500)
	positionFile(t, dir, "justbegun001", 4)
	m.SetResume(dir)

	live := plainVideo("midvideo001")
	live.LiveStatus = "is_live"
	short := plainVideo("midvideo001")
	short.Short = true
	unknown := plainVideo("midvideo001")
	unknown.Duration = 0

	for name, tc := range map[string]struct {
		video    media.Video
		fraction float64
		ok       bool
		preview  string
	}{
		"in the middle":  {plainVideo("midvideo001"), 0.6, true, "36:00 / 1:00:00"},
		"near the end":   {plainVideo("almostdone01"), 1, true, "watched"},
		"just begun":     {plainVideo("justbegun001"), 0, false, ""},
		"not seen":       {plainVideo("neverseen001"), 0, false, ""},
		"a live stream":  {live, 0, false, ""},
		"a short":        {short, 0, false, ""},
		"unknown length": {unknown, 0, false, "at 36:00"},
	} {
		if got, ok := m.watchedFraction(tc.video); got != tc.fraction || ok != tc.ok {
			t.Errorf("%s: watchedFraction = %v, %v, want %v, %v", name, got, ok, tc.fraction, tc.ok)
		}
		got := m.previewProgress(tc.video, 40)
		if tc.preview == "" && got != "" || !strings.Contains(got, tc.preview) {
			t.Errorf("%s: previewProgress = %q, want %q", name, got, tc.preview)
		}
	}
}

func TestProgressBar(t *testing.T) {
	for fraction, want := range map[float64]string{0: "─────", 0.1: "━────", 0.5: "━━━──", 0.96: "━━━━━", 1: "━━━━━", 2: "━━━━━", -1: "─────"} {
		if got := progressBar(fraction, 5); got != want {
			t.Errorf("progressBar(%v) = %q, want %q", fraction, got, want)
		}
	}
}

func TestListAndPreviewShowTheProgress(t *testing.T) {
	m, dir := newResumeModel(t, plainVideo("midvideo001"))
	positionFile(t, dir, "midvideo001", 2160)
	m.SetResume(dir)

	if bar := m.listBar(m.items[0]); bar != "━━━━━───" {
		t.Errorf("the row must end with the bar, got %q", bar)
	}
	if out := m.View(); !strings.Contains(out, "━━━━━───") || !strings.Contains(out, "36:00 / 1:00:00") {
		t.Errorf("the list and the preview must show where the video was left:\n%s", out)
	}
}

func TestResumeOptions(t *testing.T) {
	m, dir := newResumeModel(t)
	positionFile(t, dir, "almostdone01", 3500)
	positionFile(t, dir, "midvideo001", 2160)
	m.SetResume(dir)

	live := plainVideo("livevideo001")
	live.LiveStatus = "is_live"

	if got := New("x").resumeOptions([]media.Video{plainVideo("midvideo001")}, player.Options{}); got.ResumeDir != "" {
		t.Errorf("without a folder nothing is remembered: %+v", got)
	}

	got := m.resumeOptions([]media.Video{plainVideo("midvideo001")}, player.Options{})
	if got.ResumeDir != dir || got.StartOver {
		t.Errorf("a video left halfway resumes: %+v", got)
	}

	if got := m.resumeOptions([]media.Video{plainVideo("almostdone01")}, player.Options{}); !got.StartOver || got.ResumeDir != dir {
		t.Errorf("a video left near its end starts over: %+v", got)
	}

	if got := m.resumeOptions([]media.Video{live}, player.Options{}); got.ResumeDir != "" {
		t.Errorf("a live stream has no position: %+v", got)
	}
	if got := m.resumeOptions([]media.Video{plainVideo("midvideo001"), live}, player.Options{}); got.ResumeDir != "" {
		t.Errorf("a queue with a live stream shares one mpv, so none is remembered: %+v", got)
	}
	if got := m.resumeOptions([]media.Video{plainVideo("midvideo001"), plainVideo("othervideo01")}, player.Options{}); got.ResumeDir != dir || got.StartOver {
		t.Errorf("a queue of videos is remembered: %+v", got)
	}

	if got := m.resumeOptions([]media.Video{plainVideo("midvideo001")}, player.Options{StartOver: true}); !got.StartOver {
		t.Error("what the user asked for stays")
	}
}

// fakeMPV records its arguments, one per line, and does nothing else.
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

func mpvArgs(t *testing.T, out string) string {
	t.Helper()

	for range 100 {
		if data, err := os.ReadFile(out); err == nil && strings.Contains(string(data), "--\n") {
			return string(data)
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("mpv was not started")

	return ""
}

func TestEnterResumesAndSStartsOver(t *testing.T) {
	out := fakeMPV(t)
	v := plainVideo("midvideo001")
	v.WebpageURL = "https://www.youtube.com/watch?v=midvideo001"
	m, dir := newResumeModel(t, v)
	positionFile(t, dir, "midvideo001", 2160)
	m.SetResume(dir)

	if cmd := m.playSelected(); cmd == nil {
		t.Fatal("nothing started")
	}
	args := mpvArgs(t, out)
	if !strings.Contains(args, "--watch-later-directory="+dir+"\n") || !strings.Contains(args, "--save-position-on-quit\n") ||
		strings.Contains(args, "--no-resume-playback") {
		t.Errorf("Enter must save and resume:\n%s", args)
	}
	if !strings.HasPrefix(m.status, "Resuming at 36:00") {
		t.Errorf("status = %q", m.status)
	}

	// s plays the same video from the start (it is playing: leave it first).
	delete(m.playing, v.ID)
	os.Remove(out)
	m.handleKey(runeKey('s'))
	args = mpvArgs(t, out)
	if !strings.Contains(args, "--no-resume-playback\n") || !strings.Contains(args, "--save-position-on-quit\n") {
		t.Errorf("s must start over and still save the new position:\n%s", args)
	}
	if strings.HasPrefix(m.status, "Resuming") {
		t.Errorf("status = %q", m.status)
	}
}

func TestAVideoLeftNearItsEndStartsOverOnEnter(t *testing.T) {
	out := fakeMPV(t)
	v := plainVideo("almostdone01")
	v.WebpageURL = "https://www.youtube.com/watch?v=almostdone01"
	m, dir := newResumeModel(t, v)
	positionFile(t, dir, "almostdone01", 3500)
	m.SetResume(dir)

	m.playSelected()
	if args := mpvArgs(t, out); !strings.Contains(args, "--no-resume-playback\n") {
		t.Errorf("a video that counts as watched starts from the start:\n%s", args)
	}
}

func TestPlayerExitReadsThePositionMpvWrote(t *testing.T) {
	m, dir := newResumeModel(t, plainVideo("midvideo001"))
	if m.listBar(m.items[0]) != "" {
		t.Fatal("nothing was left yet")
	}

	positionFile(t, dir, "midvideo001", 1800) // mpv wrote it when it quit
	m.Update(playerExitedMsg{ids: []string{"midvideo001"}})
	if got := m.listBar(m.items[0]); got != "━━━━────" {
		t.Errorf("the list must show the new position, got %q", got)
	}
}

func TestWithoutAResumeFolderNothingChanges(t *testing.T) {
	m := New("test")
	m.items = []media.Video{plainVideo("midvideo001")}
	if m.listBar(m.items[0]) != "" || m.previewProgress(m.items[0], 40) != "" {
		t.Error("no folder, no progress")
	}
	m.Update(playerExitedMsg{ids: []string{"midvideo001"}}) // must not read anything
}

func TestAResultWithoutDetailsGetsASocketForALateChatReplay(t *testing.T) {
	out := fakeMPV(t)
	v := plainVideo("midvideo001")
	v.WebpageURL = "https://www.youtube.com/watch?v=midvideo001"
	m, _ := newResumeModel(t, v)
	m.cfg.Chat.Enabled = true

	m.playSelected()
	if args := mpvArgs(t, out); !strings.Contains(args, "--input-ipc-server=") {
		t.Errorf("the chat replay of a stream that is not known yet needs the socket:\n%s", args)
	}
}
