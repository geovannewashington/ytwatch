package history

import (
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

var now = time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)

func TestAddKeepsEachQueryOnceAndNewestLast(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "history")
	s := Load(path, 30*24*time.Hour, 100, now)
	s.Add("golang", now)
	s.Add("  rust   book ", now.Add(time.Minute))
	s.Add("golang", now.Add(2*time.Minute))
	want := []string{"rust book", "golang"}
	if got := s.Queries(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Queries() = %q, want %q", got, want)
	}
	// It is saved: a new Store reads the same list.
	again := Load(path, 30*24*time.Hour, 100, now.Add(time.Hour))
	if got := again.Queries(); !reflect.DeepEqual(got, want) {
		t.Fatalf("after reload %q, want %q", got, want)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("history file should be private: %v %v", info, err)
	}
}

func TestLoadDropsOldAndSurplusEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history")
	day := 24 * time.Hour
	line := func(age time.Duration, q string) string {
		return strconv.FormatInt(now.Add(-age).Unix(), 10) + "\t" + q + "\n"
	}
	data := line(40*day, "too old") + line(3*day, "one") + "garbage line\n" +
		line(2*day, "two") + line(day, "three")
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	s := Load(path, 30*day, 2, now)
	if got, want := s.Queries(), []string{"two", "three"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Queries() = %q, want %q", got, want)
	}
	// The pruned list was written back.
	saved, _ := os.ReadFile(path)
	if want := line(2*day, "two") + line(day, "three"); string(saved) != want {
		t.Fatalf("saved file:\n%s\nwant:\n%s", saved, want)
	}
}

func TestMissingFileAndEmptyPath(t *testing.T) {
	if got := Load(filepath.Join(t.TempDir(), "none"), time.Hour, 10, now).Queries(); len(got) != 0 {
		t.Fatalf("got %q", got)
	}
	s := Load("", time.Hour, 10, now)
	s.Add("x", now) // must not panic or write anywhere
	if len(s.Queries()) != 0 {
		t.Fatal("an empty path keeps nothing")
	}
}

func TestLoadDoesNotShortenTheFileWhenReadingStopsEarly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history")
	line := func(q string) string { return strconv.FormatInt(now.Add(-time.Hour).Unix(), 10) + "\t" + q + "\n" }
	huge := strings.Repeat("x", 2<<20) // longer than a line may be
	data := line("before") + line(huge) + line("after")
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	s := Load(path, 30*24*time.Hour, 100, now)
	if got := s.Queries(); len(got) != 1 || got[0] != "before" {
		t.Fatalf("Queries() = %q", got)
	}
	saved, _ := os.ReadFile(path)
	if string(saved) != data {
		t.Fatal("the file was rewritten, which would delete the lines that were not read")
	}
}

func TestLeadingSlashesOfOldEntriesAreRemoved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history")
	line := func(age time.Duration, q string) string {
		return strconv.FormatInt(now.Add(-age).Unix(), 10) + "\t" + q + "\n"
	}
	data := line(3*time.Hour, "/the primetime") + line(2*time.Hour, "go") + line(time.Hour, "the primetime")
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	s := Load(path, 30*24*time.Hour, 100, now)
	if got, want := s.Queries(), []string{"go", "the primetime"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Queries() = %q, want %q", got, want)
	}
	s.Add("/rust", now)
	if got := s.Queries(); got[len(got)-1] != "rust" {
		t.Fatalf("Add should drop the slash: %q", got)
	}
}
