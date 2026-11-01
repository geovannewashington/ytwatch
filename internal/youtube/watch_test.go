package youtube

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/geovannewashington/ytwatch/internal/locale"
	"github.com/geovannewashington/ytwatch/internal/media"
)

// watchPage builds a page like YouTube's: the two JSON values are assigned to
// variables inside scripts, with other code around them.
func watchPage(t *testing.T, player, data map[string]any) []byte {
	t.Helper()

	playerJSON, err := json.Marshal(player)
	if err != nil {
		t.Fatal(err)
	}

	dataJSON, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}

	return []byte("<html><script>var ytInitialPlayerResponse = " + string(playerJSON) +
		";var meta = {};</script><script>var ytInitialData = " + string(dataJSON) +
		";</script></html>")
}

// viewersData is the part of ytInitialData that holds the viewer count.
func viewersData(live bool, count string) map[string]any {
	return map[string]any{"contents": map[string]any{"twoColumnWatchNextResults": map[string]any{
		"results": map[string]any{"results": map[string]any{"contents": []any{
			map[string]any{"videoPrimaryInfoRenderer": map[string]any{"viewCount": map[string]any{
				"videoViewCountRenderer": map[string]any{"isLive": live, "originalViewCount": count},
			}}},
		}}},
	}}}
}

func recordingPlayer(id string) map[string]any {
	return map[string]any{
		"playabilityStatus": map[string]any{"status": "OK"},
		"videoDetails": map[string]any{
			"videoId": id, "title": "A recording", "lengthSeconds": "653",
			"channelId": "UCchannel1", "author": "Some Channel", "viewCount": "1148269",
			"shortDescription": "The description\nof the video.",
		},
		"microformat": map[string]any{"playerMicroformatRenderer": map[string]any{
			"publishDate": "2025-12-04T10:00:30-08:00",
		}},
	}
}

func TestParseWatchPageReadsARecording(t *testing.T) {
	page := watchPage(t, recordingPlayer("abcdefghijk"), viewersData(false, "1148296"))

	video, err := parseWatchPage(page, "abcdefghijk")
	if err != nil {
		t.Fatal(err)
	}

	if video.Title != "A recording" || video.Channel != "Some Channel" || video.ChannelID != "UCchannel1" {
		t.Errorf("title or channel wrong: %+v", video)
	}
	if video.Duration != 653 || video.ViewCount != 1148269 {
		t.Errorf("length %v or views %d wrong", video.Duration, video.ViewCount)
	}
	if video.Description != "The description\nof the video." {
		t.Errorf("description = %q", video.Description)
	}
	if video.UploadDate != "20251204" || video.Timestamp == 0 {
		t.Errorf("date: upload %q timestamp %v", video.UploadDate, video.Timestamp)
	}
	if video.LiveStatus != "not_live" || video.IsLive || video.ConcurrentViewers != 0 {
		t.Errorf("a recording is not live: %+v", video)
	}
}

func TestParseWatchPageReadsALiveStreamWithItsViewers(t *testing.T) {
	player := recordingPlayer("abcdefghijk")
	details := player["videoDetails"].(map[string]any)
	details["isLive"], details["isLiveContent"], details["lengthSeconds"] = true, true, "0"
	player["microformat"] = map[string]any{"playerMicroformatRenderer": map[string]any{
		"publishDate":          "2026-08-19T00:31:51-07:00",
		"liveBroadcastDetails": map[string]any{"isLiveNow": true, "startTimestamp": "2026-08-19T07:43:43+00:00"},
	}}

	video, err := parseWatchPage(watchPage(t, player, viewersData(true, "10585")), "abcdefghijk")
	if err != nil {
		t.Fatal(err)
	}

	if !video.IsCurrentlyLive() || video.LiveStatus != "is_live" {
		t.Errorf("should be live: %+v", video)
	}
	if video.ConcurrentViewers != 10585 {
		t.Errorf("viewers = %d, want 10585", video.ConcurrentViewers)
	}
	if video.ReleaseTimestamp == 0 || video.Duration != 0 {
		t.Errorf("start %v or length %v wrong", video.ReleaseTimestamp, video.Duration)
	}
}

func TestParseWatchPageDoesNotCountPeopleWaitingForAnUpcomingStream(t *testing.T) {
	player := recordingPlayer("abcdefghijk")
	details := player["videoDetails"].(map[string]any)
	details["isUpcoming"], details["isLiveContent"], details["lengthSeconds"] = true, true, "0"

	// An upcoming stream's counter says "23 waiting", and has isLive set.
	video, err := parseWatchPage(watchPage(t, player, viewersData(true, "23")), "abcdefghijk")
	if err != nil {
		t.Fatal(err)
	}

	if !video.IsUpcoming() || video.WasLive {
		t.Errorf("should be upcoming and not a past stream: %+v", video)
	}
	if video.ConcurrentViewers != 0 {
		t.Errorf("waiting people are not viewers, got %d", video.ConcurrentViewers)
	}
}

func TestParseWatchPageKnowsAPastStreamAndAShort(t *testing.T) {
	past := recordingPlayer("abcdefghijk")
	past["videoDetails"].(map[string]any)["isLiveContent"] = true
	video, err := parseWatchPage(watchPage(t, past, nil), "abcdefghijk")
	if err != nil {
		t.Fatal(err)
	}
	if !video.IsPastLive() {
		t.Errorf("a recorded stream should be a past live: %+v", video)
	}

	short := recordingPlayer("abcdefghijk")
	short["videoDetails"].(map[string]any)["lengthSeconds"] = "13"
	short["streamingData"] = map[string]any{"adaptiveFormats": []any{
		map[string]any{"mimeType": "audio/mp4"},
		map[string]any{"width": 720, "height": 1280},
	}}
	video, err = parseWatchPage(watchPage(t, short, nil), "abcdefghijk")
	if err != nil {
		t.Fatal(err)
	}
	if !video.IsShort() {
		t.Errorf("a tall 13 second video should be a Short: %+v", video)
	}
}

