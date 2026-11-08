package player

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
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

func TestStartEscapesDollarInTheTitle(t *testing.T) {
	out := fakeMPV(t)
	p, err := Start(
		[]media.Video{{ID: "video0001", Title: "Cost: ${media-title} $5"}},
		config.Default(),
		Options{},
	)
	if err != nil {
		t.Fatal(err)
	}
	_ = p.Wait()
	data, _ := os.ReadFile(out)
	if !strings.Contains(string(data), "--title=ytwatch | Cost: $${media-title} $$5") {
		t.Fatalf("mpv would expand the title:\n%s", data)
	}
}

func TestSubtitleArgs(t *testing.T) {
	for name, tc := range map[string]struct {
		mpv  config.MPV
		want []string
	}{
		"off":            {config.MPV{}, nil},
		"auto only":      {config.MPV{AutoSubtitles: true}, nil},
		"manual":         {config.MPV{Subtitles: "en,pt-BR"}, []string{"--slang=en,pt-BR"}},
		"with automatic": {config.MPV{Subtitles: "en", AutoSubtitles: true}, []string{"--slang=en", "--ytdl-raw-options-append=write-auto-subs=", "--ytdl-raw-options-append=sub-langs=en"}},
		"several languages": {config.MPV{Subtitles: "en,pt-BR", AutoSubtitles: true},
			[]string{"--slang=en,pt-BR", "--ytdl-raw-options-append=write-auto-subs=", "--ytdl-raw-options-append=sub-langs=en|pt-BR"}},
	} {
		got := subtitleArgs(tc.mpv)
		if strings.Join(got, "\n") != strings.Join(tc.want, "\n") {
			t.Errorf("%s: %q, want %q", name, got, tc.want)
		}
	}
}

func TestStartPassesTheSubtitleLanguages(t *testing.T) {
	out := fakeMPV(t)
	cfg := config.Default()
	cfg.MPV.Subtitles, cfg.MPV.AutoSubtitles = "pt-BR,en", true

	p, err := Start([]media.Video{{ID: "video0001", Title: "A"}}, cfg, Options{})
	if err != nil {
		t.Fatal(err)
	}
	_ = p.Wait()

	data, _ := os.ReadFile(out)
	for _, want := range []string{"--slang=pt-BR,en", "--ytdl-raw-options-append=write-auto-subs=", "--ytdl-raw-options-append=sub-langs=pt-BR|en"} {
		if !strings.Contains(string(data), want+"\n") {
			t.Errorf("mpv must get %q as one argument:\n%s", want, data)
		}
	}
}

func TestResumeArgs(t *testing.T) {
	old := mpvKnows
	t.Cleanup(func() { mpvKnows = old })

	mpvKnows = func(string) bool { return true }
	if got := resumeArgs(Options{}); got != nil {
		t.Errorf("off by default: %q", got)
	}

	got := strings.Join(resumeArgs(Options{ResumeDir: "/state/watch_later"}), "\n")
	for _, want := range []string{"--save-position-on-quit", "--watch-later-directory=/state/watch_later",
		"--write-filename-in-watch-later-config", "--watch-later-options=start"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	if strings.Contains(got, "resume-playback") {
		t.Errorf("a normal play resumes:\n%s", got)
	}

	if got := resumeArgs(Options{ResumeDir: "/d", StartOver: true}); !slices.Contains(got, "--no-resume-playback") {
		t.Errorf("starting over must not resume: %q", got)
	}

	// An mpv that does not know the option must not be given it: it would stop.
	mpvKnows = func(string) bool { return false }
	if got := resumeArgs(Options{ResumeDir: "/d"}); slices.Contains(got, "--watch-later-options=start") {
		t.Errorf("an old mpv does not know --watch-later-options: %q", got)
	}
}

func TestStartPassesTheResumeArguments(t *testing.T) {
	old := mpvKnows
	t.Cleanup(func() { mpvKnows = old })
	mpvKnows = func(string) bool { return true }

	out := fakeMPV(t)
	p, err := Start([]media.Video{{ID: "video0001", Title: "A"}}, config.Default(), Options{ResumeDir: "/state/wl"})
	if err != nil {
		t.Fatal(err)
	}
	_ = p.Wait()

	data, _ := os.ReadFile(out)
	if !strings.Contains(string(data), "--watch-later-directory=/state/wl\n") || !strings.Contains(string(data), "--save-position-on-quit\n") {
		t.Errorf("args:\n%s", data)
	}
}

func TestMpvKnowsReadsTheOptionList(t *testing.T) {
	dir := t.TempDir()
	list := "#!/bin/sh\nprintf ' --watch-later-options            String list (default: start)\\n --save-position-on-quit          Flag\\n'\n"
	if err := os.WriteFile(filepath.Join(dir, "mpv"), []byte(list), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	knownOptionsOnce, knownOptions = sync.Once{}, ""
	t.Cleanup(func() { knownOptionsOnce, knownOptions = sync.Once{}, "" })

	if !mpvKnows("--watch-later-options") || !mpvKnows("--save-position-on-quit") || mpvKnows("--watch-later") || mpvKnows("--nothing") {
		t.Error("an option is known only when it is a whole name in the list")
	}
}
