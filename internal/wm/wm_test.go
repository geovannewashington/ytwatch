package wm

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeBackend struct {
	calls   int
	appears int // call number from which "new" exists
	moved   []string
	err     error
}

func (f *fakeBackend) Windows(ctx context.Context) (map[string]Window, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	w := map[string]Window{"1": {ID: "1", Title: "old"}}
	if f.calls >= f.appears {
		w["2"] = Window{ID: "2", Title: "ytwatch | video"}
	}
	return w, nil
}

func (f *fakeBackend) Move(ctx context.Context, id, workspace string) error {
	f.moved = append(f.moved, id+"@"+workspace)
	return nil
}

func fast(t *testing.T) {
	t.Helper()
	oldPoll, oldTimeout := pollInterval, moveTimeout
	pollInterval, moveTimeout = time.Millisecond, 50*time.Millisecond
	t.Cleanup(func() { pollInterval, moveTimeout = oldPoll, oldTimeout })
}

func matchTitle(w Window) bool { return w.Title == "ytwatch | video" }

func TestMoveNewMovesWindowThatAppearsLater(t *testing.T) {
	fast(t)
	f := &fakeBackend{appears: 3}
	before := map[string]Window{"1": {ID: "1", Title: "old"}}
	MoveNew(f, before, matchTitle, "3")
	if len(f.moved) != 1 || f.moved[0] != "2@3" {
		t.Fatalf("moved = %v, want [2@3]", f.moved)
	}
}

func TestMoveNewSkipsExistingWindows(t *testing.T) {
	fast(t)
	f := &fakeBackend{appears: 1}
	before := map[string]Window{"2": {ID: "2", Title: "ytwatch | video"}}
	MoveNew(f, before, matchTitle, "3")
	if len(f.moved) != 0 {
		t.Fatalf("moved = %v, want none", f.moved)
	}
}

func TestMoveNewNoOps(t *testing.T) {
	fast(t)
	f := &fakeBackend{appears: 1}
	before := map[string]Window{}
	MoveNew(nil, before, matchTitle, "3")
	MoveNew(f, before, matchTitle, "")
	MoveNew(f, nil, matchTitle, "3")
	if f.calls != 0 {
		t.Fatalf("backend called %d times, want 0", f.calls)
	}
}

func TestMoveNewGivesUpOnTimeout(t *testing.T) {
	fast(t)
	f := &fakeBackend{err: errors.New("down")}
	MoveNew(f, map[string]Window{}, matchTitle, "3")
	if len(f.moved) != 0 {
		t.Fatalf("moved = %v, want none", f.moved)
	}
}

func TestSnapshotNilBackend(t *testing.T) {
	if Snapshot(nil) != nil {
		t.Fatal("want nil snapshot")
	}
}

