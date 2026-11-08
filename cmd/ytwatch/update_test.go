package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/geovannewashington/ytwatch/internal/update"
)

// fakeUpdate replaces what runUpdate uses and records what it did.
func fakeUpdate(t *testing.T, latest string, downloadErr error) *string {
	t.Helper()

	var replaced string
	oldL, oldD, oldE, oldR := updateLatest, updateDownload, updateExecutable, updateReplace
	t.Cleanup(func() { updateLatest, updateDownload, updateExecutable, updateReplace = oldL, oldD, oldE, oldR })

	updateLatest = func(context.Context) (update.Version, error) { return update.ParseVersion(latest) }
	updateDownload = func(context.Context, update.Version, string) ([]byte, error) {
		return []byte("program"), downloadErr
	}
	updateExecutable = func() (string, error) { return "/opt/ytwatch", nil }
	updateReplace = func(path string, program []byte) error {
		replaced = path + ":" + string(program)
		return nil
	}

	return &replaced
}

func TestUpdateReplacesAnOlderVersion(t *testing.T) {
	replaced := fakeUpdate(t, "0.2.0", nil)

	var out, errs bytes.Buffer
	if code := runUpdate(nil, "0.1.0", &out, &errs); code != 0 || *replaced != "/opt/ytwatch:program" {
		t.Errorf("code %d, replaced %q, out %q, err %q", code, *replaced, out.String(), errs.String())
	}
	if !strings.Contains(out.String(), "0.1.0 to 0.2.0") {
		t.Errorf("out = %q", out.String())
	}
}

func TestUpdateCheckOnlyLooks(t *testing.T) {
	replaced := fakeUpdate(t, "0.2.0", nil)

	var out, errs bytes.Buffer
	if code := runUpdate([]string{"--check"}, "0.1.0", &out, &errs); code != 0 || *replaced != "" || !strings.Contains(out.String(), "0.2.0 is available") {
		t.Errorf("code %d, replaced %q, out %q", code, *replaced, out.String())
	}
}

func TestUpdateDoesNothingWhenCurrentOrNewer(t *testing.T) {
	for _, current := range []string{"0.2.0", "0.3.0"} {
		replaced := fakeUpdate(t, "0.2.0", nil)

		var out, errs bytes.Buffer
		if code := runUpdate(nil, current, &out, &errs); code != 0 || *replaced != "" || !strings.Contains(out.String(), "up to date") {
			t.Errorf("%s: code %d, replaced %q, out %q", current, code, *replaced, out.String())
		}
	}
}

func TestUpdateRefusesABuildFromSourceAndABadDownload(t *testing.T) {
	replaced := fakeUpdate(t, "0.2.0", nil)

	var out, errs bytes.Buffer
	if code := runUpdate(nil, "dev", &out, &errs); code == 0 || *replaced != "" || !strings.Contains(out.String(), "make install") {
		t.Errorf("a build from source is not replaced: code %d, out %q", code, out.String())
	}

	replaced = fakeUpdate(t, "0.2.0", errors.New("checksum does not match"))
	out.Reset()
	errs.Reset()
	if code := runUpdate(nil, "0.1.0", &out, &errs); code == 0 || *replaced != "" || !strings.Contains(errs.String(), "does not match") {
		t.Errorf("a failed download changes nothing: code %d, replaced %q, err %q", code, *replaced, errs.String())
	}

	if code := runUpdate([]string{"--force"}, "0.1.0", &out, &errs); code != 2 {
		t.Errorf("an unknown option is a usage error, got %d", code)
	}
}
