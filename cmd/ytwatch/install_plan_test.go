package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/geovannewashington/ytwatch/internal/config"
)

func lookPathOf(found ...string) func(string) (string, error) {
	return func(name string) (string, error) {
		for _, f := range found {
			if f == name {
				return "/usr/bin/" + name, nil
			}
		}

		return "", errors.New("not found")
	}
}

func TestPlanInstallForPacmanKeepsTheOrderAndPicksTheClipboardToolBySession(t *testing.T) {
	missing := []string{"yt-dlp", "mpv", "ffmpeg", "ueberzugpp", "browser", "clipboard", "wmctrl"}

	plan := planInstall(missing, true, lookPathOf("pacman"))
	want := "sudo pacman -S --needed yt-dlp mpv ffmpeg ueberzugpp wl-clipboard wmctrl"
	if plan.command != want {
		t.Errorf("command = %q\nwant      %q", plan.command, want)
	}
	if len(plan.notes) != 1 || !strings.Contains(plan.notes[0], "browser") {
		t.Errorf("a browser cannot be installed by name, it needs a note: %v", plan.notes)
	}

	plan = planInstall([]string{"clipboard"}, false, lookPathOf("pacman"))
	if plan.command != "sudo pacman -S --needed xclip" {
		t.Errorf("an X11 session needs xclip, got %q", plan.command)
	}
}

func TestPlanInstallForAptUsesPipxForYtDlpAndNotesUeberzugpp(t *testing.T) {
	plan := planInstall([]string{"yt-dlp", "mpv", "ueberzugpp"}, false, lookPathOf("apt-get"))

	want := "sudo apt install pipx mpv && pipx install yt-dlp"
	if plan.command != want {
		t.Errorf("command = %q, want %q", plan.command, want)
	}
	if len(plan.notes) != 1 || !strings.Contains(plan.notes[0], "ueberzugpp is not in apt") {
		t.Errorf("ueberzugpp needs a note: %v", plan.notes)
	}

	plan = planInstall([]string{"mpv", "ffmpeg"}, false, lookPathOf("apt-get"))
	if plan.command != "sudo apt install mpv ffmpeg" {
		t.Errorf("without yt-dlp pipx is not needed, got %q", plan.command)
	}
}

func TestPlanInstallWithoutAKnownPackageManagerOnlyListsTheNames(t *testing.T) {
	plan := planInstall([]string{"mpv", "ffmpeg"}, false, lookPathOf())

	if plan.command != "" {
		t.Errorf("no command can be right for an unknown system, got %q", plan.command)
	}
	if len(plan.notes) == 0 || !strings.Contains(plan.notes[0], "mpv, ffmpeg") {
		t.Errorf("the missing programs should be listed: %v", plan.notes)
	}

	if got := planInstall(nil, false, lookPathOf("pacman")); got.command != "" || got.notes != nil {
		t.Errorf("nothing missing needs no plan: %+v", got)
	}
}

func TestDoctorShowsTheInstallCommandOnlyWhenSomethingIsMissing(t *testing.T) {
	var out bytes.Buffer
	code := runDoctor(&out, config.Default(), fakeProbe(nil, "pacman", "yt-dlp", "mpv"))

	if code != 1 || !strings.Contains(out.String(), "sudo pacman -S --needed ffmpeg ueberzugpp") {
		t.Errorf("the report should end with the install command, exit code %d:\n%s", code, out.String())
	}

	out.Reset()
	code = runDoctor(&out, config.Default(), fakeProbe(nil, "yt-dlp", "mpv", "ffmpeg", "ueberzugpp"))

	if code != 0 || strings.Contains(out.String(), "Install what is missing") {
		t.Errorf("nothing is missing, exit code %d:\n%s", code, out.String())
	}
}

func TestInstallCommandPrintsNothingElseAndNothingWhenComplete(t *testing.T) {
	got := installCommand(config.Default(), fakeProbe(nil, "pacman", "yt-dlp"))
	if got != "sudo pacman -S --needed mpv ffmpeg ueberzugpp" {
		t.Errorf("installCommand = %q", got)
	}

	got = installCommand(config.Default(), fakeProbe(nil, "yt-dlp", "mpv", "ffmpeg", "ueberzugpp"))
	if got != "" {
		t.Errorf("nothing is missing, got %q", got)
	}
}