func TestParseWatchPageRejectsPagesThatAreNotTheVideo(t *testing.T) {
	for name, page := range map[string][]byte{
		"consent page":    []byte("<html>Before you continue to YouTube</html>"),
		"wrong video":     watchPage(t, recordingPlayer("zzzzzzzzzzz"), nil),
		"no video detail": watchPage(t, map[string]any{"playabilityStatus": map[string]any{"status": "LOGIN_REQUIRED"}}, nil),
		"broken json":     []byte("var ytInitialPlayerResponse = {\"videoDetails\": "),
	} {
		if _, err := parseWatchPage(page, "abcdefghijk"); err == nil {
			t.Errorf("%s should be an error", name)
		}
	}
}

// fakePages makes fetchResultsPage return a page for each video ID in pages,
// and an error for the others. It restores the real one when the test ends.
func fakePages(t *testing.T, pages map[string][]byte) {
	t.Helper()

	old := fetchResultsPage
	t.Cleanup(func() { fetchResultsPage = old })

	fetchResultsPage = func(_ context.Context, pageURL string, _ locale.Locale) ([]byte, error) {
		for id, page := range pages {
			if strings.Contains(pageURL, "v="+id) {
				return page, nil
			}
		}

		return nil, errors.New("offline")
	}
}

// fakeYtDlp installs a yt-dlp that logs its arguments, one run per line, and
// prints one video per ID it is given in the URLs.
func fakeYtDlp(t *testing.T) (log string) {
	t.Helper()

	binDir := t.TempDir()
	log = filepath.Join(binDir, "runs.log")
	fake := "#!/bin/sh\necho \"$*\" >> " + log + "\n" +
		`for arg in "$@"; do case "$arg" in *v=*) id="${arg##*v=}"; ` +
		`printf '{"id":"%s","title":"From yt-dlp","duration":42}\n' "$id" ;; esac; done` + "\n"
	if err := os.WriteFile(filepath.Join(binDir, "yt-dlp"), []byte(fake), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	return log
}

func TestFetchDetailsBatchReadsWatchPagesWithoutRunningYtDlp(t *testing.T) {
	log := fakeYtDlp(t)
	fakePages(t, map[string][]byte{
		"aaaaaaaaaaa": watchPage(t, recordingPlayer("aaaaaaaaaaa"), nil),
		"bbbbbbbbbbb": watchPage(t, recordingPlayer("bbbbbbbbbbb"), nil),
	})

	var mu sync.Mutex
	got := map[string]media.Video{}
	err := FetchDetailsBatch(
		context.Background(),
		[]media.Video{{ID: "aaaaaaaaaaa"}, {ID: "bbbbbbbbbbb"}},
		locale.Locale{},
		func(v media.Video) {
			mu.Lock()
			got[v.ID] = v
			mu.Unlock()
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	for _, id := range []string{"aaaaaaaaaaa", "bbbbbbbbbbb"} {
		if v := got[id]; !v.Detailed || v.Duration != 653 || v.Description == "" {
			t.Errorf("%s: details missing: %+v", id, v)
		}
	}
	if data, _ := os.ReadFile(log); len(data) != 0 {
		t.Errorf("yt-dlp must not run when the pages can be read, it ran:\n%s", data)
	}
}

func TestFetchDetailsBatchAsksYtDlpOnlyAboutPagesItCouldNotRead(t *testing.T) {
	log := fakeYtDlp(t)
	fakePages(t, map[string][]byte{
		"aaaaaaaaaaa": watchPage(t, recordingPlayer("aaaaaaaaaaa"), nil),
		// bbbbbbbbbbb has no page: the fetch fails.
	})

	var mu sync.Mutex
	got := map[string]media.Video{}
	err := FetchDetailsBatch(
		context.Background(),
		[]media.Video{{ID: "aaaaaaaaaaa"}, {ID: "bbbbbbbbbbb"}, {ID: "not a video id"}},
		locale.Locale{},
		func(v media.Video) {
			mu.Lock()
			got[v.ID] = v
			mu.Unlock()
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if got["aaaaaaaaaaa"].Description == "" {
		t.Error("the readable page should give its details")
	}
	if got["bbbbbbbbbbb"].Title != "From yt-dlp" {
		t.Errorf("the unreadable page should come from yt-dlp: %+v", got["bbbbbbbbbbb"])
	}

	data, _ := os.ReadFile(log)
	runs := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(runs) != 1 {
		t.Fatalf("yt-dlp should run once, ran %d times:\n%s", len(runs), data)
	}
	if strings.Contains(runs[0], "v=aaaaaaaaaaa") {
		t.Errorf("yt-dlp must not be asked about the video whose page was read: %s", runs[0])
	}
}

func TestFetchDetailsBatchStopsWhenTheContextEnds(t *testing.T) {
	log := fakeYtDlp(t)
	fakePages(t, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	called := false
	_ = FetchDetailsBatch(ctx, []media.Video{{ID: "aaaaaaaaaaa"}}, locale.Locale{}, func(media.Video) { called = true })

	if called {
		t.Error("nothing should be reported after the context ended")
	}
	if data, _ := os.ReadFile(log); len(data) != 0 && strings.Contains(string(data), "aaaaaaaaaaa") {
		t.Errorf("yt-dlp must not be started for videos nobody wants any more:\n%s", data)
	}
}
