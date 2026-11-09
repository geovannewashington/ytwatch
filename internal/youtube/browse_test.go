package youtube

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/geovannewashington/ytwatch/internal/locale"
	"github.com/geovannewashington/ytwatch/internal/media"
)

func TestBrowseListsPlaylistVideosInOrderAndSkipsRemovedOnes(t *testing.T) {
	binDir := t.TempDir()
	fake := `#!/bin/sh
args="$*"
case "$args" in
  *--dump-single-json*) exit 1 ;;
  *playlist?list=PLabc*)
    printf '%s\n' '{"id":"first000001","title":"First","duration":60}'
    printf '%s\n' '{"id":"gone0000001","title":"[Private video]"}'
    printf '%s\n' '{"id":"second00001","title":"Second","duration":90}'
    printf '%s\n' '{"id":"third000001","title":"[Deleted video]"}'
    printf '%s\n' '{"id":"fourth00001","title":"Fourth","duration":30}'
    ;;
esac
`
	if err := os.WriteFile(filepath.Join(binDir, "yt-dlp"), []byte(fake), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var mu sync.Mutex
	type row struct {
		id   string
		rank int
	}
	var rows []row
	var done []Event
	Browse(ctx, "https://www.youtube.com/playlist?list=PLabc", locale.Locale{}, func(e Event) {
		mu.Lock()
		defer mu.Unlock()
		switch e.Kind {
		case EventResult:
			rows = append(rows, row{e.Video.ID, e.Rank})
		case EventSearchDone, EventDetailsEnd:
			done = append(done, e)
		}
	})
	var ids []string
	for i, r := range rows {
		ids = append(ids, r.id)
		if i > 0 && r.rank <= rows[i-1].rank {
			t.Errorf("ranks must follow the playlist order: %v", rows)
		}
	}
	if got := strings.Join(ids, ","); got != "first000001,second00001,fourth00001" {
		t.Fatalf("videos = %s", got)
	}
	if len(done) != 2 ||
		done[0].Kind != EventSearchDone ||
		done[0].Err != nil ||
		done[0].Count != 3 ||
		done[1].Kind != EventDetailsEnd {
		t.Fatalf("end events = %+v", done)
	}
}

func TestBrowseReportsAnEmptyPlaylist(t *testing.T) {
	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "yt-dlp"), []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	var got Event
	Browse(context.Background(), "https://www.youtube.com/playlist?list=PLabc", locale.Locale{}, func(e Event) {
		if e.Kind == EventSearchDone {
			got = e
		}
	})
	if got.Err == nil || !strings.Contains(got.Err.Error(), "no videos") {
		t.Fatalf("event = %+v", got)
	}
}

