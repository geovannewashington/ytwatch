package download

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/geovannewashington/ytwatch/internal/config"
	"github.com/geovannewashington/ytwatch/internal/media"
)

func TestBuildArgsAudioOnlyAndUserArgsLast(t *testing.T) {
	cfg := config.Default()
	cfg.MPV.CookiesFromBrowser = "firefox"
	cfg.Download.Args = []string{"--embed-metadata"}

	args := buildArgs("https://youtu.be/x", "/tmp/d", cfg, Options{AudioOnly: true})

	want := []string{"-f", audioFormat, "--cookies-from-browser", "firefox", "--embed-metadata", "--", "https://youtu.be/x"}
	if got := args[len(args)-len(want):]; !reflect.DeepEqual(got, want) {
		t.Fatalf("tail = %v, want %v", got, want)
	}
}

func TestBuildArgsVideoHasNoFormat(t *testing.T) {
	for _, a := range buildArgs("u", "/tmp/d", config.Default(), Options{}) {
		if a == "-f" {
			t.Fatal("video download should use the yt-dlp default format")
		}
	}
}

func TestBuildArgsQualityLimitsTheHeight(t *testing.T) {
	args := buildArgs("u", "/tmp/d", config.Default(), Options{Quality: "720p"})
	i := slices.Index(args, "-f")
	if i < 0 || args[i+1] != "bestvideo[height<=720]+bestaudio/best[height<=720]/best" {
		t.Fatalf("args = %v", args)
	}
	// Audio-only wins over a quality: there is no video to limit.
	args = buildArgs("u", "/tmp/d", config.Default(), Options{Quality: "720p", AudioOnly: true})
	if i := slices.Index(args, "-f"); i < 0 || args[i+1] != audioFormat {
		t.Fatalf("args = %v", args)
	}
}

func TestExpandDir(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	got, _ := ExpandDir("~/Videos/ytwatch")
	if want := filepath.Join(home, "Videos", "ytwatch"); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got, _ := ExpandDir("/abs/dir"); got != "/abs/dir" {
		t.Fatalf("got %q", got)
	}
}

func TestFailureReason(t *testing.T) {
	stderr := "WARNING: x\nERROR: [youtube] abc: Video unavailable\n"
	if got := failureReason(stderr, errors.New("exit status 1")); got != "[youtube] abc: Video unavailable" {
		t.Fatalf("got %q", got)
	}
	blocked := "ERROR: [youtube] abc: Sign in to confirm you're not a bot.\n"
	if got := failureReason(blocked, errors.New("exit status 1")); !strings.Contains(got, "cookies_from_browser") {
		t.Fatalf("got %q", got)
	}
	if got := failureReason("", errors.New("exit status 1")); got != "exit status 1" {
		t.Fatalf("got %q", got)
	}
}

func TestLastLine(t *testing.T) {
	if got := lastLine("a\n/home/me/v.mp4\n"); got != "/home/me/v.mp4" {
		t.Fatalf("got %q", got)
	}
}

func TestRunRejectsLiveAndUpcoming(t *testing.T) {
	cfg := config.Default()
	for _, v := range []media.Video{
		{ID: "video0001", IsLive: true},
		{ID: "video0002", LiveStatus: "is_upcoming"},
	} {
		if _, err := Run(context.Background(), v, cfg, Options{}); err == nil {
			t.Fatalf("want error for %+v", v)
		}
	}
}
