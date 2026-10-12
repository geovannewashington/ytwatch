package links

import (
	"reflect"
	"regexp"
	"testing"

	"github.com/geovannewashington/ytwatch/internal/config"
)

func TestAnnotateNumbersLinksAndKeepsRepeats(t *testing.T) {
	text := "Site: http://www.rammstein.com. Shop (https://shop.example.com/a_(b)) and again http://www.rammstein.com, bye"
	got, urls := Annotate(text)

	wantURLs := []string{"http://www.rammstein.com", "https://shop.example.com/a_(b)"}
	if !reflect.DeepEqual(urls, wantURLs) {
		t.Fatalf("urls = %v, want %v", urls, wantURLs)
	}
	want := "Site: [1] http://www.rammstein.com. Shop ([2] https://shop.example.com/a_(b)) and again [1] http://www.rammstein.com, bye"
	if got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}

func TestAnnotateWithoutLinks(t *testing.T) {
	got, urls := Annotate("no links here, only ftp://x and text")
	if got != "no links here, only ftp://x and text" || len(urls) != 0 {
		t.Fatalf("got %q, %v", got, urls)
	}
}

func TestTrimTrailing(t *testing.T) {
	for in, want := range map[string]string{
		"https://a.com/x.":       "https://a.com/x",
		"https://a.com/x?!":      "https://a.com/x",
		"https://a.com/x)":       "https://a.com/x",
		"https://a.com/f_(b)":    "https://a.com/f_(b)",
		"https://a.com/p?q=1&r=": "https://a.com/p?q=1&r=",
	} {
		// "=" is not sentence punctuation, so the last case keeps it.
		if got := trimTrailing(in); got != want {
			t.Errorf("trimTrailing(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCommandFirstMatchWinsAndLinkIsOneArgument(t *testing.T) {
	handlers := []config.LinkHandler{
		{Name: "images", Match: regexp.MustCompile(`\.(png|jpe?g)(\?.*)?$`), Command: []string{"nsxiv", "%l"}},
		{Name: "music", Match: regexp.MustCompile(`spotify\.com`), Command: []string{"mpv", "--no-video", "%l"}},
		{Name: "all", Match: regexp.MustCompile(`.`), Command: []string{"echo", "%l"}},
	}
	for url, want := range map[string]struct {
		argv []string
		name string
	}{
		"https://x.com/a b;rm -rf.png?x=1": {[]string{"nsxiv", "https://x.com/a b;rm -rf.png?x=1"}, "images"},
		"https://open.spotify.com/track/1": {[]string{"mpv", "--no-video", "https://open.spotify.com/track/1"}, "music"},
		"https://example.com":              {[]string{"echo", "https://example.com"}, "all"},
	} {
		argv, name, ok := Command(url, handlers)
		if !ok || !reflect.DeepEqual(argv, want.argv) || name != want.name {
			t.Errorf("Command(%q) = %v, %q, %v; want %v, %q", url, argv, name, ok, want.argv, want.name)
		}
	}
	if _, _, ok := Command("https://example.com", handlers[:2]); ok {
		t.Fatal("no handler should match")
	}
}

func TestRun(t *testing.T) {
	if err := Run(nil); err == nil {
		t.Fatal("empty command should fail")
	}
	if err := Run([]string{"definitely-not-installed-xyz", "x"}); err == nil {
		t.Fatal("missing program should fail")
	}
	if err := Run([]string{"true"}); err != nil {
		t.Fatalf("true: %v", err)
	}
}