func TestParseTree(t *testing.T) {
	data := []byte(`{"id":1,"nodes":[{"id":2,"nodes":[
		{"id":10,"name":"a","app_id":"mpv","nodes":[],"floating_nodes":[]},
		{"id":11,"name":"b","app_id":"","window_properties":{"class":"Firefox"},"nodes":[],"floating_nodes":[]},
		{"id":12,"name":"container","nodes":[],"floating_nodes":[]}
	]}],"floating_nodes":[]}`)
	got, err := parseTree(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got["10"].App != "mpv" || got["11"].App != "Firefox" {
		t.Fatalf("got = %+v", got)
	}
}

func TestParseClients(t *testing.T) {
	data := []byte(`[
		{"address":"0xaa","title":"ytwatch | x","class":"mpv","mapped":true},
		{"address":"0xbb","title":"hidden","class":"foo","mapped":false}
	]`)
	got, err := parseClients(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got["0xaa"].Title != "ytwatch | x" {
		t.Fatalf("got = %+v", got)
	}
}

func TestParseNiriWindows(t *testing.T) {
	data := []byte(`[
		{"id":12,"title":"ytwatch | x","app_id":"mpv","workspace_id":1},
		{"id":7,"title":"web","app_id":null}
	]`)
	got, err := parseNiriWindows(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got["12"].Title != "ytwatch | x" || got["12"].App != "mpv" {
		t.Fatalf("got = %+v", got)
	}
}

func TestParseWmctrl(t *testing.T) {
	out := "0x03a00003  0 mpv.mpv            host ytwatch |  two  spaces\n" +
		"0x03c00004 -1 Navigator.firefox  host Mozilla Firefox\n" +
		"\n" +
		"broken line\n"
	got := parseWmctrl(out)
	if len(got) != 2 {
		t.Fatalf("got = %+v", got)
	}
	if w := got["0x03a00003"]; w.App != "mpv.mpv" || w.Title != "ytwatch |  two  spaces" {
		t.Fatalf("mpv window = %+v", w)
	}
	if w := got["0x03c00004"]; w.App != "Navigator.firefox" || w.Title != "Mozilla Firefox" {
		t.Fatalf("firefox window = %+v", w)
	}
}

func TestDetectUnsupportedSession(t *testing.T) {
	for _, name := range []string{"SWAYSOCK", "HYPRLAND_INSTANCE_SIGNATURE", "NIRI_SOCKET", "I3SOCK", "DISPLAY", "WAYLAND_DISPLAY"} {
		t.Setenv(name, "")
	}
	if Detect() != nil {
		t.Fatal("want nil backend")
	}
}

func TestDetectSkipsX11OnWayland(t *testing.T) {
	for _, name := range []string{"SWAYSOCK", "HYPRLAND_INSTANCE_SIGNATURE", "NIRI_SOCKET", "I3SOCK"} {
		t.Setenv(name, "")
	}
	t.Setenv("DISPLAY", ":0")
	t.Setenv("WAYLAND_DISPLAY", "wayland-1")
	if Detect() != nil {
		t.Fatal("want nil backend on Wayland without a native backend")
	}
}

func TestName(t *testing.T) {
	for want, b := range map[string]Backend{
		"sway":                   swayLike{bin: "swaymsg"},
		"i3":                     swayLike{bin: "i3-msg"},
		"Hyprland":               hyprland{},
		"niri":                   niri{},
		"bspwm":                  bspwm{},
		"X11 (EWMH, via wmctrl)": ewmh{},
		"":                       nil,
	} {
		if got := Name(b); got != want {
			t.Errorf("Name(%T) = %q, want %q", b, got, want)
		}
	}
}

func TestHyprlandMoveFallsBackToLua(t *testing.T) {
	var calls [][]string
	old := hyprctl
	t.Cleanup(func() { hyprctl = old })
	hyprctl = func(ctx context.Context, args ...string) error {
		calls = append(calls, args)
		if args[0] == "dispatch" {
			// Hyprland with the Lua config rejects the legacy dispatcher.
			return errors.New("exit status 7")
		}
		return nil
	}
	if err := (hyprland{}).Move(context.Background(), "0x5992", `name:a"b`); err != nil {
		t.Fatalf("Move: %v", err)
	}
	want := []string{
		"dispatch movetoworkspacesilent name:a\"b,address:0x5992",
		`eval hl.dispatch(hl.dsp.window.move({ workspace = "name:a\"b", follow = false, window = "address:0x5992" }))`,
	}
	if len(calls) != len(want) {
		t.Fatalf("calls = %q, want %q", calls, want)
	}
	for i, args := range calls {
		if got := strings.Join(args, " "); got != want[i] {
			t.Errorf("call %d = %s\nwant      %s", i, got, want[i])
		}
	}
}

func TestHyprlandMoveLegacyOnly(t *testing.T) {
	calls := 0
	old := hyprctl
	t.Cleanup(func() { hyprctl = old })
	hyprctl = func(ctx context.Context, args ...string) error { calls++; return nil }
	if err := (hyprland{}).Move(context.Background(), "0x1", "3"); err != nil || calls != 1 {
		t.Fatalf("err = %v, calls = %d; want nil, 1", err, calls)
	}
}

func TestHyprlandMoveReportsBothErrors(t *testing.T) {
	legacy, lua := errors.New("legacy failed"), errors.New("eval failed")
	old := hyprctl
	t.Cleanup(func() { hyprctl = old })
	hyprctl = func(ctx context.Context, args ...string) error {
		if args[0] == "dispatch" {
			return legacy
		}
		return lua
	}
	err := (hyprland{}).Move(context.Background(), "0x1", "3")
	if !errors.Is(err, legacy) || !errors.Is(err, lua) {
		t.Fatalf("err = %v, want both the legacy and the eval error", err)
	}
}
