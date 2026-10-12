// Package history keeps the list of past searches in a small text file.
//
// Each line is "unix-seconds<TAB>query". Entries older than the maximum age
// and entries beyond the maximum count are dropped whenever the file is
// loaded, so it cannot grow without bound.
package history

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Entry is one past search.
type Entry struct {
	Query string
	When  time.Time
}

// Store is the list of past searches, oldest first.
type Store struct {
	path       string
	maxAge     time.Duration
	maxEntries int
	entries    []Entry
}

// Path returns the history file: $XDG_STATE_HOME/ytwatch/history, or
// ~/.local/state/ytwatch/history.
func Path() string {
	dir := os.Getenv("XDG_STATE_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(dir, "ytwatch", "history")
}

// Load reads the file at path and drops expired and surplus entries. A missing
// or unreadable file gives an empty history. The pruned list is written back
// when something was dropped.
func Load(path string, maxAge time.Duration, maxEntries int, now time.Time) *Store {
	s := &Store{path: path, maxAge: maxAge, maxEntries: max(maxEntries, 1)}
	if path == "" {
		return s
	}
	file, err := os.Open(path)
	if err != nil {
		return s
	}
	defer file.Close()
	total := 0
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		total++
		seconds, query, ok := strings.Cut(scanner.Text(), "\t")
		// Older versions stored the / that opens the search as part of the query.
		query = strings.TrimSpace(strings.TrimLeft(query, "/"))
		unix, err := strconv.ParseInt(seconds, 10, 64)
		if !ok || err != nil || strings.TrimSpace(query) == "" {
			continue
		}
		when := time.Unix(unix, 0)
		if now.Sub(when) > maxAge || when.After(now.Add(24*time.Hour)) {
			continue
		}
		s.entries = append(s.entries, Entry{Query: query, When: when})
	}
	s.entries = dedupe(s.entries)
	s.trim()
	// If reading stopped early, do not write back a shortened list: that would
	// delete the lines that were not read.
	if scanner.Err() == nil && len(s.entries) != total {
		s.save()
	}
	return s
}

// Add records query as the newest search. An earlier copy of the same query
// is removed, so every query appears once.
func (s *Store) Add(query string, now time.Time) {
	query = strings.Join(strings.Fields(strings.TrimLeft(query, "/")), " ")
	if query == "" || s.path == "" {
		return
	}
	s.entries = append(s.entries, Entry{Query: query, When: now})
	s.entries = dedupe(s.entries)
	s.trim()
	s.save()
}

// Queries returns the searches, oldest first.
func (s *Store) Queries() []string {
	out := make([]string, len(s.entries))
	for i, e := range s.entries {
		out[i] = e.Query
	}
	return out
}

// dedupe keeps the newest copy of every query.
func dedupe(entries []Entry) []Entry {
	seen := make(map[string]bool, len(entries))
	out := make([]Entry, 0, len(entries))
	for i := len(entries) - 1; i >= 0; i-- {
		if !seen[entries[i].Query] {
			seen[entries[i].Query] = true
			out = append(out, entries[i])
		}
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

func (s *Store) trim() {
	if extra := len(s.entries) - s.maxEntries; extra > 0 {
		s.entries = s.entries[extra:]
	}
}

// save writes the file by renaming a temporary one, so a crash never leaves a
// half-written history. Errors are ignored: history is a convenience.
func (s *Store) save() {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return
	}
	var b strings.Builder
	for _, e := range s.entries {
		b.WriteString(strconv.FormatInt(e.When.Unix(), 10) + "\t" + e.Query + "\n")
	}
	temp := s.path + ".tmp"
	if err := os.WriteFile(temp, []byte(b.String()), 0o600); err != nil {
		return
	}
	if err := os.Rename(temp, s.path); err != nil {
		_ = os.Remove(temp)
	}
}
