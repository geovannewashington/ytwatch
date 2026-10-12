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
