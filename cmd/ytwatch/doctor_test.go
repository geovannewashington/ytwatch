package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/geovannewashington/ytwatch/internal/config"
)

func fakeProbe(env map[string]string, installed ...string) probe {
	return probe{
		getenv: func(name string) string { return env[name] },
		lookPath: func(name string) (string, error) {
			for _, n := range installed {
				if n == name {
					return "/usr/bin/" + name, nil
				}
			}
			return "", errors.New("not found")
		},
		version:       func(program, _ string) string { return program + " 1.0" },
		windowManager: func() string { return "" },
		clipboard:     func() (string, error) { return "", errors.New("none") },
		browser:       func(config.Config) string { return "" },
	}
}

func TestDoctorAllRequiredFound(t *testing.T) {
	var out bytes.Buffer
	p := fakeProbe(map[string]string{"DISPLAY": ":0"}, "yt-dlp", "mpv", "ueberzugpp")
	p.windowManager = func() string { return "bspwm" }
	code := runDoctor(&out, config.Default(), p)

	text := out.String()
	if code != 0 {
		t.Fatalf("exit code = %d\n%s", code, text)
	}
	for _, want := range []string{"X11", "bspwm", "ueberzugpp, output: x11", "yt-dlp 1.0", "All required programs found"} {
		if !strings.Contains(text, want) {
			t.Errorf("report is missing %q:\n%s", want, text)
		}
	}
	if !strings.Contains(text, "wmctrl") {
		t.Errorf("an X11 session should list wmctrl:\n%s", text)
	}
}

func TestDoctorReportsMissingRequiredPrograms(t *testing.T) {
	var out bytes.Buffer
	code := runDoctor(&out, config.Default(), fakeProbe(nil))
	text := out.String()
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	for _, want := range []string{"missing", "yt-dlp", "pipx install yt-dlp", "mpv", "2 required programs missing", "console or SSH"} {
		if !strings.Contains(text, want) {
			t.Errorf("report is missing %q:\n%s", want, text)
		}
	}
}

func TestDoctorThumbnailsOffInConfig(t *testing.T) {
	var out bytes.Buffer
	cfg := config.Default()
	cfg.Preview.Output = "off"
	runDoctor(&out, cfg, fakeProbe(map[string]string{"WAYLAND_DISPLAY": "w"}, "yt-dlp", "mpv", "ueberzugpp"))
	if !strings.Contains(out.String(), "off in config.toml") {
		t.Fatalf("report should say thumbnails are off:\n%s", out.String())
	}
}

func TestDoctorWarnsWhenTmuxFocusEventsAreOff(t *testing.T) {
	p := fakeProbe(map[string]string{"TMUX": "/tmp/tmux-1000/default,1,0"}, "yt-dlp", "mpv", "ueberzugpp")
	p.tmuxFocusEvents = func() string { return "off" }
	var out bytes.Buffer
	runDoctor(&out, config.Default(), p)
	if !strings.Contains(out.String(), "focus-events") {
		t.Errorf("the doctor should mention focus-events, got:\n%s", out.String())
	}
	p.tmuxFocusEvents = func() string { return "on" }
	out.Reset()
	runDoctor(&out, config.Default(), p)
	if strings.Contains(out.String(), "focus-events") {
		t.Errorf("no warning is needed when it is on, got:\n%s", out.String())
	}
}
