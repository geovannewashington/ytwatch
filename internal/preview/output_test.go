package preview

import "testing"

func TestResolveOutput(t *testing.T) {
	env := func(vars map[string]string) func(string) string {
		return func(name string) string { return vars[name] }
	}
	kittyEnv := map[string]string{"KITTY_WINDOW_ID": "1", "DISPLAY": ":0"}
	wayland := map[string]string{"WAYLAND_DISPLAY": "wayland-1", "DISPLAY": ":0"}

	for name, tc := range map[string]struct {
		setting           string
		vars              map[string]string
		kitty, ueberzugpp bool
		mode              Mode
		ueberzugOutput    string
	}{
		"auto in kitty":                   {"auto", kittyEnv, true, true, ModeKitty, ""},
		"auto in ghostty":                 {"", map[string]string{"GHOSTTY_RESOURCES_DIR": "/x"}, true, false, ModeKitty, ""},
		"auto in ghostty by name":         {"auto", map[string]string{"TERM_PROGRAM": "ghostty"}, true, false, ModeKitty, ""},
		"auto with TERM xterm-kitty":      {"auto", map[string]string{"TERM": "xterm-kitty"}, true, false, ModeKitty, ""},
		"auto answered but no kitty hint": {"auto", map[string]string{"DISPLAY": ":0"}, true, true, ModeUeberzug, "x11"},
		"auto hint but no answer":         {"auto", kittyEnv, false, true, ModeUeberzug, "x11"},
		"auto ueberzugpp on wayland":      {"auto", wayland, false, true, ModeUeberzug, "wayland"},
		"empty means auto on x11":         {"", map[string]string{"DISPLAY": ":0"}, false, true, ModeUeberzug, "x11"},
		"auto ueberzugpp without display": {"auto", nil, false, true, ModeUeberzug, ""},
		"auto falls back to half-blocks":  {"auto", wayland, false, false, ModeHalfBlocks, ""},
		"off":                             {"off", kittyEnv, true, true, ModeOff, ""},
		"kitty skips the hint and query":  {"Kitty", nil, false, false, ModeKitty, ""},
		"halfblocks":                      {"halfblocks", wayland, true, true, ModeHalfBlocks, ""},
		"ueberzugpp follows the session":  {"ueberzugpp", wayland, true, false, ModeUeberzug, "wayland"},
		"x11 forced under xwayland":       {"x11", map[string]string{"WAYLAND_DISPLAY": "w"}, false, true, ModeUeberzug, "x11"},
		"wayland":                         {"wayland", nil, false, true, ModeUeberzug, "wayland"},
		"sixel":                           {"sixel", nil, false, true, ModeUeberzug, "sixel"},
		"iterm2":                          {"iterm2", nil, false, true, ModeUeberzug, "iterm2"},
		"chafa":                           {"chafa", nil, false, true, ModeUeberzug, "chafa"},
	} {
		got := ResolveOutput(tc.setting, env(tc.vars), tc.kitty, tc.ueberzugpp)
		if got.Mode != tc.mode || got.Ueberzug != tc.ueberzugOutput || got.Reason == "" {
			t.Errorf("%s: got %+v; want mode %d, output %q", name, got, tc.mode, tc.ueberzugOutput)
		}
	}
}

func TestWantsKittyQueryOnlyForAutoInAKittyTerminal(t *testing.T) {
	kitty := func(name string) string { return map[string]string{"KITTY_WINDOW_ID": "1"}[name] }
	none := func(string) string { return "" }

	if !WantsKittyQuery("auto", kitty) || !WantsKittyQuery("", kitty) {
		t.Error("auto in kitty must query")
	}
	if WantsKittyQuery("auto", none) || WantsKittyQuery("kitty", kitty) || WantsKittyQuery("off", kitty) {
		t.Error("no query without the hint or for an explicit setting")
	}
}