func fakeChannelYtDlp(t *testing.T) (log string) {
	t.Helper()
	binDir := t.TempDir()
	log = filepath.Join(binDir, "log")
	fake := `#!/bin/sh
echo "$*" >> ` + log + `
case "$*" in
  *@x/shorts*) echo "ERROR: This channel does not have a shorts tab" >&2; exit 1 ;;
  *@x/streams*) ;;
  *@x/playlists*)
    printf '%s\n' '{"id":"PLabc123456","title":"A list","channel":"Playlist","url":"https://www.youtube.com/playlist?list=PLabc123456","playlist_uploader":"The X"}'
    printf '%s\n' '{"id":"UC0000000000000000000000","title":"Not a playlist","url":"https://www.youtube.com/channel/UC0000000000000000000000"}'
    ;;
  *@x/videos*)
    printf '%s\n' '{"id":"first000001","title":"First","url":"https://www.youtube.com/watch?v=first000001","playlist_uploader":"The X","playlist_channel_id":"UCthex"}'
    printf '%s\n' '{"id":"second00001","title":"Second","url":"https://www.youtube.com/watch?v=second00001","channel_url":"https://www.youtube.com/channel/UCreal"}'
    ;;
esac
`
	if err := os.WriteFile(filepath.Join(binDir, "yt-dlp"), []byte(fake), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return log
}

func browseChannel(tab int, name string) (rows []Event, done Event) {
	BrowseChannel(context.Background(), "https://www.youtube.com/@x/", tab, name, locale.Locale{}, func(e Event) {
		switch e.Kind {
		case EventResult:
			rows = append(rows, e)
		case EventSearchDone:
			done = e
		}
	})
	return rows, done
}

func TestBrowseChannelListsATabAndNamesTheChannel(t *testing.T) {
	log := fakeChannelYtDlp(t)

	rows, done := browseChannel(ChannelVideos, "Fallback")
	if len(rows) != 2 || done.Err != nil || done.Count != 2 {
		t.Fatalf("rows = %d, done = %+v", len(rows), done)
	}
	if rows[0].Video.Channel != "The X" || rows[1].Video.Channel != "The X" {
		t.Errorf("the channel name should come from the page: %q, %q", rows[0].Video.Channel, rows[1].Video.Channel)
	}
	if rows[0].Video.ChannelURL != "https://www.youtube.com/@x/" || rows[1].Video.ChannelURL != "https://www.youtube.com/channel/UCreal" {
		t.Errorf("an empty channel link is filled and a known one kept: %q, %q", rows[0].Video.ChannelURL, rows[1].Video.ChannelURL)
	}
	if rows[0].Video.ChannelID != "UCthex" {
		t.Errorf("the channel ID comes from the tab: %q", rows[0].Video.ChannelID)
	}
	if rows[0].Rank >= rows[1].Rank {
		t.Error("ranks must follow the tab's order")
	}

	data, _ := os.ReadFile(log)
	if !strings.Contains(string(data), "https://www.youtube.com/@x/videos") {
		t.Errorf("yt-dlp should read the tab of the channel: %s", data)
	}
}

func TestBrowseChannelTabsAndTheirEmptyCases(t *testing.T) {
	log := fakeChannelYtDlp(t)

	rows, done := browseChannel(ChannelPlaylists, "Fallback")
	if len(rows) != 1 || rows[0].Video.ID != "PLabc123456" || rows[0].Video.Channel != "The X" || done.Err != nil {
		t.Fatalf("the Playlists tab keeps playlists only, named after the channel: %+v %+v", rows, done)
	}

	if rows, done = browseChannel(ChannelLive, ""); len(rows) != 0 || done.Err == nil ||
		!strings.Contains(done.Err.Error(), "no live found") {
		t.Fatalf("an empty tab says so: %+v %+v", rows, done)
	}

	// yt-dlp fails on a tab the channel does not have: that is an empty tab.
	if _, done = browseChannel(ChannelShorts, ""); done.Err == nil || done.Err.Error() != "no shorts found in this channel" {
		t.Fatalf("a missing tab is an empty one: %+v", done)
	}

	data, _ := os.ReadFile(log)
	for _, tab := range []string{"/playlists", "/streams", "/shorts"} {
		if !strings.Contains(string(data), "@x"+tab) {
			t.Errorf("missing a request for %s: %s", tab, data)
		}
	}
}

func TestSearchWithTheChannelsFilterKeepsOnlyChannels(t *testing.T) {
	binDir := t.TempDir()
	fake := `#!/bin/sh
echo "$*" >> ` + filepath.Join(binDir, "log") + `
printf '%s\n' '{"id":"UCZwW2PXNovXy_xUIhmYv7cg","title":"Tiago","url":"https://www.youtube.com/channel/UCZwW2PXNovXy_xUIhmYv7cg"}'
printf '%s\n' '{"id":"video000001","title":"A video","url":"https://www.youtube.com/watch?v=video000001"}'
`
	if err := os.WriteFile(filepath.Join(binDir, "yt-dlp"), []byte(fake), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	var ids []string
	Search(context.Background(), "topic", Filters{Type: TypeChannels}, nil, locale.Locale{}, func(e Event) {
		if e.Kind == EventResult {
			ids = append(ids, e.Video.ID)
		}
	})
	if len(ids) != 1 || ids[0] != "UCZwW2PXNovXy_xUIhmYv7cg" {
		t.Fatalf("results = %v, want the channel only", ids)
	}
	data, _ := os.ReadFile(filepath.Join(binDir, "log"))
	if !strings.Contains(string(data), "sp=EgIQAg%253D%253D") || strings.Count(string(data), "\n") != 1 {
		t.Errorf("one query with the channels filter was expected: %s", data)
	}
}

func TestFetchDetailsSkipsChannels(t *testing.T) {
	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "yt-dlp"), []byte("#!/bin/sh\ntouch "+filepath.Join(binDir, "ran")+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	channel := media.Video{ID: "UCZwW2PXNovXy_xUIhmYv7cg", URL: "https://www.youtube.com/channel/UCZwW2PXNovXy_xUIhmYv7cg"}
	if err := FetchDetailsBatch(context.Background(), []media.Video{channel}, locale.Locale{}, func(media.Video) {
		t.Error("a channel has no details")
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(binDir, "ran")); err == nil {
		t.Error("yt-dlp should not run for a channel")
	}
}

func TestParseChannelPageReadsTheTabsTheChannelHas(t *testing.T) {
	tab := func(path string) string {
		return `{"tabRenderer":{"endpoint":{"commandMetadata":{"webCommandMetadata":{"url":"/@x/` + path + `"}}}}}`
	}
	page := []byte(`<script>var ytInitialData = {"metadata":{"channelMetadataRenderer":{"title":"The X","externalId":"UCthex"}},` +
		`"contents":{"twoColumnBrowseResultsRenderer":{"tabs":[` +
		tab("featured") + `,` + tab("playlists") + `,` + tab("videos") + `,` + tab("posts") +
		`,{"expandableTabRenderer":{}}]}}};</script>`)

	info, err := parseChannelPage(page)
	if err != nil {
		t.Fatal(err)
	}
	if info.ID != "UCthex" || info.Name != "The X" || !slices.Equal(info.Tabs, []int{ChannelVideos, ChannelPlaylists}) {
		t.Errorf("info = %+v", info)
	}

	if _, err := parseChannelPage([]byte(`<html>consent</html>`)); err == nil {
		t.Error("a page that is not a channel page is an error")
	}
}
