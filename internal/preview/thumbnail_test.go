package preview

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestValidFileID(t *testing.T) {
	cases := map[string]bool{
		"jNQXAC9IVRw":                        true,
		"PLpCqPSEm2Xe8sEY2haMDUVgwbkIs5NCJI": true, // playlist id, 34 characters
		"short":                              false,
		"../../etc/passwd-is-not-valid":      false,
		"":                                   false,
	}
	for id, want := range cases {
		if got := validFileID(id); got != want {
			t.Errorf("validFileID(%q) = %v, want %v", id, got, want)
		}
	}
}

func TestFetchImageSkipsMissingAndWebP(t *testing.T) {
	jpeg := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 0x10, 'J', 'F', 'I', 'F', 0, 1, 1, 0, 0, 1, 0, 1, 0, 0}
	webp := append([]byte("RIFF\x10\x00\x00\x00WEBPVP8 "), make([]byte, 16)...)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/jpeg":
			_, _ = w.Write(jpeg)
		case "/webp":
			_, _ = w.Write(webp)
		case "/missing":
			http.NotFound(w, r)
		default:
			http.Error(w, "boom", http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	if data, err := fetchImage(context.Background(), server.URL+"/jpeg"); err != nil || len(data) != len(jpeg) {
		t.Fatalf("jpeg: %v, %d bytes", err, len(data))
	}
	for _, path := range []string{"/webp", "/missing"} {
		if _, err := fetchImage(context.Background(), server.URL+path); !errors.Is(err, errUnusable) {
			t.Errorf("%s: err = %v, want errUnusable", path, err)
		}
	}
	if _, err := fetchImage(context.Background(), server.URL+"/error"); err == nil || errors.Is(err, errUnusable) {
		t.Errorf("a server error must fail, not fall through: %v", err)
	}
}

func TestResolveOutput(t *testing.T) {
	env := func(vars map[string]string) func(string) string {
		return func(name string) string { return vars[name] }
	}
	for name, tc := range map[string]struct {
		setting string
		vars    map[string]string
		output  string
		enabled bool
	}{
		"auto on wayland":           {"auto", map[string]string{"WAYLAND_DISPLAY": "wayland-1", "DISPLAY": ":0"}, "wayland", true},
		"empty means auto on x11":   {"", map[string]string{"DISPLAY": ":0"}, "x11", true},
		"auto without display":      {"auto", nil, "", true},
		"off":                       {"off", map[string]string{"DISPLAY": ":0"}, "", false},
		"explicit value wins":       {"Kitty", map[string]string{"DISPLAY": ":0"}, "kitty", true},
		"x11 forced under xwayland": {"x11", map[string]string{"WAYLAND_DISPLAY": "wayland-1"}, "x11", true},
	} {
		output, enabled := ResolveOutput(tc.setting, env(tc.vars))
		if output != tc.output || enabled != tc.enabled {
			t.Errorf("%s: got %q, %v; want %q, %v", name, output, enabled, tc.output, tc.enabled)
		}
	}
}
