package resume

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func write(t *testing.T, dir, name, content string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}

func TestLoadReadsWhatMpvWrites(t *testing.T) {
	dir := t.TempDir()
	// The first line is what mpv writes with --write-filename-in-watch-later-config.
	write(t, dir, "75170FC2", "# https://www.youtube.com/watch?v=dQw4w9WgXcQ\nstart=42.640000\n")
	write(t, dir, "A1", "# https://youtube.com/watch?v=abcdefghijk&t=5\nstart=3600\n")
	write(t, dir, "B2", "# https://www.youtube.com/watch?v=zzzzzzzzzzz\nstart=7.5\nvolume=50\n")

	got := Load(dir)
	if len(got) != 3 || got["dQw4w9WgXcQ"] != 42640*time.Millisecond || got["abcdefghijk"] != time.Hour || got["zzzzzzzzzzz"] != 7500*time.Millisecond {
		t.Errorf("Load = %v", got)
	}
}

func TestLoadSkipsWhatIsNotAYouTubeWatchLaterFile(t *testing.T) {
	dir := t.TempDir()
	for name, content := range map[string]string{
		"local":      "# /home/me/video.mkv\nstart=100\n",
		"other site": "# https://example.com/watch?v=abcdefghijk\nstart=100\n",
		"lookalike":  "# https://youtube.com.evil.example/watch?v=abcdefghijk\nstart=100\n",
		"not watch":  "# https://www.youtube.com/playlist?list=abcdefghijk\nstart=100\n",
		"bad id":     "# https://www.youtube.com/watch?v=a b\nstart=100\n",
		"short id":   "# https://www.youtube.com/watch?v=abc\nstart=100\n",
		"no start":   "# https://www.youtube.com/watch?v=abcdefghijk\nvolume=50\n",
		"bad start":  "# https://www.youtube.com/watch?v=abcdefghijk\nstart=banana\n",
		"negative":   "# https://www.youtube.com/watch?v=abcdefghijk\nstart=-5\n",
		"huge":       "# https://www.youtube.com/watch?v=abcdefghijk\nstart=1e300\n",
		"no address": "start=100\n",
		"empty":      "",
		"binary":     "\x00\x01\x02\xff",
		"javascript": "# javascript:alert(1)\nstart=1\n",
	} {
		write(t, dir, name, content)
	}
	if err := os.Mkdir(filepath.Join(dir, "a-folder"), 0o700); err != nil {
		t.Fatal(err)
	}

	if got := Load(dir); len(got) != 0 {
		t.Errorf("nothing here is a position of a video: %v", got)
	}
}

func TestLoadReadsALargeFileOnlyUpToALimit(t *testing.T) {
	dir := t.TempDir()
	// The start line is after the limit, so the file does not count.
	write(t, dir, "big", "# https://www.youtube.com/watch?v=abcdefghijk\n"+strings.Repeat("x=1\n", maxFileSize)+"start=100\n")

	if got := Load(dir); len(got) != 0 {
		t.Errorf("a file over the limit must not be read in full: %v", got)
	}
}

func TestLoadWithoutAFolder(t *testing.T) {
	if got := Load(""); len(got) != 0 {
		t.Error("an empty path gives nothing")
	}
	if got := Load(filepath.Join(t.TempDir(), "missing")); len(got) != 0 {
		t.Error("a missing folder gives nothing")
	}
}

func TestResumable(t *testing.T) {
	hour := time.Hour
	for name, tc := range map[string]struct {
		position, duration time.Duration
		want               bool
	}{
		"in the middle":          {30 * time.Minute, hour, true},
		"just started":           {5 * time.Second, hour, false},
		"ten seconds in":         {MinPosition, hour, true},
		"near the end":           {58 * time.Minute, hour, false},
		"right at the threshold": {time.Duration(float64(hour) * Finished), hour, false},
		"just before it":         {time.Duration(float64(hour)*Finished) - time.Second, hour, true},
		"length unknown":         {30 * time.Minute, 0, true},
	} {
		if got := Resumable(tc.position, tc.duration); got != tc.want {
			t.Errorf("%s: Resumable(%v, %v) = %v, want %v", name, tc.position, tc.duration, got, tc.want)
		}
	}
}

func TestFraction(t *testing.T) {
	if got := Fraction(30*time.Minute, time.Hour); got != 0.5 {
		t.Errorf("Fraction = %v", got)
	}
	for _, tc := range [][2]time.Duration{{0, time.Hour}, {time.Hour, 0}, {-time.Second, time.Hour}} {
		if got := Fraction(tc[0], tc[1]); got != 0 {
			t.Errorf("Fraction(%v, %v) = %v, want 0", tc[0], tc[1], got)
		}
	}
	if got := Fraction(2*time.Hour, time.Hour); got != 1 {
		t.Errorf("past the end counts as the end, got %v", got)
	}
}

func TestVideoID(t *testing.T) {
	for address, want := range map[string]string{
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ":        "dQw4w9WgXcQ",
		"https://youtube.com/watch?v=dQw4w9WgXcQ&list=PL1":   "dQw4w9WgXcQ",
		"http://WWW.YOUTUBE.COM/watch?v=dQw4w9WgXcQ":         "dQw4w9WgXcQ",
		"https://m.youtube.com/watch?v=dQw4w9WgXcQ":          "",
		"https://youtu.be/dQw4w9WgXcQ":                       "",
		"https://www.youtube.com/watch":                      "",
		"ftp://www.youtube.com/watch?v=dQw4w9WgXcQ":          "",
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ%0Aevil": "",
	} {
		got, ok := VideoID(address)
		if got != want || ok != (want != "") {
			t.Errorf("VideoID(%q) = %q, %v; want %q", address, got, ok, want)
		}
	}
}

func TestPruneDeletesOldFilesOnly(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()

	old := write(t, dir, "old", "# https://www.youtube.com/watch?v=abcdefghijk\nstart=100\n")
	fresh := write(t, dir, "fresh", "# https://www.youtube.com/watch?v=zzzzzzzzzzz\nstart=100\n")
	if err := os.Chtimes(old, now.Add(-40*24*time.Hour), now.Add(-40*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "folder"), 0o700); err != nil {
		t.Fatal(err)
	}

	Prune(dir, 30*24*time.Hour, now)

	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Error("a file older than the limit must go")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Error("a recent file must stay")
	}
	if _, err := os.Stat(filepath.Join(dir, "folder")); err != nil {
		t.Error("only files are deleted")
	}

	Prune("", time.Hour, now)                            // no folder: nothing happens
	Prune(dir, 0, now)                                   // no age: nothing is deleted
	Prune(filepath.Join(dir, "missing"), time.Hour, now) // a folder that is not there
	if _, err := os.Stat(fresh); err != nil {
		t.Error("a limit of zero must delete nothing")
	}
}

func TestPrepareMakesAPrivateFolder(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state", "watch_later")
	if err := Prepare(dir); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
		t.Errorf("the folder must be for the user only: %v, %v", info, err)
	}
	if Prepare("") == nil {
		t.Error("no folder, no way to prepare it")
	}
}

func TestDirFollowsTheStateHome(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/tmp/state-home")
	if got := Dir(); got != "/tmp/state-home/ytwatch/watch_later" {
		t.Errorf("Dir = %q", got)
	}
}
