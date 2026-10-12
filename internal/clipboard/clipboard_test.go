package clipboard

import (
	"errors"
	"reflect"
	"testing"
)

func env(vars map[string]string) func(string) string {
	return func(name string) string { return vars[name] }
}

func installed(names ...string) func(string) (string, error) {
	return func(name string) (string, error) {
		for _, n := range names {
			if n == name {
				return "/usr/bin/" + name, nil
			}
		}
		return "", errors.New("not found")
	}
}

func TestPickFollowsTheSession(t *testing.T) {
	wl := env(map[string]string{"WAYLAND_DISPLAY": "wayland-1", "DISPLAY": ":0"})
	x := env(map[string]string{"DISPLAY": ":0"})
	for name, tc := range map[string]struct {
		getenv func(string) string
		have   []string
		want   []string
	}{
		"wayland prefers wl-copy":       {wl, []string{"wl-copy", "xclip"}, []string{"wl-copy"}},
		"wayland falls back to xclip":   {wl, []string{"xclip"}, []string{"xclip", "-selection", "clipboard"}},
		"x11 prefers xclip":             {x, []string{"wl-copy", "xclip", "xsel"}, []string{"xclip", "-selection", "clipboard"}},
		"x11 falls back to xsel":        {x, []string{"xsel"}, []string{"xsel", "--clipboard", "--input"}},
		"x11 uses wl-copy as last case": {x, []string{"wl-copy"}, []string{"wl-copy"}},
	} {
		got, err := pick(tc.getenv, installed(tc.have...))
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: got %v, %v; want %v", name, got, err, tc.want)
		}
	}
}

func TestPickWithoutAnyTool(t *testing.T) {
	if _, err := pick(env(nil), installed()); !errors.Is(err, ErrNoTool) {
		t.Fatalf("err = %v, want ErrNoTool", err)
	}
}
