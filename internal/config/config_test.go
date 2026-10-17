package config

import (
	"reflect"
	"testing"
)

func TestParse(t *testing.T) {
	cfg, err := Parse(`
# comment
[mpv]
workspace = 3
args = ["--ao=pipewire", "--volume=60"] # trailing

[chat]
enabled = false
workspace = "4:chat"
browser = "firefox"
`)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MPV.Workspace != "3" || cfg.Chat.Workspace != "4:chat" || cfg.Chat.Enabled {
		t.Fatalf("unexpected config: %+v", cfg)
	}
	if want := []string{"--ao=pipewire", "--volume=60"}; !reflect.DeepEqual(cfg.MPV.Args, want) {
		t.Fatalf("args = %v", cfg.MPV.Args)
	}
}

func TestParseErrors(t *testing.T) {
	for _, text := range []string{"[mpv]\nbogus = 1", "[mpv]\nworkspace", "[chat]\nenabled = maybe"} {
		if _, err := Parse(text); err == nil {
			t.Errorf("expected error for %q", text)
		}
	}
}

func TestDefaults(t *testing.T) {
	cfg, _ := Parse("")
	if !cfg.Chat.Enabled || cfg.Chat.Browser != "" {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
}

func TestParseCookiesFromBrowser(t *testing.T) {
	cfg, err := Parse("[mpv]\ncookies_from_browser = \"firefox\"")
	if err != nil || cfg.MPV.CookiesFromBrowser != "firefox" {
		t.Fatalf("cfg=%+v err=%v", cfg, err)
	}
}

func TestParseDownloadAndGeneral(t *testing.T) {
	cfg, err := Parse(`
[general]
audio_only = true

[download]
dir = "~/Music/ytwatch"
args = ["--embed-metadata"]
`)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.General.AudioOnly || cfg.Download.Dir != "~/Music/ytwatch" ||
		!reflect.DeepEqual(cfg.Download.Args, []string{"--embed-metadata"}) {
		t.Fatalf("unexpected config: %+v", cfg)
	}
	if def, _ := Parse(""); def.Download.Dir != "~/Downloads/ytwatch" || def.General.AudioOnly {
		t.Fatalf("unexpected defaults: %+v", def)
	}
}

func TestParseTheme(t *testing.T) {
	cfg, err := Parse(`
[theme]
name = "Nord"
accent = "#112233"   # a single colour
live = 9
`)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Theme.Name != "nord" || cfg.Theme.Colors["accent"] != "#112233" || cfg.Theme.Colors["live"] != "9" {
		t.Fatalf("unexpected theme: %+v", cfg.Theme)
	}
	if def, _ := Parse(""); def.Theme.Name != "" || len(def.Theme.Colors) != 0 {
		t.Fatalf("default theme should be empty (the built-in default): %+v", def.Theme)
	}
}

func TestParseThemeErrors(t *testing.T) {
	for _, text := range []string{
		"[theme]\nname = \"nope\"",
		"[theme]\naccent = \"green\"",
		"[theme]\naccent = \"#12345\"",
		"[theme]\nbogus = \"#112233\"",
		"[theme]\nname = nord",
	} {
		if _, err := Parse(text); err == nil {
			t.Errorf("expected error for %q", text)
		}
	}
}

func TestParseLinkHandlersKeepFileOrderAndLiteralStrings(t *testing.T) {
	cfg, err := Parse(`
[general]
browser = "firefox --private-window"

[link.images]
match = '\.(png|jpe?g)(\?.*)?$'   # literal string: no escapes needed
command = ["nsxiv", "%l"]

[link.music]
match = "spotify\\.com"
command = ["mpv", "--no-video", "%l"]
`)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.General.Browser != "firefox --private-window" {
		t.Fatalf("browser = %q", cfg.General.Browser)
	}
	if len(cfg.Links) != 2 || cfg.Links[0].Name != "images" || cfg.Links[1].Name != "music" {
		t.Fatalf("links = %+v", cfg.Links)
	}
	if !cfg.Links[0].Match.MatchString("https://x.com/a.jpg?w=1") || cfg.Links[0].Match.MatchString("https://x.com/a.html") {
		t.Fatal("images pattern does not match as expected")
	}
	if !cfg.Links[1].Match.MatchString("https://open.spotify.com/track/1") {
		t.Fatal("music pattern should match spotify links")
	}
	if want := []string{"mpv", "--no-video", "%l"}; !reflect.DeepEqual(cfg.Links[1].Command, want) {
		t.Fatalf("command = %v", cfg.Links[1].Command)
	}
}

func TestParseLinkHandlerErrors(t *testing.T) {
	for _, text := range []string{
		"[link.a]\nmatch = \"(\"\ncommand = [\"x\"]",   // bad regexp
		"[link.a]\nmatch = \"x\"",                      // no command
		"[link.a]\ncommand = [\"x\"]",                  // no pattern
		"[link.a]\nmatch = \"x\"\ncommand = []",        // empty command
		"[link.a]\nmatch = \"x\"\ncommand = \"nsxiv\"", // not an array
		"[link.a]\nbogus = 1",                          // unknown key
		"[link.]\nmatch = \"x\"\ncommand = [\"x\"]",    // no name
	} {
		if _, err := Parse(text); err == nil {
			t.Errorf("expected error for %q", text)
		}
	}
}

func TestCommentsInsideSingleQuotesAreKept(t *testing.T) {
	if got := stripComment(`match = 'a#b' # note`); got != `match = 'a#b' ` {
		t.Fatalf("got %q", got)
	}
	if got := stripComment(`x = "it's" # note`); got != `x = "it's" ` {
		t.Fatalf("got %q", got)
	}
}

func TestParsePreviewOutput(t *testing.T) {
	cfg, err := Parse("[preview]\noutput = \"X11\"\n")
	if err != nil || cfg.Preview.Output != "x11" {
		t.Fatalf("got %+v, %v", cfg.Preview, err)
	}
	for _, ok := range []string{"auto", "off", "wayland", "sixel", "kitty", "iterm2", "chafa"} {
		if _, err := Parse("[preview]\noutput = \"" + ok + "\"\n"); err != nil {
			t.Errorf("%s should be accepted: %v", ok, err)
		}
	}
	if _, err := Parse("[preview]\noutput = \"fbdev\"\n"); err == nil {
		t.Fatal("unknown output should fail")
	}
}

func TestParseHistory(t *testing.T) {
	if d := Default().History; !d.Enabled || d.MaxAgeDays != 30 || d.MaxEntries != 100 {
		t.Fatalf("defaults: %+v", d)
	}
	cfg, err := Parse("[history]\nenabled = false\nmax_age_days = 7\nmax_entries = 20\n")
	if err != nil {
		t.Fatal(err)
	}
	if h := cfg.History; h.Enabled || h.MaxAgeDays != 7 || h.MaxEntries != 20 {
		t.Fatalf("history: %+v", h)
	}
	if _, err := Parse("[history]\nmax_entries = 0\n"); err == nil {
		t.Fatal("0 entries should be rejected")
	}
}

func TestParseCache(t *testing.T) {
	if c := Default().Cache; c.MaxAgeDays != 7 || c.MaxSizeMB != 100 {
		t.Fatalf("defaults: %+v", c)
	}
	cfg, err := Parse("[cache]\nmax_age_days = 3\nmax_size_mb = 20\n")
	if err != nil {
		t.Fatal(err)
	}
	if c := cfg.Cache; c.MaxAgeDays != 3 || c.MaxSizeMB != 20 {
		t.Fatalf("cache: %+v", c)
	}
	for _, bad := range []string{"max_age_days = 0", "max_size_mb = 0", "max_size_mb = 1000000"} {
		if _, err := Parse("[cache]\n" + bad + "\n"); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

func TestParseQuality(t *testing.T) {
	if Default().General.Quality != "best" {
		t.Fatalf("default quality = %q", Default().General.Quality)
	}
	cfg, err := Parse("[general]\nquality = \"720P\"\n")
	if err != nil || cfg.General.Quality != "720p" {
		t.Fatalf("quality = %q, err = %v", cfg.General.Quality, err)
	}
	if _, err := Parse("[general]\nquality = \"4k\"\n"); err == nil {
		t.Fatal("4k should be rejected")
	}
}

func TestParseSearchLocale(t *testing.T) {
	if d := Default().Search; d.Language != "" || d.Region != "" {
		t.Fatalf("defaults: %+v", d)
	}
	cfg, err := Parse("[search]\nlanguage = \"pt-br\"\nregion = \"br\"\n")
	if err != nil || cfg.Search.Language != "pt-BR" || cfg.Search.Region != "BR" {
		t.Fatalf("search: %+v, err %v", cfg.Search, err)
	}
	for _, bad := range []string{"language = \"portuguese\"", "region = \"BRA\""} {
		if _, err := Parse("[search]\n" + bad + "\n"); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

func TestHistoryLimitsHaveAnUpperBound(t *testing.T) {
	for _, bad := range []string{"max_age_days = 999999", "max_age_days = 0", "max_entries = 1000000"} {
		if _, err := Parse("[history]\n" + bad + "\n"); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
	if _, err := Parse("[history]\nmax_age_days = 36500\nmax_entries = 100000\n"); err != nil {
		t.Errorf("the largest allowed values were rejected: %v", err)
	}
}

func TestParseUIAnimations(t *testing.T) {
	if Default().UI.Animations != "full" {
		t.Fatalf("default = %q", Default().UI.Animations)
	}
	for _, value := range []string{"full", "minimal", "off"} {
		cfg, err := Parse("[ui]\nanimations = \"" + value + "\"\n")
		if err != nil || cfg.UI.Animations != value {
			t.Errorf("%s: %v %q", value, err, cfg.UI.Animations)
		}
	}
	if _, err := Parse("[ui]\nanimations = \"lots\"\n"); err == nil {
		t.Error("an unknown level should be rejected")
	}
}
