package resume

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
	"unicode/utf8"
)

// mpv keeps only the address and the position of a video it left. For the
// history of what was played, and to bring half-watched videos back in a
// search, ytwatch also keeps what it knew of each video when it played it: a
// small file next to the folder of mpv. It holds the same kind of data as the
// folder, so it is private to the user.

// Entry is what ytwatch knew of a video when it was played.
type Entry struct {
	ID       string  `json:"id"`
	Title    string  `json:"title"`
	Channel  string  `json:"channel"`
	Duration float64 `json:"duration"`
	// Played is when the video was last played, in seconds since 1970. It is 0
	// for a video that was only learned about from its position.
	Played int64 `json:"played,omitempty"`
	// Short is set for a YouTube Short.
	Short bool `json:"short,omitempty"`
}

const (
	// maxMetaSize bounds what is read from the file.
	maxMetaSize = 1 << 20

	// maxTextLen bounds a title and a channel name, in characters.
	maxTextLen = 200
)

// MetaPath is the file of entries that goes with the watch later folder dir.
func MetaPath(dir string) string {
	if dir == "" {
		return ""
	}

	return filepath.Join(filepath.Dir(dir), "watching.json")
}

// LoadMeta reads the entries in path, by video id. A file that is missing or
// broken has none, and an entry with an id that is not a video id is skipped:
// the file is data from disk, and ids end up in addresses.
func LoadMeta(path string) map[string]Entry {
	entries := map[string]Entry{}
	if path == "" {
		return entries
	}

	file, err := os.Open(path)
	if err != nil {
		return entries
	}
	defer file.Close()

	var list []Entry
	if err := json.NewDecoder(io.LimitReader(file, maxMetaSize)).Decode(&list); err != nil {
		return entries
	}

	for i, entry := range list {
		if i >= maxFiles {
			break
		}
		if !videoID.MatchString(entry.ID) {
			continue
		}
		entry.Title, entry.Channel = clip(entry.Title), clip(entry.Channel)
		if entry.Duration < 0 || entry.Duration > 1e7 {
			entry.Duration = 0
		}
		if entry.Played < 0 {
			entry.Played = 0
		}
		entries[entry.ID] = entry
	}

	return entries
}

// clip cuts text to maxTextLen characters.
func clip(text string) string {
	if utf8.RuneCountInString(text) <= maxTextLen {
		return text
	}

	return string([]rune(text)[:maxTextLen])
}

// SaveMeta writes the entries to path, for the user only. It writes a new file
// and renames it, so a crash leaves the old one whole.
func SaveMeta(path string, entries map[string]Entry) error {
	if path == "" {
		return os.ErrInvalid
	}

	list := make([]Entry, 0, len(entries))
	for _, entry := range entries {
		list = append(list, entry)
	}
	// The newest stay if there are too many.
	sort.Slice(list, func(i, j int) bool {
		if list[i].Played != list[j].Played {
			return list[i].Played > list[j].Played
		}

		return list[i].ID < list[j].ID
	})
	if len(list) > maxFiles {
		list = list[:maxFiles]
	}

	data, err := json.Marshal(list)
	if err != nil {
		return err
	}

	temp, err := os.CreateTemp(filepath.Dir(path), "watching-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())

	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}

	return os.Rename(temp.Name(), path)
}

// Left is a video that has a position in the watch later folder.
type Left struct {
	ID       string
	Position time.Duration
	At       time.Time // when mpv wrote the position
}

// Recent lists the videos with a position in dir, the one left last first.
func Recent(dir string) []Left {
	if dir == "" {
		return nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var left []Left
	for i, entry := range entries {
		if i >= maxFiles {
			break
		}
		if !entry.Type().IsRegular() {
			continue
		}

		id, position, ok := readFile(filepath.Join(dir, entry.Name()))
		if !ok {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		left = append(left, Left{ID: id, Position: position, At: info.ModTime()})
	}

	sort.Slice(left, func(i, j int) bool { return left[i].At.After(left[j].At) })

	return left
}
