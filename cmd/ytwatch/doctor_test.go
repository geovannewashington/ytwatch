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
		clipboard:     func() (string, error) { return "wl-copy", nil },
		browser:       func(config.Config) string { return "firefox" },
	}
}

func TestDoctorAllRequiredFound(t *testing.T) {
	var out bytes.Buffer
	p := fakeProbe(map[string]string{"DISPLAY": ":0"}, "yt-dlp", "mpv", "ffmpeg", "ueberzugpp", "wmctrl")
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
	for _, want := range []string{"missing", "yt-dlp", "pipx install yt-dlp", "mpv", "ffmpeg", "ueberzugpp", "3 required programs missing", "console or SSH"} {
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

func TestDoctorTreatsEveryProgramAsRequired(t *testing.T) {
	var out bytes.Buffer
	p := fakeProbe(map[string]string{"DISPLAY": ":0"}, "yt-dlp", "mpv")
	p.browser = func(config.Config) string { return "" }
	p.clipboard = func() (string, error) { return "", errors.New("none") }

	code := runDoctor(&out, config.Default(), p)
	text := out.String()

	if code != 1 {
		t.Errorf("a missing program must fail the doctor, exit code %d", code)
	}

	for _, name := range []string{"ffmpeg", "browser", "clipboard", "wmctrl"} {
		if !strings.Contains(text, "missing  "+name) {
			t.Errorf("%s should be reported as missing:\n%s", name, text)
		}
	}

	if !strings.Contains(text, "4 required programs missing") {
		t.Errorf("the summary should count all of them:\n%s", text)
	}
	if strings.Contains(text, "missing  ueberzugpp") {
		t.Errorf("ueberzugpp is optional:\n%s", text)
	}
}

func TestDoctorDoesNotNeedUeberzugpp(t *testing.T) {
	var out bytes.Buffer
	code := runDoctor(&out, config.Default(), fakeProbe(map[string]string{"WAYLAND_DISPLAY": "w"}, "yt-dlp", "mpv", "ffmpeg"))
	text := out.String()

	if code != 0 || !strings.Contains(text, "half-blocks") || !strings.Contains(text, "none") {
		t.Errorf("without ueberzugpp thumbnails are half-blocks, exit code %d:\n%s", code, text)
	}
}

func TestDoctorReportsKittyAndTmuxPassthrough(t *testing.T) {
	env := map[string]string{"KITTY_WINDOW_ID": "1", "TMUX": "/tmp/tmux-1000/default,1,0"}
	p := fakeProbe(env, "yt-dlp", "mpv", "ffmpeg")
	p.kittyDetected = func() bool { return true }
	p.tmuxPassthrough = func() string { return "off" }

	var out bytes.Buffer
	runDoctor(&out, config.Default(), p)
	for _, want := range []string{"kitty graphics", "allow-passthrough"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("report is missing %q:\n%s", want, out.String())
		}
	}

	for _, value := range []string{"on", "all"} {
		p.tmuxPassthrough = func() string { return value }
		out.Reset()
		runDoctor(&out, config.Default(), p)
		if strings.Contains(out.String(), "allow-passthrough") {
			t.Errorf("no warning is needed when it is %s:\n%s", value, out.String())
		}
	}
}

func TestDoctorExplicitKittyNeedsNoDetection(t *testing.T) {
	cfg := config.Default()
	cfg.Preview.Output = "kitty"

	var out bytes.Buffer
	runDoctor(&out, cfg, fakeProbe(nil, "yt-dlp", "mpv", "ffmpeg"))
	if !strings.Contains(out.String(), "kitty graphics · set in config.toml") {
		t.Errorf("report should say kitty was set:\n%s", out.String())
	}
}

func TestDoctorUeberzugppSetButMissing(t *testing.T) {
	cfg := config.Default()
	cfg.Preview.Output = "ueberzugpp"

	var out bytes.Buffer
	runDoctor(&out, cfg, fakeProbe(nil, "yt-dlp", "mpv", "ffmpeg"))
	if !strings.Contains(out.String(), "ueberzugpp is not installed · no thumbnails") {
		t.Errorf("report should say there are no thumbnails:\n%s", out.String())
	}
}

func TestDoctorDoesNotAskForWmctrlOnI3(t *testing.T) {
	p := fakeProbe(map[string]string{"DISPLAY": ":0"}, "yt-dlp", "mpv", "ffmpeg", "ueberzugpp")
	p.windowManager = func() string { return "i3" }

	var out bytes.Buffer
	code := runDoctor(&out, config.Default(), p)

	if code != 0 || strings.Contains(out.String(), "wmctrl") {
		t.Errorf("i3 moves windows itself, exit code %d:\n%s", code, out.String())
	}
}
