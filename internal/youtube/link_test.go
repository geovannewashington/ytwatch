package youtube

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/geovannewashington/ytwatch/internal/locale"
)

func TestParseLinkReadsVideoAndPlaylistLinks(t *testing.T) {
	for input, want := range map[string]Link{
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ":                  {VideoID: "dQw4w9WgXcQ"},
		"https://youtube.com/watch?v=dQw4w9WgXcQ&t=42s":                {VideoID: "dQw4w9WgXcQ"},
		"http://m.youtube.com/watch?v=dQw4w9WgXcQ":                     {VideoID: "dQw4w9WgXcQ"},
		"https://music.youtube.com/watch?v=dQw4w9WgXcQ":                {VideoID: "dQw4w9WgXcQ"},
		"youtube.com/watch?v=dQw4w9WgXcQ":                              {VideoID: "dQw4w9WgXcQ"},
		"https://youtu.be/dQw4w9WgXcQ?si=abc":                          {VideoID: "dQw4w9WgXcQ"},
		"https://www.youtube.com/shorts/dQw4w9WgXcQ":                   {VideoID: "dQw4w9WgXcQ"},
		"https://www.youtube.com/live/dQw4w9WgXcQ?feature=share":       {VideoID: "dQw4w9WgXcQ"},
		"https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ":           {VideoID: "dQw4w9WgXcQ"},
		"  https://WWW.YOUTUBE.COM/watch?v=dQw4w9WgXcQ  ":              {VideoID: "dQw4w9WgXcQ"},
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ&list=PLabc123":    {VideoID: "dQw4w9WgXcQ"},
		"https://www.youtube.com/playlist?list=PL4cUxeGkcC9gC":         {PlaylistID: "PL4cUxeGkcC9gC"},
		"https://music.youtube.com/playlist?list=OLAK5uy_abcdefghijkl": {PlaylistID: "OLAK5uy_abcdefghijkl"},
	} {
		got, ok := ParseLink(input)
		if !ok || got != want {
			t.Errorf("ParseLink(%q) = %+v, %v; want %+v", input, got, ok, want)
		}
	}
}

func TestParseLinkLeavesEverythingElseToTheSearch(t *testing.T) {
	for _, input := range []string{
		"",
		"programming",
		"dQw4w9WgXcQ",
		"go tutorial",
		"youtube.com",
		"https://www.youtube.com/",
		"https://www.youtube.com/watch?v=short",
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ--exec",
		"https://www.youtube.com/watch?v=--exec=rm",
		"https://www.youtube.com/playlist?list=PL%20x",
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ and more",
		"https://example.com/watch?v=dQw4w9WgXcQ",
		"https://youtube.com.evil.example/watch?v=dQw4w9WgXcQ",
		"ftp://youtube.com/watch?v=dQw4w9WgXcQ",
		"https://youtu.be/",
		"@GoogleDevelopers",
		"https://www.youtube.com/@",
		"https://www.youtube.com/@ab",
		"https://www.youtube.com/@bad%20name",
		"https://www.youtube.com/@a/b/c",
		"https://www.youtube.com/@GoogleDevelopers/secret",
		"https://www.youtube.com/@a$(rm)",
		"https://www.youtube.com/channel/UCshort",
		"https://www.youtube.com/channel/UC_x5XG1OV2P6uZZ5FSM9Ttw/extra/videos",
		"https://www.youtube.com/channel/",
		"https://www.youtube.com/c/",
		"https://www.youtube.com/user/bad%2Fname",
		"https://example.com/@GoogleDevelopers",
	} {
		if got, ok := ParseLink(input); ok {
			t.Errorf("ParseLink(%q) = %+v, want no link", input, got)
		}
	}
}

