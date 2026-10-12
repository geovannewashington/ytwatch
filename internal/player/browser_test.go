package player

import (
	"errors"
	"reflect"
	"testing"
)

func fakeLookPath(installed ...string) func(string) (string, error) {
	return func(name string) (string, error) {
		for _, n := range installed {
			if n == name {
				return "/usr/bin/" + name, nil
			}
		}
		return "", errors.New("not found")
	}
}

func TestResolveBrowserConfiguredWins(t *testing.T) {
	b, err := resolveBrowser("chromium --incognito", "firefox", "firefox.desktop", fakeLookPath("chromium", "firefox"))
	if err != nil {
		t.Fatal(err)
	}
	if b.path != "/usr/bin/chromium" || b.name != "chromium" || !b.newWindow ||
		!reflect.DeepEqual(b.args, []string{"--incognito"}) {
		t.Fatalf("got %+v", b)
	}
}

func TestResolveBrowserConfiguredMissingIsError(t *testing.T) {
	_, err := resolveBrowser("nope", "firefox", "", fakeLookPath("firefox"))
	if err == nil || err.Error() != "nope is not installed" {
		t.Fatalf("err = %v", err)
	}
}

func TestResolveBrowserEnvList(t *testing.T) {
	b, err := resolveBrowser("", "missing %s:firefox %s", "", fakeLookPath("firefox"))
	if err != nil {
		t.Fatal(err)
	}
	if b.name != "firefox" || !b.newWindow || len(b.args) != 0 {
		t.Fatalf("got %+v", b)
	}
}

func TestResolveBrowserDesktopAlias(t *testing.T) {
	b, err := resolveBrowser("", "", "google-chrome.desktop\n", fakeLookPath("google-chrome-stable"))
	if err != nil {
		t.Fatal(err)
	}
	if b.name != "google-chrome" || !b.newWindow || len(b.args) != 0 {
		t.Fatalf("got %+v", b)
	}
}

func TestResolveBrowserUnknownBrowserGetsNoFlag(t *testing.T) {
	b, err := resolveBrowser("", "", "qutebrowser.desktop", fakeLookPath("qutebrowser"))
	if err != nil {
		t.Fatal(err)
	}
	if b.name != "qutebrowser" || b.newWindow || len(b.args) != 0 {
		t.Fatalf("got %+v", b)
	}
}

func TestResolveBrowserFallsBackToXdgOpen(t *testing.T) {
	b, err := resolveBrowser("", "", "", fakeLookPath("xdg-open"))
	if err != nil {
		t.Fatal(err)
	}
	if b.path != "/usr/bin/xdg-open" || b.name != "" || len(b.args) != 0 {
		t.Fatalf("got %+v", b)
	}
}

func TestResolveBrowserNothingFound(t *testing.T) {
	if _, err := resolveBrowser("", "", "", fakeLookPath()); err == nil {
		t.Fatal("want error")
	}
}
