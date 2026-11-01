package main

import "strings"

// installPlan is how to install missing programs with the package manager of
// the system.
type installPlan struct {
	command string   // one shell command, or "" when there is none
	notes   []string // what the command cannot cover
}

// planInstall turns the names of the missing programs (the names diagnose
// uses) into an installPlan. Only pacman and apt are known: they cover the
// systems ytwatch is used on, and a wrong package name would be worse than
// none. For other systems the plan only lists what to install.
func planInstall(missing []string, wayland bool, lookPath func(string) (string, error)) installPlan {
	if len(missing) == 0 {
		return installPlan{}
	}

	has := func(name string) bool {
		_, err := lookPath(name)

		return err == nil
	}

	clipboard := "xclip"
	if wayland {
		clipboard = "wl-clipboard"
	}

	switch {
	case has("pacman"):
		return planPacman(missing, clipboard)
	case has("apt-get"):
		return planApt(missing, clipboard)
	}

	return installPlan{notes: []string{
		"Install with your package manager: " + strings.Join(missing, ", "),
		"(ytwatch knows pacman and apt; see the README for other systems.)",
	}}
}

const browserNote = "A web browser: install one, for example firefox."

func planPacman(missing []string, clipboard string) installPlan {
	var packages []string
	var plan installPlan

	for _, name := range missing {
		switch name {
		case "browser":
			plan.notes = append(plan.notes, browserNote)
		case "clipboard":
			packages = append(packages, clipboard)
		default: // yt-dlp, mpv, ffmpeg, ueberzugpp and wmctrl have their own names
			packages = append(packages, name)
		}
	}

	if len(packages) > 0 {
		plan.command = "sudo pacman -S --needed " + strings.Join(packages, " ")
	}

	return plan
}

// planApt installs yt-dlp with pipx, because the version in apt is often too
// old for YouTube, and has no package for ueberzugpp.
func planApt(missing []string, clipboard string) installPlan {
	var packages []string
	var plan installPlan
	pipx := false

	for _, name := range missing {
		switch name {
		case "browser":
			plan.notes = append(plan.notes, browserNote)
		case "clipboard":
			packages = append(packages, clipboard)
		case "yt-dlp":
			pipx = true
			packages = append(packages, "pipx")
		case "ueberzugpp":
			plan.notes = append(plan.notes, "ueberzugpp is not in apt: see https://github.com/jstkdng/ueberzugpp")
		default: // mpv, ffmpeg and wmctrl
			packages = append(packages, name)
		}
	}

	if len(packages) > 0 {
		plan.command = "sudo apt install " + strings.Join(packages, " ")

		if pipx {
			plan.command += " && pipx install yt-dlp"
		}
	}

	return plan
}