func TestParseLinkReadsChannelLinks(t *testing.T) {
	for input, want := range map[string]string{
		"https://www.youtube.com/@GoogleDevelopers":                          "https://www.youtube.com/@GoogleDevelopers",
		"youtube.com/@GoogleDevelopers/videos":                               "https://www.youtube.com/@GoogleDevelopers",
		"https://m.youtube.com/@tiago.temporin/shorts":                       "https://www.youtube.com/@tiago.temporin",
		"https://www.youtube.com/@a-b_c/streams":                             "https://www.youtube.com/@a-b_c",
		"https://www.youtube.com/@GoogleDevelopers/live":                     "https://www.youtube.com/@GoogleDevelopers",
		"https://www.youtube.com/@GoogleDevelopers/":                         "https://www.youtube.com/@GoogleDevelopers",
		"https://www.youtube.com/channel/UC_x5XG1OV2P6uZZ5FSM9Ttw":           "https://www.youtube.com/channel/UC_x5XG1OV2P6uZZ5FSM9Ttw",
		"https://www.youtube.com/channel/UC_x5XG1OV2P6uZZ5FSM9Ttw/playlists": "https://www.youtube.com/channel/UC_x5XG1OV2P6uZZ5FSM9Ttw",
		"https://www.youtube.com/c/GoogleDevelopers":                         "https://www.youtube.com/c/GoogleDevelopers",
		"https://www.youtube.com/user/google/videos":                         "https://www.youtube.com/user/google",
		"https://www.youtube.com/@кино":                                      "https://www.youtube.com/@%D0%BA%D0%B8%D0%BD%D0%BE",
	} {
		got, ok := ParseLink(input)
		if !ok || got.Channel == "" || got.VideoID != "" || got.PlaylistID != "" || got.ChannelURL() != want {
			t.Errorf("ParseLink(%q) = %+v, %v; want the channel %s", input, got, ok, want)
		}
	}
}

func TestLookupShowsTheVideoFromItsWatchPage(t *testing.T) {
	log := fakeYtDlp(t)
	fakePages(t, map[string][]byte{"aaaaaaaaaaa": watchPage(t, recordingPlayer("aaaaaaaaaaa"), nil)})

	events := lookupEvents(t, "aaaaaaaaaaa")
	if len(events) != 3 || events[0].Kind != EventResult || events[0].Video.ID != "aaaaaaaaaaa" ||
		!events[0].Video.Detailed {
		t.Fatalf("events = %+v", events)
	}
	if done := events[1]; done.Kind != EventSearchDone || done.Count != 1 || done.Err != nil {
		t.Fatalf("done = %+v", done)
	}
	if data, _ := os.ReadFile(log); len(data) != 0 {
		t.Errorf("yt-dlp must not run when the page can be read, it ran:\n%s", data)
	}
}

func TestLookupAsksYtDlpWhenThePageCannotBeRead(t *testing.T) {
	fakeYtDlp(t)
	fakePages(t, nil)

	events := lookupEvents(t, "bbbbbbbbbbb")
	if len(events) != 3 || events[0].Video.ID != "bbbbbbbbbbb" || events[0].Video.Title != "From yt-dlp" {
		t.Fatalf("events = %+v", events)
	}
}

func TestLookupReportsAVideoThatDoesNotExist(t *testing.T) {
	binDir := t.TempDir()
	fake := "#!/bin/sh\necho 'ERROR: [youtube] ccccccccccc: Video unavailable' >&2\nexit 1\n"
	if err := os.WriteFile(filepath.Join(binDir, "yt-dlp"), []byte(fake), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	fakePages(t, nil)

	events := lookupEvents(t, "ccccccccccc")
	if len(events) != 2 || events[0].Kind != EventSearchDone || events[0].Count != 0 || events[0].Err == nil {
		t.Fatalf("events = %+v", events)
	}
}

func lookupEvents(t *testing.T, id string) []Event {
	t.Helper()
	var events []Event
	Lookup(context.Background(), id, locale.Locale{}, func(e Event) { events = append(events, e) })
	return events
}
