package history

import (
	"os"
	"path/filepath"
	"reflect"
	"strconv"
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
